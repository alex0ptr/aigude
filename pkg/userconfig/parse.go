package userconfig

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// slug is the form of every Id: lowercase letters, digits, underscore, hyphen.
var slug = regexp.MustCompile(`^[a-z0-9_-]+$`)

// weekdays are the accepted names of the days in a Schedule.
var weekdays = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday,
	"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

// The raw types mirror the file; pointers tell "not written" from the zero value.
type rawFile struct {
	User         rawUser                   `toml:"user"`
	CastProfiles map[string]rawCastProfile `toml:"cast_profiles"`
}

type rawUser struct {
	ID          string `toml:"id"`
	DisplayName string `toml:"display_name"`
}

type rawSubscription struct {
	DisplayName string `toml:"display_name"`
	Kind        string `toml:"kind"`
	URL         string `toml:"url"`
	// DropQueryParams are the query parameters ignored in the links of the Subscription's Content.
	DropQueryParams []string `toml:"drop_query_params"`
}

type rawCastProfile struct {
	DisplayName   string                     `toml:"display_name"`
	Subscriptions map[string]rawSubscription `toml:"subscriptions"`
	MaxLookback   string                     `toml:"max_lookback"`
	WordBudget    *int                       `toml:"word_budget"`
	Prompt        string                     `toml:"prompt"`
	Language      string                     `toml:"language"`
	Voice         string                     `toml:"voice"`
	Enabled       *bool                      `toml:"enabled"`
	Schedules     []rawSchedule              `toml:"schedules"`
}

type rawSchedule struct {
	Weekdays []string `toml:"weekdays"`
	Time     string   `toml:"time"`
	Enabled  *bool    `toml:"enabled"`
}

// parsed is one valid file together with where its User Id was written.
type parsed struct {
	user  User
	idPos position
}

// parseFile decodes strictly, then validates what was decoded. The User is
// filled in even when there are problems, so the caller can still check its
// Id against other files; it is only usable when there are none.
func parseFile(file string, data []byte) (parsed, Problems) {
	slog.Debug("Parsing User Config", "file", file, "bytes", len(data))
	var raw rawFile
	err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)

	loc := newLocator(data)
	var problems Problems
	add := func(pos position, format string, args ...any) {
		problems = append(problems, Problem{File: file, Line: pos.line, Column: pos.column, Message: fmt.Sprintf(format, args...)})
	}

	var strict *toml.StrictMissingError
	var decode *toml.DecodeError
	switch {
	case errors.As(err, &strict):
		for i := range strict.Errors {
			row, col := strict.Errors[i].Position()
			add(position{row, col}, "unknown key %q", strings.Join(strict.Errors[i].Key(), "."))
		}
	case errors.As(err, &decode) && len(loc.repeated) > 0 && strings.Contains(decode.Error(), "already exists"):
		for _, r := range loc.repeated {
			add(r.pos, "%s", repeatedMessage(r.path))
		}
		return parsed{}, problems
	case errors.As(err, &decode):
		row, col := decode.Position()
		key := decode.Key()
		if len(key) == 0 {
			key = loc.byLine[row]
		}
		if len(key) > 0 {
			add(position{row, col}, "invalid value for %q: %s", strings.Join(key, "."), strings.TrimPrefix(decode.Error(), "toml: "))
		} else {
			add(position{row, col}, "%s", decode.Error())
		}
		return parsed{}, problems
	case len(loc.repeated) > 0:
		for _, r := range loc.repeated {
			add(r.pos, "%s", repeatedMessage(r.path))
		}
		return parsed{}, problems
	case err != nil:
		add(position{}, "%v", err)
		return parsed{}, problems
	}

	user := validate(&raw, loc, add)
	slog.Debug("Validated User Config", "file", file, "problems", len(problems))
	return parsed{user: user, idPos: loc.at("user", "id")}, problems
}

// repeatedMessage says that a table was defined twice, naming the Id when it is one.
func repeatedMessage(path []string) string {
	if len(path) == 4 && path[0] == "cast_profiles" && path[2] == "subscriptions" {
		return fmt.Sprintf("Subscription Id %q of CastProfile %q is defined twice", path[3], path[1])
	}
	if len(path) == 2 && path[0] == "cast_profiles" {
		return fmt.Sprintf("CastProfile Id %q is defined twice", path[1])
	}
	return fmt.Sprintf("table %q is defined twice", strings.Join(path, "."))
}

// validate checks the decoded values and builds the User. Every mistake is
// reported through add and checking goes on, so one run shows them all.
func validate(raw *rawFile, loc *locator, add func(position, string, ...any)) User {
	user := User{ID: raw.User.ID, DisplayName: raw.User.DisplayName}
	switch {
	case raw.User == rawUser{}:
		add(position{1, 1}, "the [user] section with id and display_name is missing")
	case raw.User.ID == "":
		add(loc.at("user"), "the [user] section needs an id")
	default:
		checkID(add, loc.at("user", "id"), "User", raw.User.ID)
	}
	if raw.User != (rawUser{}) && raw.User.DisplayName == "" {
		add(loc.at("user"), "the [user] section needs a display_name")
	}

	for _, id := range orderedIDs(loc.order["cast_profiles"], raw.CastProfiles) {
		user.CastProfiles = append(user.CastProfiles, validateCastProfile(id, raw.CastProfiles[id], loc, add))
	}
	return user
}

// validateCastProfile checks one CastProfile and its Schedules.
func validateCastProfile(id string, cp rawCastProfile, loc *locator, add func(position, string, ...any)) CastProfile {
	checkID(add, loc.at("cast_profiles", id), "CastProfile", id)
	if cp.DisplayName == "" {
		add(loc.at("cast_profiles", id), "CastProfile %q needs a display_name", id)
	}
	if len(cp.Subscriptions) == 0 {
		add(loc.at("cast_profiles", id), "CastProfile %q needs at least one Subscription, for example [cast_profiles.%s.subscriptions.news]", id, id)
	}
	lookback := validateMaxLookback(id, cp.MaxLookback, loc, add)
	wordBudget := validateWordBudget(id, cp.WordBudget, loc, add)

	out := CastProfile{
		ID: id, DisplayName: cp.DisplayName, MaxLookback: lookback, WordBudget: wordBudget,
		Prompt: cp.Prompt, Language: cp.Language, Voice: cp.Voice, Enabled: enabled(cp.Enabled),
	}
	for _, subID := range orderedIDs(loc.order[pathKey("cast_profiles", id, "subscriptions")], cp.Subscriptions) {
		out.Subscriptions = append(out.Subscriptions, validateSubscription(id, subID, cp.Subscriptions[subID], loc, add))
	}
	for i, s := range cp.Schedules {
		out.Schedules = append(out.Schedules, validateSchedule(id, i, s, loc, add))
	}
	return out
}

// validateMaxLookback reads the Max Lookback of a CastProfile: a duration in Go notation, greater than zero.
func validateMaxLookback(profileID, text string, loc *locator, add func(position, string, ...any)) time.Duration {
	if text == "" {
		add(loc.at("cast_profiles", profileID), "CastProfile %q needs a max_lookback, a duration such as \"24h\" or \"72h\"", profileID)
		return 0
	}
	at := loc.at("cast_profiles", profileID, "max_lookback")
	lookback, err := time.ParseDuration(text)
	slog.Debug("Read max_lookback", "profile", profileID, "text", text, "duration", lookback, "error", err)
	if err != nil {
		add(at, "CastProfile %q has max_lookback %q, expected a duration such as \"24h\" or \"72h\"", profileID, text)
		return 0
	}
	if lookback <= 0 {
		add(at, "CastProfile %q has max_lookback %q, which must be greater than zero", profileID, text)
		return 0
	}
	return lookback
}

// validateWordBudget reads the Word Budget of a CastProfile: a number of words greater than zero, or zero when the
// CastProfile states none.
func validateWordBudget(profileID string, words *int, loc *locator, add func(position, string, ...any)) int {
	if words == nil {
		return 0
	}
	slog.Debug("Read word_budget", "profile", profileID, "words", *words)
	if *words <= 0 {
		add(loc.at("cast_profiles", profileID, "word_budget"), "CastProfile %q has word_budget %d, which must be greater than zero", profileID, *words)
		return 0
	}
	return *words
}

// validateSubscription checks one Subscription of the CastProfile.
func validateSubscription(profileID, id string, sub rawSubscription, loc *locator, add func(position, string, ...any)) Subscription {
	where := func(key ...string) position {
		return loc.at(append([]string{"cast_profiles", profileID, "subscriptions", id}, key...)...)
	}
	checkID(add, where(), "Subscription", id)
	if sub.DisplayName == "" {
		add(where(), "Subscription %q of CastProfile %q needs a display_name", id, profileID)
	}
	switch SubscriptionKind(sub.Kind) {
	case "":
		add(where(), "Subscription %q of CastProfile %q needs a kind, expected \"rss\"", id, profileID)
	case KindRSS:
		if u, err := url.Parse(sub.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add(where("url"), "Subscription %q of CastProfile %q needs a url with an http or https address, got %q", id, profileID, sub.URL)
		}
	default:
		add(where("kind"), "Subscription %q of CastProfile %q has unknown kind %q, expected \"rss\"", id, profileID, sub.Kind)
	}
	for _, name := range sub.DropQueryParams {
		if name == "" {
			add(where("drop_query_params"), "Subscription %q of CastProfile %q lists an empty name in drop_query_params", id, profileID)
		}
	}
	return Subscription{ID: id, DisplayName: sub.DisplayName, Kind: SubscriptionKind(sub.Kind), URL: sub.URL, DropQueryParams: sub.DropQueryParams}
}

// validateSchedule checks the weekdays and the time of day of one Schedule.
func validateSchedule(profileID string, n int, s rawSchedule, loc *locator, add func(position, string, ...any)) Schedule {
	base := []string{"cast_profiles", profileID, "schedules", element(n)}
	out := Schedule{Enabled: enabled(s.Enabled)}

	if len(s.Weekdays) == 0 {
		add(loc.at(append(base, "weekdays")...), "a Schedule of CastProfile %q needs weekdays, for example [\"mon\", \"fri\"]", profileID)
	}
	for _, name := range s.Weekdays {
		day, ok := weekdays[name]
		if !ok {
			add(loc.at(append(base, "weekdays")...), "unknown weekday %q, expected mon, tue, wed, thu, fri, sat or sun", name)
			continue
		}
		out.Weekdays = append(out.Weekdays, day)
	}

	t, err := time.Parse("15:04", s.Time)
	if err != nil {
		add(loc.at(append(base, "time")...), "a Schedule needs a time of day as HH:MM, got %q", s.Time)
	}
	out.Hour, out.Minute = t.Hour(), t.Minute()
	return out
}

// checkID reports an Id that is not a lowercase slug.
func checkID(add func(position, string, ...any), pos position, what, id string) {
	if id == "" {
		add(pos, "%s needs an Id", what)
		return
	}
	if !slug.MatchString(id) {
		add(pos, "%s Id %q must only contain lowercase letters, digits, underscore and hyphen", what, id)
	}
}

// enabled applies the default of switches that are on unless they say otherwise.
func enabled(v *bool) bool { return v == nil || *v }

// orderedIDs returns every key of decoded in the order it was written. Keys
// the locator did not see, because they sit in an inline table, follow sorted.
func orderedIDs[V any](written []string, decoded map[string]V) []string {
	ids := make([]string, 0, len(decoded))
	seen := map[string]bool{}
	for _, id := range written {
		if _, ok := decoded[id]; ok && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	var rest []string
	for id := range decoded {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(ids, rest...)
}
