// Package web is the web interface of AI Gude: a standard library router,
// html/template pages and interactivity through HTMX.
//
// Pico.css, HTMX, Prism with its TOML grammar and the fonts of the theme are
// embedded in the binary, so the browser loads everything from this service.
// The four files and the fonts in static/ are git-ignored: `task assets`
// downloads them unmodified from their npm packages @picocss/pico, htmx.org,
// prismjs and @fontsource in the versions and with the checksums pinned in
// Taskfile.yml. The build embeds them, so build and test through task, or run
// `task assets` once, before using plain go commands.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// userCookie remembers the chosen User in the browser. The cookie says
// whose pages to show; the service is meant for a trusted home network.
const userCookie = "aigude_user"

// UserConfigSource hands out the User Config in force. Every request fetches
// it once and works with that Snapshot throughout, so a reload in the middle
// of a request cannot show a mix of old and new.
type UserConfigSource interface {
	Snapshot() *userconfig.Snapshot
	// File reads the file of a User as it is on disk.
	File(userID string) (userconfig.File, error)
	// Save replaces the file of a User if the text is valid and the file still has the content with the hash.
	Save(userID, text, baseHash string) error
}

// Generator queues the production of Episodes.
type Generator interface {
	// Generate asks for an Episode of the User's CastProfile; it is produced when its turn comes.
	Generate(userID, castProfileID string) error
	// Generating tells whether an Episode of the User's CastProfile is queued or being produced.
	Generating(userID, castProfileID string) bool
	// NothingNew tells whether the latest production for the User's CastProfile found no unused Content, and when it
	// looked.
	NothingNew(userID, castProfileID string) (time.Time, bool)
}

// Headers of the answer to GET /log: the cursor for the next request, and "true" when the reader starts over.
const (
	logCursorHeader = "X-Log-Cursor"
	logResetHeader  = "X-Log-Reset"
)

// LogSource hands out the newest log lines of the service, oldest first. Since returns the lines after the position
// of a cursor and the cursor for the next call; reset is true when the cursor stood for no position and the reader
// has to start over.
type LogSource interface {
	Since(cursor string) (lines []string, next string, reset bool)
}

// castCard is a CastProfile on the overview, with whether an Episode of it is being produced.
type castCard struct {
	userconfig.CastProfile
	Generating bool
	// NothingNew is when the latest production found no unused Content in the Max Lookback; empty if it did not.
	NothingNew string
}

// page is what the templates render.
type page struct {
	UsersDir string
	// Users fill the dropdown.
	Users []userconfig.User
	// Selected is the User whose pages are shown; nil when there are no Users.
	Selected *userconfig.User
	// Pinned tells that the proxy named the User, who therefore cannot be switched.
	Pinned bool
	// Demo tells that the request is in Demo Mode: nothing that changes something is offered, and what only the person
	// running the service should see is left out.
	Demo bool
	// Problems are what is wrong in the User Config files right now.
	Problems userconfig.Problems
	// CastCards are the CastProfiles of the selected User.
	CastCards []castCard
	// Producing are the CastProfiles of the selected User of which an Episode is being produced.
	Producing []castCard
	// Episodes are the newest Episodes of the selected User; More loads the older ones.
	Episodes episodeList
	// Episode is the Episode shown on its own page.
	Episode episodeView
	// Source is the source shown on its own page.
	Source sourceView
	// Editor is the User Config editor.
	Editor editorView
}

// Handler serves the pages and the embedded static files.
type Handler struct {
	// usersDir is the folder holding the User Config files, shown in the empty state.
	usersDir string
	// source is where the current User Config comes from.
	source UserConfigSource
	// episodes holds the produced Episodes.
	episodes business.EpisodeRepository
	// audio opens the audio of the Episodes for playing.
	audio business.AudioStore
	// generator produces Episodes on request.
	generator Generator
	// logs are the log lines shown in the folding log.
	logs LogSource
	// loc is the time zone in which dates are shown.
	loc      *time.Location
	index    *template.Template
	episode  *template.Template
	fragment *template.Template
	// sourcePage shows one source on a page of its own.
	sourcePage *template.Template
	// config is the page of the User Config editor.
	config *template.Template
	// mux routes the pages of the interface, which sit behind the check of the proxy headers.
	mux *http.ServeMux
	// root routes the health endpoint around that check and everything else through it.
	root *http.ServeMux
}

// New parses the embedded templates and builds the router.
func New(usersDir string, source UserConfigSource, episodes business.EpisodeRepository, audio business.AudioStore, generator Generator, logs LogSource, loc *time.Location) (*Handler, error) {
	index, err := template.ParseFS(templateFS, "templates/layout.html", "templates/index.html", "templates/episode_list.html", "templates/generate.html", "templates/usage.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	episode, err := template.ParseFS(templateFS, "templates/layout.html", "templates/episode.html", "templates/usage.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	fragment, err := template.ParseFS(templateFS, "templates/episode_list.html", "templates/generate.html", "templates/log.html", "templates/usage.html", "templates/source.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	sourcePage, err := template.ParseFS(templateFS, "templates/layout.html", "templates/source_page.html", "templates/source.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	config, err := template.ParseFS(templateFS, "templates/layout.html", "templates/config.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("open embedded static files: %w", err)
	}

	h := &Handler{
		usersDir: usersDir, source: source, episodes: episodes, audio: audio, generator: generator, logs: logs, loc: loc,
		index: index, episode: episode, fragment: fragment, sourcePage: sourcePage, config: config, mux: http.NewServeMux(), root: http.NewServeMux(),
	}
	h.mux.HandleFunc("GET /{$}", h.showIndex)
	h.mux.HandleFunc("POST /user", h.selectUser)
	h.mux.HandleFunc("GET /config", h.showConfig)
	h.mux.HandleFunc("POST /config", h.saveConfig)
	h.mux.HandleFunc("POST /cast-profiles/{id}/generate", h.generate)
	h.mux.HandleFunc("GET /cast-profiles/{id}/generation", h.showGeneration)
	h.mux.HandleFunc("GET /log", h.showLog)
	h.mux.HandleFunc("GET /episodes", h.listEpisodes)
	h.mux.HandleFunc("GET /episodes/{id}", h.showEpisode)
	h.mux.HandleFunc("GET /episodes/{id}/audio", h.serveAudio)
	h.mux.HandleFunc("GET /episodes/{id}/sources/{number}", h.showSource)
	h.mux.HandleFunc("POST /episodes/{id}/delete", h.deleteEpisode)
	h.mux.HandleFunc("POST /episodes/{id}/retry", h.retryEpisode)
	h.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	h.root.HandleFunc("GET /healthz", h.healthz)
	h.root.Handle("/", h.withAccess(h.mux))
	slog.Info("Proxy header is honoured", "userHeader", userHeader, "demoUser", demoUserID)
	return h, nil
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.root.ServeHTTP(w, r)
}

// healthz answers 200 with "ok" and checks nothing in the backend. It lies outside the check of the proxy header,
// so a health check needs no header and Demo Mode does not matter.
func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok")
}

// newPage is the part of every page that does not depend on what the page shows.
func (h *Handler) newPage(a access, selected *userconfig.User) page {
	data := page{UsersDir: h.usersDir, Users: a.snapshot.Users(), Selected: selected, Pinned: a.pinned != nil, Demo: a.demo}
	if !a.demo {
		data.Problems = a.snapshot.Problems()
	}
	return data
}

// showIndex renders the page of the selected User, or the empty state when
// there are no Users.
func (h *Handler) showIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	a := accessOf(r)
	data := h.newPage(a, a.selectedUser(r))
	if data.Selected != nil {
		for _, profile := range data.Selected.CastProfiles {
			card := castCard{CastProfile: profile, Generating: h.generator.Generating(data.Selected.ID, profile.ID)}
			if at, found := h.generator.NothingNew(data.Selected.ID, profile.ID); found && !card.Generating {
				card.NothingNew = h.formatTime(at)
			}
			data.CastCards = append(data.CastCards, card)
			if card.Generating {
				data.Producing = append(data.Producing, card)
			}
		}
		list, err := h.episodePage(data.Selected, business.ListOpts{}, a.demo)
		if err != nil {
			slog.Error("List episodes failed", "user", data.Selected.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		list.Demo = a.demo
		data.Episodes = list
	}
	if err := h.index.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("Render index page failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// selectUser remembers the chosen User in a cookie and returns to the index page.
func (h *Handler) selectUser(w http.ResponseWriter, r *http.Request) {
	id := r.PostFormValue("user")
	if _, ok := accessOf(r).snapshot.User(id); !ok {
		slog.Warn("Choice of unknown User rejected", "user", id)
		http.Error(w, "unknown user", http.StatusBadRequest)
		return
	}
	slog.Debug("User chosen", "user", id)
	http.SetCookie(w, &http.Cookie{
		Name: userCookie, Value: id, Path: "/", MaxAge: 365 * 24 * 60 * 60,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// showLog answers the polling of the folding log with the lines after the cursor of the request, the oldest first,
// the newest at the bottom. The cursor for the next request and whether the reader starts over are in the headers.
func (h *Handler) showLog(w http.ResponseWriter, r *http.Request) {
	lines, next, reset := h.logs.Since(r.URL.Query().Get("cursor"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set(logCursorHeader, next)
	if reset {
		w.Header().Set(logResetHeader, "true")
	}
	if err := h.fragment.ExecuteTemplate(w, "log", lines); err != nil {
		slog.Error("Render log failed", "error", err)
	}
}
