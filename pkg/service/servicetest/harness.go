// Package servicetest is the test seam of AI Gude: it builds the whole
// service in-process with temporary folders, replaced external systems and a
// controllable clock, and lets tests drive it through HTTP.
//
// Only the edges are fake. Settings loading aside, everything else (handlers,
// templates, ffmpeg) is the real thing.
package servicetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/logbuffer"
	"github.com/alex0ptr/aigude/pkg/service"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // reads the database of the service
)

// How long WaitFor looks for an effect and how often.
const (
	waitTimeout  = 5 * time.Second
	waitInterval = 20 * time.Millisecond
)

// Harness is a running service together with its fakes.
type Harness struct {
	// Settings the service was built with; the folders are temporary.
	Settings settings.Settings
	// Clock is the clock the service sees; it only moves when told to.
	Clock *FakeClock
	// ScriptWriter and TTS are the fakes behind the Script and speech.
	ScriptWriter *FakeScriptWriter
	TTS          *FakeTTS
	// Crawler is the fake behind the retrieval of full article pages.
	Crawler *FakeCrawler
	// Logs keeps what the service logs while the test runs.
	Logs *logbuffer.Handler
	// URL is the base address of the in-process HTTP server.
	URL string

	client  *http.Client
	service *service.Service
	server  *httptest.Server
	cancel  context.CancelFunc
}

// Settings returns the defaults with every folder in a temporary directory
// and the time zone fixed, so tests do not depend on the machine.
func Settings(t *testing.T) settings.Settings {
	t.Helper()
	s := settings.Defaults()
	s.TimeZone = "Europe/Berlin"
	// Background work checks the clock often, so tests that move the fake clock see its effects soon.
	s.SchedulerMaxSleep = 20 * time.Millisecond
	dir := t.TempDir()
	s.UsersDir = dir + "/users"
	s.DataDir = dir + "/data"
	require.NoError(t, os.MkdirAll(s.UsersDir, 0o755))
	return s
}

// Option changes how Start builds the service.
type Option func(t *testing.T, h *Harness)

// WithUserConfigFile puts a User Config file into the users folder before
// the service starts. The name is the file name inside the folder.
func WithUserConfigFile(name, text string) Option {
	return func(t *testing.T, h *Harness) { WriteUserConfig(t, h.Settings, name, text) }
}

// WithSettings changes the settings the service is built with, for example a shorter article limit.
func WithSettings(change func(*settings.Settings)) Option {
	return func(t *testing.T, h *Harness) { change(&h.Settings) }
}

// WriteUserConfig writes a User Config file into the users folder of the settings.
func WriteUserConfig(t *testing.T, s settings.Settings, name, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(s.UsersDir, name), []byte(text), 0o644))
}

// RemoveUserConfig deletes a User Config file from the users folder of the settings.
func RemoveUserConfig(t *testing.T, s settings.Settings, name string) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(s.UsersDir, name)))
}

// logCapacity is how many lines the log of a test service keeps.
const logCapacity = 500

// processLogger is the logger the process started with, before any test replaced it.
var processLogger = slog.Default()

// NewLogs creates the log lines of a test service. Its records are kept and not printed.
func NewLogs() *logbuffer.Handler {
	return logbuffer.New(slog.NewTextHandler(io.Discard, nil), logCapacity)
}

// logsOnTopOfTheLogger creates the log lines of a test service that also pass records on to the logger a test has
// installed to look at them. The logger of the process itself is not wrapped, because its handler writes through the
// logger that is the default, which would be these lines again.
func logsOnTopOfTheLogger() *logbuffer.Handler {
	if slog.Default() == processLogger {
		return NewLogs()
	}
	return logbuffer.New(slog.Default().Handler(), logCapacity)
}

// Start builds the service with fakes and serves it on a local test server
// that is closed when the test ends. The HTTP client keeps cookies like a
// browser, so a chosen User stays chosen across requests.
func Start(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	h := &Harness{
		Settings:     Settings(t),
		Clock:        NewFakeClock(),
		ScriptWriter: &FakeScriptWriter{},
		TTS:          &FakeTTS{},
		Crawler:      NewFakeCrawler(),
	}
	for _, opt := range opts {
		opt(t, h)
	}
	h.Logs = logsOnTopOfTheLogger()
	previous := slog.Default()
	slog.SetDefault(slog.New(h.Logs))
	t.Cleanup(func() { slog.SetDefault(previous) })
	h.launch(t)
	t.Cleanup(func() { h.stop(t) })
	return h
}

// launch builds the service from the settings of the harness and starts it and its test server.
func (h *Harness) launch(t *testing.T) {
	t.Helper()
	svc, err := service.New(h.Settings, h.ScriptWriter, h.TTS, h.Crawler, h.Clock, h.Logs)
	require.NoError(t, err)
	h.service = svc
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	require.NoError(t, svc.Start(ctx))
	slog.Debug("Test service assembled", "usersDir", h.Settings.UsersDir)

	h.server = httptest.NewServer(svc.Handler())
	h.URL = h.server.URL
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	h.client = h.server.Client()
	h.client.Jar = jar
}

// stop ends the service and closes its test server; stopping a stopped service does nothing.
func (h *Harness) stop(t *testing.T) {
	t.Helper()
	if h.service == nil {
		return
	}
	h.server.Close()
	h.cancel()
	h.service.Wait()
	require.NoError(t, h.service.Close())
	h.service = nil
}

// Stop ends the service as a shutdown does. The folders, the database and the clock stay as they are.
func (h *Harness) Stop(t *testing.T) {
	t.Helper()
	h.stop(t)
}

// Restart starts the service again on the folders, the database and the clock of the one that was stopped, as a
// restart of the process does. The browser is a new one: it has no cookies.
func (h *Harness) Restart(t *testing.T) {
	t.Helper()
	require.Nil(t, h.service, "the service must be stopped before it is restarted")
	h.launch(t)
}

// AddEpisode puts a produced Episode with its audio into the persistence of the service, as generation will, and
// returns it with its ID. It is how tests prepare Episodes.
func (h *Harness) AddEpisode(t *testing.T, episode business.Episode, audio []byte) business.Episode {
	t.Helper()
	saved, err := h.service.Episodes().Save(episode, audio)
	require.NoError(t, err)
	return saved
}

// AddFailedEpisode puts an Episode that failed with the reason into the persistence, as a failed production does.
func (h *Harness) AddFailedEpisode(t *testing.T, episode business.Episode, failure string) business.Episode {
	t.Helper()
	saved, err := h.service.Episodes().SaveFailed(episode, failure)
	require.NoError(t, err)
	return saved
}

// Episodes returns all Episodes of the User as the persistence holds them, newest first, including those that
// failed and have no audio.
func (h *Harness) Episodes(t *testing.T, userID string) []business.Episode {
	t.Helper()
	episodes, err := h.service.Episodes().List(userID, business.ListOpts{})
	require.NoError(t, err)
	return episodes
}

// Episode returns one Episode of the User with its Sections and Chapters.
func (h *Harness) Episode(t *testing.T, userID string, id int64) business.Episode {
	t.Helper()
	episode, err := h.service.Episodes().Get(userID, id)
	require.NoError(t, err)
	return episode
}

// CachedPages returns the pages in the crawl cache, by address, as the database holds them.
func (h *Harness) CachedPages(t *testing.T) map[string]business.Page {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.Settings.DataDir, "aigude.db")+"?mode=ro&_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT url, title, body FROM crawled_pages`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	pages := make(map[string]business.Page)
	for rows.Next() {
		var url string
		var page business.Page
		require.NoError(t, rows.Scan(&url, &page.Title, &page.Text))
		pages[url] = page
	}
	require.NoError(t, rows.Err())
	return pages
}

// AudioExists tells whether the audio of an Episode prepared with AddEpisode is still stored. The harness runs the
// file implementation of the audio store, so it can look into the data folder.
func (h *Harness) AudioExists(t *testing.T, episode business.Episode) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(h.Settings.DataDir, episode.AudioLocation))
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	return true
}

// Page is a response of the service as a User's browser would see it.
type Page struct {
	Status int
	Header http.Header
	Body   string
}

// Get requests a path of the service.
func (h *Harness) Get(t *testing.T, path string) Page {
	t.Helper()
	resp, err := h.client.Get(h.URL + path)
	require.NoError(t, err)
	return readPage(t, resp)
}

// GetWithHeader requests a path of the service with one request header set, for example a Range.
func (h *Harness) GetWithHeader(t *testing.T, path, header, value string) Page {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set(header, value)
	resp, err := h.client.Do(req)
	require.NoError(t, err)
	return readPage(t, resp)
}

// GetWithHeaders requests a path of the service with several request headers set, as a proxy in front would. A header
// with an empty value is sent empty.
func (h *Harness) GetWithHeaders(t *testing.T, path string, headers map[string]string) Page {
	t.Helper()
	return h.Send(t, http.MethodGet, path, headers, nil)
}

// PostWithHeaders submits an empty form to a path with several request headers set.
func (h *Harness) PostWithHeaders(t *testing.T, path string, headers map[string]string) Page {
	t.Helper()
	return h.Send(t, http.MethodPost, path, headers, nil)
}

// SelectUserWithHeaders chooses a User in the dropdown with request headers set, as a proxy in front would.
func (h *Harness) SelectUserWithHeaders(t *testing.T, id string, headers map[string]string) Page {
	t.Helper()
	return h.Send(t, http.MethodPost, "/user", headers, url.Values{"user": {id}})
}

// Send makes a request of any method with several request headers set; form is sent as the body of a POST.
func (h *Harness) Send(t *testing.T, method, path string, headers map[string]string, form url.Values) Page {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, h.URL+path, body)
	require.NoError(t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := h.client.Do(req)
	require.NoError(t, err)
	return readPage(t, resp)
}

// GetWithHeaderValues requests a path with request headers that may be repeated, as a misconfigured proxy would send them.
func (h *Harness) GetWithHeaderValues(t *testing.T, path string, header http.Header) Page {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.URL+path, nil)
	require.NoError(t, err)
	req.Header = header
	resp, err := h.client.Do(req)
	require.NoError(t, err)
	return readPage(t, resp)
}

// Post submits an empty form to a path, as pressing a button does, and returns the page the browser lands on.
func (h *Harness) Post(t *testing.T, path string) Page {
	t.Helper()
	resp, err := h.client.PostForm(h.URL+path, url.Values{})
	require.NoError(t, err)
	return readPage(t, resp)
}

// WaitFor requests a path again and again until check accepts the page, for
// effects that arrive on their own such as a reloaded User Config. It
// fails the test with the last page when that does not happen in time.
func (h *Harness) WaitFor(t *testing.T, path, what string, check func(Page) bool) Page {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		page := h.Get(t, path)
		if check(page) {
			return page
		}
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s; last response %d:\n%s", what, page.Status, page.Body)
		}
		time.Sleep(waitInterval)
	}
}

// SelectUser chooses a User in the dropdown, as submitting its form does, and
// returns the page the browser lands on.
func (h *Harness) SelectUser(t *testing.T, id string) Page {
	t.Helper()
	resp, err := h.client.PostForm(h.URL+"/user", url.Values{"user": {id}})
	require.NoError(t, err)
	return readPage(t, resp)
}

// SetCookie plants a cookie in the client, as a browser that visited before would have one.
func (h *Harness) SetCookie(t *testing.T, name, value string) {
	t.Helper()
	u, err := url.Parse(h.URL)
	require.NoError(t, err)
	h.client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

// readPage reads and closes a response.
func readPage(t *testing.T, resp *http.Response) Page {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return Page{Status: resp.StatusCode, Header: resp.Header, Body: string(body)}
}

// ServeFeed serves an RSS document on a local test server and returns its
// address, to be used as a Subscription's feed address. The server is closed
// when the test ends.
func ServeFeed(t *testing.T, rss string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, rss)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// ChangingFeed is an RSS feed whose document can be replaced while the service runs.
type ChangingFeed struct {
	// URL is the address of the feed, to be used as a Subscription's feed address.
	URL string

	mu  sync.Mutex
	rss string
}

// ServeChangingFeed serves the RSS document on a local test server, which is closed when the test ends.
func ServeChangingFeed(t *testing.T, rss string) *ChangingFeed {
	t.Helper()
	feed := &ChangingFeed{rss: rss}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feed.mu.Lock()
		defer feed.mu.Unlock()
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, feed.rss)
	}))
	t.Cleanup(srv.Close)
	feed.URL = srv.URL
	return feed
}

// Set replaces the document the feed serves.
func (f *ChangingFeed) Set(rss string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rss = rss
}

// FakeClock is a clock that stands still until it is advanced.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	reads  int
	sleeps []time.Duration
}

// NewFakeClock starts a clock at a fixed instant: Thursday 29 October 2026, 08:00 in Berlin.
func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Date(2026, time.October, 29, 7, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return c.now
}

// Sleep does not wait: it moves the clock forward by the duration and remembers it.
func (c *FakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

// Sleeps returns the durations the service waited, in order.
func (c *FakeClock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

// Reads is how often the clock has been asked for the time, which shows how busy the code that watches it is.
func (c *FakeClock) Reads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// Advance moves the clock forward; a negative duration moves it back.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// ScriptRequest is one request to FakeScriptWriter.
type ScriptRequest struct {
	System, Style string
	Contents      []business.Content
}

// FakeCrawler is a crawler that knows a fixed set of pages.
type FakeCrawler struct {
	mu    sync.Mutex
	pages map[string]business.Page
	fails map[string]error
	calls []string
}

// NewFakeCrawler creates a crawler that knows no pages.
func NewFakeCrawler() *FakeCrawler {
	return &FakeCrawler{pages: make(map[string]business.Page), fails: make(map[string]error)}
}

// AddPage makes the crawler return the page for the address.
func (f *FakeCrawler) AddPage(url string, page business.Page) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[url] = page
}

// FailFor makes the crawler fail for the address.
func (f *FakeCrawler) FailFor(url string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[url] = err
}

// Crawl returns the page known for the address, or fails. A page that is returned counts as one page of Usage.
func (f *FakeCrawler) Crawl(url string) (business.Page, business.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, url)
	if err := f.fails[url]; err != nil {
		return business.Page{}, nil, err
	}
	page, ok := f.pages[url]
	if !ok {
		return business.Page{}, nil, fmt.Errorf("fake crawler knows no page at %s", url)
	}
	return page, business.Usage{{Kind: business.UsageCrawl, Unit: business.UnitPages, Quantity: 1}}, nil
}

// Calls returns the addresses that were crawled, in order.
func (f *FakeCrawler) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// FakeScriptWriter is a script writer that returns a fixed Script.
type FakeScriptWriter struct {
	// Script is returned for every request.
	Script business.Script
	// Err, when set, is returned for every request.
	Err error
	// Delay is how long writing takes.
	Delay time.Duration
	// Usage is what every request reports, also a failed one.
	Usage business.Usage

	mu        sync.Mutex
	requests  []ScriptRequest
	active    int
	maxActive int
}

// WriteScript returns the fixed Script.
func (f *FakeScriptWriter) WriteScript(system, style string, contents []business.Content) (business.Script, business.Usage, error) {
	f.mu.Lock()
	f.requests = append(f.requests, ScriptRequest{System: system, Style: style, Contents: contents})
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	script, err, delay, usage := f.Script, f.Err, f.Delay, f.Usage
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return script, usage, err
}

// Requests returns every request made so far, in order.
func (f *FakeScriptWriter) Requests() []ScriptRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ScriptRequest(nil), f.requests...)
}

// MostAtOnce is the largest number of requests that were being written at the same time.
func (f *FakeScriptWriter) MostAtOnce() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

// fakeSampleRate is the sample rate of the speech FakeTTS makes.
const fakeSampleRate = 24000

// SpeechCall is one request to FakeTTS.
type SpeechCall struct {
	Text  string
	Voice business.Voice
}

// FakeTTS is a speech synthesis that answers with silence of a fixed length.
type FakeTTS struct {
	// Length is the duration of the speech of every text; one second when not set.
	Length time.Duration
	// Err, when set, is returned for every text.
	Err error
	// Errs are the errors of the first calls, in order; a nil entry is a call that succeeds. Calls after them
	// succeed, unless Err is set.
	Errs []error
	// Usage is what every call reports, also a failed one.
	Usage business.Usage

	mu    sync.Mutex
	calls []SpeechCall
}

// Synthesize returns silence of the configured length.
func (f *FakeTTS) Synthesize(text string, voice business.Voice) (business.Speech, business.Usage, error) {
	f.mu.Lock()
	call := len(f.calls)
	f.calls = append(f.calls, SpeechCall{Text: text, Voice: voice})
	length, err, usage := f.Length, f.Err, f.Usage
	if call < len(f.Errs) {
		err = f.Errs[call]
	}
	f.mu.Unlock()
	if err != nil {
		return business.Speech{}, usage, err
	}
	if length == 0 {
		length = time.Second
	}
	samples := int(length * fakeSampleRate / time.Second)
	return business.Speech{PCM: make([]byte, samples*2), SampleRate: fakeSampleRate}, usage, nil
}

// Calls returns every request made so far, in order.
func (f *FakeTTS) Calls() []SpeechCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SpeechCall(nil), f.calls...)
}

// Texts returns the texts that were spoken, in order.
func (f *FakeTTS) Texts() []string {
	var texts []string
	for _, c := range f.Calls() {
		texts = append(texts, c.Text)
	}
	return texts
}

// opusSampleRate is the rate in which the granule positions of an Ogg Opus stream count.
const opusSampleRate = 48000

// OggDuration reads the length of OGG/Opus audio from the granule position of its last page, which counts samples
// at 48 kHz, including the few milliseconds of encoder delay at the start.
func OggDuration(t *testing.T, ogg []byte) time.Duration {
	t.Helper()
	last := bytes.LastIndex(ogg, []byte("OggS"))
	require.GreaterOrEqual(t, last, 0, "not OGG audio")
	require.GreaterOrEqual(t, len(ogg), last+14, "truncated OGG page")
	granule := binary.LittleEndian.Uint64(ogg[last+6:])
	return time.Duration(granule) * time.Second / opusSampleRate
}

// OggChapters reads the chapters of an OGG file with ffprobe, a reader that is independent of the code that wrote
// them, and returns them in order. A file without chapters yields none.
func OggChapters(t *testing.T, ogg []byte) []business.Chapter {
	t.Helper()
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_chapters", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(ogg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Run(), "ffprobe: %s", stderr.String())

	var probed struct {
		Chapters []struct {
			StartTime string            `json:"start_time"`
			Tags      map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &probed))
	var chapters []business.Chapter
	for _, c := range probed.Chapters {
		seconds, err := strconv.ParseFloat(c.StartTime, 64)
		require.NoError(t, err)
		chapters = append(chapters, business.Chapter{Title: c.Tags["title"], Start: time.Duration(math.Round(seconds*1000)) * time.Millisecond})
	}
	return chapters
}

// OggPlays tells whether ffmpeg decodes the whole OGG file without an error.
func OggPlays(t *testing.T, ogg []byte) bool {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(ogg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil || stderr.Len() > 0 {
		t.Logf("ffmpeg could not play the file: %v %s", err, stderr.String())
		return false
	}
	return true
}
