package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/yuin/goldmark"
)

// episodePageSize is how many Episodes the list shows before "load more".
const episodePageSize = 20

// episodeRow is one Episode in the list.
type episodeRow struct {
	ID    int64
	Title string
	// CastProfileID seeds the generated cover, so that the Episodes of a show look related.
	CastProfileID string
	CastProfile   string
	Created       string
	Duration      string
	// Failed tells that producing the Episode failed. Failure says why; it is empty for an Episode that is ready and
	// for Visitors in Demo Mode, who are not told why.
	Failed  bool
	Failure string
	// Usage are the lines of the tooltip of what the Episode consumed; none when nothing is stored.
	Usage []string
	// Listened tells that the Episode is Listened.
	Listened bool
}

// episodeList is a page of Episodes, newest first.
type episodeList struct {
	Rows []episodeRow
	// More is the address that loads the next older Episodes; empty when there are no older ones.
	More string
	// Demo tells that the list is shown in Demo Mode, where nothing is offered that changes something.
	Demo bool
}

// chapterView is one Chapter of an Episode; Start is in seconds, as the audio element expects it, and Clock is the
// same moment as minutes and seconds.
type chapterView struct {
	Title string
	Start string
	Clock string
}

// sourceLink is a source named in a Section; Number counts from 1 among the sources of the Episode.
type sourceLink struct {
	Number int
	Title  string
}

// sectionView is a Section of the Script on the Episode page, its text in paragraphs.
type sectionView struct {
	Heading    string
	Paragraphs []string
	Sources    []sourceLink
}

// sourceView is a source in the popup; Text is rendered from Markdown that cannot carry markup of its own.
type sourceView struct {
	Title     string
	Name      string
	Link      string
	Retrieved string
	Text      template.HTML
}

// episodeView is an Episode on its own page; Seconds is the length of the audio in seconds, which the player
// uses to lay out the Chapters before the browser has read the file.
type episodeView struct {
	episodeRow
	Seconds  string
	Chapters []chapterView
	Script   []sectionView
}

// newEpisodeRow shows an Episode of the User in the list and on its page. The CastProfile is shown by its current
// Display Name, or by its Id when the User Config has no such CastProfile. The reason of a failure is left out in Demo
// Mode.
func (h *Handler) newEpisodeRow(user *userconfig.User, e business.Episode, demo bool) episodeRow {
	name := e.CastProfileID
	if profile, ok := user.CastProfile(e.CastProfileID); ok {
		name = profile.DisplayName
	}
	row := episodeRow{
		ID: e.ID, Title: e.Script.Title, CastProfileID: e.CastProfileID, CastProfile: name,
		Created: h.formatTime(e.Date), Duration: formatDuration(e.Duration), Failed: e.Failure != "", Usage: usageLines(e.Usage),
		Listened: e.Listened,
	}
	if !demo {
		row.Failure = e.Failure
	}
	return row
}

// showParam is the query parameter that chooses which Episodes the list shows. With the value showAllValue it shows
// Listened Episodes too; without it the list shows only the Unlistened ones.
const (
	showParam    = "show"
	showAllValue = "all"
)

// pageOpts are the optional parameters of a page of the list of Episodes. The zero value is the first page of the
// Unlistened Episodes.
type pageOpts struct {
	// BeforeID continues after the Episode with this ID, which is the last one of the previous page; zero starts with
	// the newest.
	BeforeID int64
	// ShowAll lists the Listened Episodes too.
	ShowAll bool
	// Demo tells that the list is shown in Demo Mode.
	Demo bool
}

// pageOptsOf reads from the request whether the list shows every Episode.
func pageOptsOf(r *http.Request, demo bool) pageOpts {
	return pageOpts{ShowAll: r.URL.Query().Get(showParam) == showAllValue, Demo: demo}
}

// all tells whether the list shows Episodes of every state. Demo Mode always does, because a Visitor has no state of
// their own.
func (o pageOpts) all() bool {
	return o.ShowAll || o.Demo
}

// episodePage returns one page of the User's Episodes, newest first, after opts.BeforeID. Episodes that failed are
// listed with their reason. The address that loads the next page keeps the choice of opts.ShowAll.
func (h *Handler) episodePage(user *userconfig.User, opts pageOpts) (episodeList, error) {
	episodes, err := h.library.List(user.ID, business.ListOpts{
		Limit: episodePageSize + 1, BeforeID: opts.BeforeID, UnlistenedOnly: !opts.all(),
	})
	if err != nil {
		return episodeList{}, err
	}
	var list episodeList
	if len(episodes) > episodePageSize {
		episodes = episodes[:episodePageSize]
		list.More = fmt.Sprintf("/episodes?before=%d", episodes[len(episodes)-1].ID)
		if opts.ShowAll && !opts.Demo {
			list.More += "&" + showParam + "=" + showAllValue
		}
	}
	for _, e := range episodes {
		list.Rows = append(list.Rows, h.newEpisodeRow(user, e, opts.Demo))
	}
	list.Demo = opts.Demo
	return list, nil
}

// listEpisodes returns the next older Episodes of the selected User as a fragment that "load more" appends.
func (h *Handler) listEpisodes(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	if user == nil {
		http.NotFound(w, r)
		return
	}
	demo := accessOf(r).demo
	opts := pageOptsOf(r, demo)
	if v := r.URL.Query().Get("before"); v != "" {
		var err error
		if opts.BeforeID, err = strconv.ParseInt(v, 10, 64); err != nil || opts.BeforeID < 1 {
			slog.Warn("Load more with a bad cursor rejected", "before", v)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	list, err := h.episodePage(user, opts)
	if err != nil {
		slog.Error("List episodes failed", "user", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.fragment.ExecuteTemplate(w, "episodeList", list); err != nil {
		slog.Error("Render episode list failed", "error", err)
	}
}

// showEpisode renders the page of one Episode of the selected User.
func (h *Handler) showEpisode(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	user := a.selectedUser(r)
	episode, ok := h.episodeNamedInPath(w, r, user, h.library.GetReady)
	if !ok {
		return
	}
	data := h.newPage(a, user)
	data.Episode = episodeView{
		episodeRow: h.newEpisodeRow(user, episode, a.demo),
		Seconds:    strconv.FormatFloat(episode.Duration.Seconds(), 'f', 3, 64),
	}
	for _, section := range episode.Script.Sections {
		view := sectionView{Heading: section.Heading, Paragraphs: paragraphs(section.Body)}
		for _, number := range section.Sources {
			if source, found := episode.Source(number); found {
				view.Sources = append(view.Sources, sourceLink{Number: number, Title: source.Title})
			}
		}
		data.Episode.Script = append(data.Episode.Script, view)
	}
	for _, c := range episode.Chapters {
		data.Episode.Chapters = append(data.Episode.Chapters, chapterView{
			Title: c.Title, Start: strconv.FormatFloat(c.Start.Seconds(), 'f', 3, 64), Clock: formatDuration(c.Start),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.episode.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("Render episode page failed", "id", episode.ID, "error", err)
	}
}

// showSource answers with the popup of one source of an Episode of the selected User as a fragment, which the page
// puts in its dialog.
func (h *Handler) showSource(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	episode, ok := h.episodeNamedInPath(w, r, user, h.library.GetReady)
	if !ok {
		return
	}
	number, err := strconv.Atoi(r.PathValue("number"))
	source, found := episode.Source(number)
	if err != nil || !found {
		slog.Debug("Source not found", "user", user.ID, "id", episode.ID, "number", r.PathValue("number"))
		http.NotFound(w, r)
		return
	}
	view := sourceView{
		Title: source.Title, Name: source.Name, Link: httpLink(source.Link), Retrieved: h.formatTime(source.Retrieved),
		Text: markdown(source.Text),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.fragment.ExecuteTemplate(w, "source", view); err != nil {
		slog.Error("Render source failed", "id", episode.ID, "number", number, "error", err)
	}
}

// serveAudio sends the audio of an Episode of the selected User. Range requests are answered with partial
// content, so the browser can seek.
func (h *Handler) serveAudio(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id, ok := episodeID(r)
	if user == nil || !ok {
		http.NotFound(w, r)
		return
	}
	audio, modTime, err := h.library.OpenAudio(user.ID, id)
	if errors.Is(err, business.ErrEpisodeNotFound) {
		answerEpisodeError(w, r, err)
		return
	}
	if err != nil {
		slog.Error("Open episode audio failed", "user", user.ID, "id", id, "error", err)
		http.Error(w, "audio not available", http.StatusNotFound)
		return
	}
	defer func() { _ = audio.Close() }()
	w.Header().Set("Content-Type", "audio/ogg")
	http.ServeContent(w, r, "", modTime, audio)
}

// setListened sets whether an Episode of the selected User is Listened to the value the form names in listened, true or
// false. The state is sent rather than switched, so that the automatic mark of the player and a click cannot undo
// each other. The marker comes back as a fragment that HTMX puts in place of the old one.
func (h *Handler) setListened(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id, ok := episodeID(r)
	if user == nil || !ok {
		http.NotFound(w, r)
		return
	}
	value := r.PostFormValue("listened")
	if value != "true" && value != "false" {
		slog.Warn("Listened value that is neither true nor false rejected", "user", user.ID, "id", id, "value", value)
		http.Error(w, "listened must be true or false", http.StatusBadRequest)
		return
	}
	episode, err := h.library.SetListened(user.ID, id, value == "true")
	if answerEpisodeError(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.fragment.ExecuteTemplate(w, "listened", h.newEpisodeRow(user, episode, false)); err != nil {
		slog.Error("Render listened marker failed", "id", episode.ID, "error", err)
	}
}

// deleteEpisode removes an Episode of the selected User and returns to the list.
func (h *Handler) deleteEpisode(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id, ok := episodeID(r)
	if user == nil || !ok {
		http.NotFound(w, r)
		return
	}
	if answerEpisodeError(w, r, h.library.Delete(user.ID, id)) {
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// retryEpisode asks for a new Episode in place of a failed Episode of the selected User.
func (h *Handler) retryEpisode(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id, ok := episodeID(r)
	if user == nil || !ok {
		http.NotFound(w, r)
		return
	}
	switch err := h.requests.Retry(user.ID, id); {
	case err == nil:
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, business.ErrCastProfileNotFound):
		http.Error(w, "the CastProfile of this Episode has been removed from the User Config", http.StatusConflict)
	case errors.Is(err, business.ErrAlreadyProducing):
		http.Error(w, "an Episode of this CastProfile is being produced right now", http.StatusConflict)
	case errors.Is(err, business.ErrEpisodeNotFound):
		answerEpisodeError(w, r, err)
	default:
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
}

// generate asks for an Episode of one of the selected User's CastProfiles and returns to the overview.
func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id := r.PathValue("id")
	if user == nil {
		slog.Debug("Generate requested without a User", "profile", id)
		http.NotFound(w, r)
		return
	}
	switch err := h.requests.Request(user.ID, id); {
	case err == nil:
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, business.ErrCastProfileNotFound):
		http.NotFound(w, r)
	default:
		slog.Error("Generate request failed", "user", user.ID, "profile", id, "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
}

// showGeneration answers the polling of a CastProfile's card: the running state while an Episode of it is queued or
// being produced, and the order to reload the page once it is not, so that the new Episode shows.
func (h *Handler) showGeneration(w http.ResponseWriter, r *http.Request) {
	user := accessOf(r).selectedUser(r)
	id := r.PathValue("id")
	if user == nil {
		http.NotFound(w, r)
		return
	}
	profile, ok := user.CastProfile(id)
	if !ok {
		slog.Debug("Generation state requested for an unknown CastProfile", "profile", id)
		http.NotFound(w, r)
		return
	}
	if !h.generator.Generating(user.ID, id) {
		slog.Debug("Production ended, the page is told to reload", "user", user.ID, "profile", id)
		w.Header().Set("HX-Refresh", "true")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.fragment.ExecuteTemplate(w, "generate", castCard{CastProfile: profile, Generating: true}); err != nil {
		slog.Error("Render generation state failed", "profile", id, "error", err)
	}
}

// episodeID reads the number of the Episode from the path.
func episodeID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// episodeNamedInPath finds the Episode named in the path with get, which is one of the lookups of the EpisodeLibrary
// for the selected User. It answers the request itself and reports false when there is none, so another User's Episode
// is indistinguishable from a missing one.
func (h *Handler) episodeNamedInPath(w http.ResponseWriter, r *http.Request, user *userconfig.User,
	get func(userID string, id int64) (business.Episode, error)) (business.Episode, bool) {
	id, ok := episodeID(r)
	if user == nil || !ok {
		http.NotFound(w, r)
		return business.Episode{}, false
	}
	episode, err := get(user.ID, id)
	return episode, !answerEpisodeError(w, r, err)
}

// answerEpisodeError answers the request for an error of the EpisodeLibrary and reports whether it did: 404 for an
// Episode that is not there or cannot do what was asked, 500 for anything else. It does nothing for a nil error.
func answerEpisodeError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, business.ErrEpisodeNotFound), errors.Is(err, business.ErrEpisodeFailed):
		slog.Debug("Episode not found or not able to do what was asked", "path", r.URL.Path, "error", err)
		http.NotFound(w, r)
	default:
		slog.Error("Episode request failed", "path", r.URL.Path, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	return true
}

// markdown renders the Markdown of a crawled page. Raw HTML in it is left out and links with dangerous schemes are
// emptied, so a hostile page cannot bring markup or script into the popup.
func markdown(text string) template.HTML {
	var out bytes.Buffer
	if err := goldmark.New().Convert([]byte(text), &out); err != nil {
		slog.Error("Rendering Markdown failed", "error", err)
		return template.HTML(template.HTMLEscapeString(text))
	}
	return template.HTML(out.String()) //nolint:gosec // goldmark leaves raw HTML out unless told otherwise
}

// httpLink is the address if it is an http or https one, else empty.
func httpLink(address string) string {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	return address
}

// paragraphs splits a text at blank lines.
func paragraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// usageLines shows what an Episode consumed as one line for each kind and unit in the form "Script input tokens:
// 45,120", sorted by their text. Every kind and unit is rendered the same way, so a new one is one more line.
func usageLines(usage business.Usage) []string {
	lines := make([]string, 0, len(usage))
	for _, item := range usage {
		kind := strings.ToUpper(string(item.Kind)[:1]) + string(item.Kind)[1:]
		lines = append(lines, fmt.Sprintf("%s %s: %s", kind, strings.ReplaceAll(string(item.Unit), "_", " "), withThousands(item.Quantity)))
	}
	sort.Strings(lines)
	return lines
}

// withThousands writes a number with a comma between groups of three digits.
func withThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return sign + digits
}

// formatTime shows a time in the service time zone.
func (h *Handler) formatTime(t time.Time) string {
	return t.In(h.loc).Format("2006-01-02 15:04")
}

// formatDuration shows a length as minutes and seconds.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
