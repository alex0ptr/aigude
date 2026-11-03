// Package service assembles AI Gude from its settings and its external
// systems and runs it. It is the single place where the pieces meet, which is
// also why tests build the whole service through New.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/alex0ptr/aigude/business"
	pkgaudio "github.com/alex0ptr/aigude/pkg/audio"
	"github.com/alex0ptr/aigude/pkg/database"
	"github.com/alex0ptr/aigude/pkg/filestore"
	"github.com/alex0ptr/aigude/pkg/oggchapters"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/alex0ptr/aigude/pkg/web"
)

// shutdownGrace is how long running requests get to finish on shutdown.
const shutdownGrace = 10 * time.Second

// Service is the assembled AI Gude.
type Service struct {
	settings settings.Settings
	store    *userconfig.Store
	db       *database.Database
	episodes business.EpisodeRepository
	handler  http.Handler
	// generator produces the requested Episodes one at a time.
	generator *generator
	// scheduler starts the Episodes of the Schedules when they are due.
	scheduler *scheduler
	// maintenance keeps the crawl cache and the stored Episodes tidy.
	maintenance *maintenance

	// watchDone, workerDone, schedulerDone and maintenanceDone are closed when the watching of the User Config, the
	// generator's worker, the scheduler and the maintenance have ended.
	watchDone       <-chan struct{}
	workerDone      <-chan struct{}
	schedulerDone   <-chan struct{}
	maintenanceDone <-chan struct{}
}

// New assembles the service. It fails, without starting anything, when the time zone is unknown, the data folder
// or the database cannot be created or migrated, or the User Config is invalid.
func New(s settings.Settings, writer business.ScriptWriter, tts business.TTS, crawler business.Crawler, clock business.Clock, logs web.LogSource) (*Service, error) {
	loc, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("cannot start: time zone %q: %w", s.TimeZone, err)
	}
	slog.Debug("Preparing data folder", "dataDir", s.DataDir)
	if err := os.MkdirAll(s.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot start: data folder %s: %w", s.DataDir, err)
	}

	slog.Debug("Opening database", "dataDir", s.DataDir)
	db, err := database.Open(s.DataDir)
	if err != nil {
		return nil, fmt.Errorf("cannot start: %w", err)
	}

	slog.Debug("Loading User Config", "usersDir", s.UsersDir)
	store, err := userconfig.Open(s.UsersDir)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cannot start: %w", err)
	}

	audio := filestore.New(s.DataDir)
	episodes := db.Episodes(audio)
	cachingCrawler := business.NewCachingCrawler(crawler, db.CrawlCache(), clock, s.CrawlCacheTTL)
	voice := business.Voice{Language: s.DefaultLanguage, Name: s.DefaultVoice}
	producer := business.NewEpisodes(writer, tts, oggchapters.NewEncoder(pkgaudio.NewFFmpegConverter()), episodes, clock, loc, voice,
		s.WordBudget, s.ArticleCharLimit, s.SectionPause)
	gen := newGenerator(store, producer, cachingCrawler, clock)
	handler, err := web.New(s.UsersDir, store, episodes, audio, gen, logs, loc)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cannot start: %w", err)
	}
	slog.Info("Service assembled", "timeZone", s.TimeZone, "now", clock.Now().In(loc),
		"usersDir", s.UsersDir, "dataDir", s.DataDir)
	return &Service{settings: s, store: store, db: db, episodes: episodes, handler: handler, generator: gen,
		scheduler:   newScheduler(store, gen, episodes, clock, loc, s.SchedulerMaxSleep),
		maintenance: newMaintenance(cachingCrawler, episodes, s.EpisodeRetention, clock, s.SchedulerMaxSleep)}, nil
}

// Episodes returns the persistence of produced Episodes.
func (s *Service) Episodes() business.EpisodeRepository {
	return s.episodes
}

// Close releases the database. It is called once the service has stopped.
func (s *Service) Close() error {
	return s.db.Close()
}

// Handler returns the HTTP interface of the service.
func (s *Service) Handler() http.Handler {
	return s.handler
}

// Start begins the background work of the service, which keeps going until ctx is cancelled: following the users
// folder, so changes to the files take effect while the service runs, producing the requested Episodes, starting
// those that are scheduled and, once a day, tidying the crawl cache and deleting the Episodes past their Retention.
// It returns once that work is running; Wait blocks until it has ended.
func (s *Service) Start(ctx context.Context) error {
	slog.Debug("Starting background work")
	done, err := s.store.Watch(ctx)
	if err != nil {
		return fmt.Errorf("cannot start: %w", err)
	}
	s.watchDone = done
	s.workerDone = s.generator.run(ctx)
	s.schedulerDone = s.scheduler.run(ctx)
	s.maintenanceDone = s.maintenance.run(ctx)
	return nil
}

// Wait blocks until the background work begun by Start has ended.
func (s *Service) Wait() {
	if s.watchDone != nil {
		<-s.watchDone
	}
	if s.workerDone != nil {
		<-s.workerDone
	}
	if s.schedulerDone != nil {
		<-s.schedulerDone
	}
	if s.maintenanceDone != nil {
		<-s.maintenanceDone
	}
}

// Run starts the background work and serves the web interface on the
// configured listen address until ctx is cancelled. A requested shutdown
// returns nil.
func (s *Service) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.settings.ListenAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", s.settings.ListenAddr, err)
	}
	ctx, stop := context.WithCancel(ctx)
	defer func() {
		stop()
		s.Wait()
		if err := s.Close(); err != nil {
			slog.Error("Closing the database failed", "error", err)
		}
	}()
	if err := s.Start(ctx); err != nil {
		_ = ln.Close()
		return err
	}
	srv := &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	slog.Info("Listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("web server stopped: %w", err)
	case <-ctx.Done():
	}

	slog.Info("Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
