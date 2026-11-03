package userconfig_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alexConfig = `
[user]
id = "alex"
display_name = "Alex"

[cast_profiles.morning]
display_name = "Morning briefing"
max_lookback = "24h"
prompt = "Be brief."
language = "en-US"
voice = "Kore"
word_budget = 800

[cast_profiles.morning.subscriptions.heise]
display_name = "heise online"
kind = "rss"
url = "https://www.heise.de/rss/heise-atom.xml"

[cast_profiles.morning.subscriptions.tagesschau]
display_name = "Tagesschau"
kind = "rss"
url = "https://www.tagesschau.de/index~rss2.xml"

[[cast_profiles.morning.schedules]]
weekdays = ["mon", "tue", "wed", "thu", "fri"]
time = "08:00"

[cast_profiles.evening]
display_name = "Evening recap"
max_lookback = "72h"
enabled = false

[cast_profiles.evening.subscriptions.heise]
display_name = "heise evening"
kind = "rss"
url = "https://www.heise.de/rss/heise-atom.xml"
`

// writeFiles creates the given files (name to content) in a fresh users folder.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func TestLoadDirReadsAUsersSubscriptionsAndCastProfiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{"whatever-name.toml": alexConfig})

	snapshot, err := userconfig.LoadDir(dir)

	require.NoError(t, err)
	users := snapshot.Users()
	require.Len(t, users, 1)
	alex := users[0]
	assert.Equal(t, "alex", alex.ID, "the User is identified by the Id inside the file, not by the file name")
	assert.Equal(t, "Alex", alex.DisplayName)

	require.Len(t, alex.CastProfiles, 2)
	morning := alex.CastProfiles[0]
	assert.Equal(t, "morning", morning.ID)
	assert.Equal(t, "Morning briefing", morning.DisplayName)
	require.Len(t, morning.Subscriptions, 2)
	assert.Equal(t, userconfig.Subscription{
		ID: "heise", DisplayName: "heise online", Kind: userconfig.KindRSS, URL: "https://www.heise.de/rss/heise-atom.xml",
	}, morning.Subscriptions[0], "Subscriptions keep the order of the file")
	assert.Equal(t, "tagesschau", morning.Subscriptions[1].ID)
	assert.Equal(t, 24*time.Hour, morning.MaxLookback)
	assert.Equal(t, "Be brief.", morning.Prompt)
	assert.Equal(t, "en-US", morning.Language)
	assert.Equal(t, "Kore", morning.Voice)
	assert.Equal(t, 800, morning.WordBudget)
	assert.True(t, morning.Enabled, "a CastProfile is enabled unless it says otherwise")
	require.Len(t, morning.Schedules, 1)
	assert.Equal(t, userconfig.Schedule{
		Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		Hour:     8, Minute: 0, Enabled: true,
	}, morning.Schedules[0])

	evening := alex.CastProfiles[1]
	assert.False(t, evening.Enabled)
	assert.Equal(t, 72*time.Hour, evening.MaxLookback)
	assert.Zero(t, evening.WordBudget, "a CastProfile without a word_budget takes the service's")
	require.Len(t, evening.Subscriptions, 1)
	assert.Equal(t, "heise evening", evening.Subscriptions[0].DisplayName, "the same Id in another CastProfile is a Subscription of its own")
	assert.Empty(t, evening.Schedules)
}

// minimal is a valid file; the validation tests break one line of it.
const minimal = `[user]
id = "alex"
display_name = "Alex"

[cast_profiles.morning]
display_name = "Morning"
max_lookback = "24h"

[cast_profiles.morning.subscriptions.heise]
display_name = "heise"
kind = "rss"
url = "https://example.org/feed.xml"
`

// inProfile adds lines to the CastProfile of minimal, above its Subscription.
func inProfile(lines string) string {
	return strings.Replace(minimal, "max_lookback = \"24h\"\n", "max_lookback = \"24h\"\n"+lines, 1)
}

func TestLoadDirReportsEveryMistakeWithFileLineAndColumn(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		line    int
		column  int
		message string
	}{
		{"unknown key", "a.toml", inProfile("colour = \"blue\"\n"), 8, 1, `unknown key "cast_profiles.morning.colour"`},
		{"unknown key in a table", "a.toml",
			strings.Replace(minimal, `kind = "rss"`, "kind = \"rss\"\n  feed = \"x\"", 1), 12, 3, `unknown key "cast_profiles.morning.subscriptions.heise.feed"`},
		{"syntax error", "a.toml", minimal + "this is not toml\n", 13, 6, "expected '=' after key"},
		{"uppercase user id", "a.toml", strings.Replace(minimal, `id = "alex"`, `id = "Alex"`, 1), 2, 1, `User Id "Alex"`},
		{"subscription id with a space", "a.toml", strings.Replace(minimal, `[cast_profiles.morning.subscriptions.heise]`, `[cast_profiles.morning.subscriptions."my feed"]`, 1), 9, 2, `Subscription Id "my feed"`},
		{"cast profile id with umlaut", "a.toml", strings.Replace(minimal, `[cast_profiles.morning]`, `[cast_profiles."früh"]`, 1), 5, 2, `CastProfile Id "früh"`},
		{"duplicate subscription", "a.toml", minimal + "[cast_profiles.morning.subscriptions.heise]\n", 13, 2, `Subscription Id "heise" of CastProfile "morning" is defined twice`},
		{"duplicate cast profile and a value of the wrong type", "a.toml", strings.Replace(minimal, `"24h"`, `24`, 1) + "[cast_profiles.morning]\n", 7, 16, `max_lookback`},
		{"duplicate cast profile", "a.toml", minimal + "[cast_profiles.morning]\n", 13, 2, `CastProfile Id "morning" is defined twice`},
		{"max lookback that is no duration", "a.toml", strings.Replace(minimal, `"24h"`, `"yesterday"`, 1), 7, 1, `max_lookback "yesterday"`},
		{"max lookback of zero", "a.toml", strings.Replace(minimal, `"24h"`, `"0s"`, 1), 7, 1, `max_lookback "0s", which must be greater than zero`},
		{"negative max lookback", "a.toml", strings.Replace(minimal, `"24h"`, `"-1h"`, 1), 7, 1, `max_lookback "-1h", which must be greater than zero`},
		{"max lookback as a number", "a.toml", strings.Replace(minimal, `"24h"`, `24`, 1), 7, 16, `max_lookback`},
		{"word budget of zero", "a.toml", inProfile("word_budget = 0\n"), 8, 1, `word_budget 0, which must be greater than zero`},
		{"negative word budget", "a.toml", inProfile("word_budget = -1\n"), 8, 1, `word_budget -1, which must be greater than zero`},
		{"word budget that is no number", "a.toml", inProfile("word_budget = \"many\"\n"), 8, 15, `word_budget`},
		{"the old time_profile key", "a.toml", inProfile("time_profile = \"24h\"\n"), 8, 1, `unknown key "cast_profiles.morning.time_profile"`},
		{"bad weekday", "a.toml", minimal + "[[cast_profiles.morning.schedules]]\nweekdays = [\"mon\", \"funday\"]\ntime = \"08:00\"\n", 14, 1, `unknown weekday "funday"`},
		{"bad time of day", "a.toml", minimal + "[[cast_profiles.morning.schedules]]\nweekdays = [\"mon\"]\ntime = \"25:99\"\n", 15, 1, `HH:MM, got "25:99"`},
		{"second schedule is the broken one", "a.toml", minimal +
			"[[cast_profiles.morning.schedules]]\nweekdays = [\"mon\"]\ntime = \"08:00\"\n[[cast_profiles.morning.schedules]]\nweekdays = [\"mon\"]\ntime = \"8am\"\n", 18, 1, `got "8am"`},
		{"bad feed address", "a.toml", strings.Replace(minimal, "https://example.org/feed.xml", "ftp://example.org", 1), 12, 1, "http or https"},
		{"unknown subscription kind", "a.toml", strings.Replace(minimal, `"rss"`, `"podcast"`, 1), 11, 1, `unknown kind "podcast"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{tt.file: tt.content})

			_, err := userconfig.LoadDir(dir)

			var problems userconfig.Problems
			require.ErrorAs(t, err, &problems)
			require.NotEmpty(t, problems)
			p := problems[0]
			assert.Equal(t, filepath.Join(dir, tt.file), p.File, "the problem names its file")
			assert.Equal(t, tt.line, p.Line, "line of %s", p)
			if tt.column != 0 {
				assert.Equal(t, tt.column, p.Column, "column of %s", p)
			}
			assert.Contains(t, p.Message, tt.message)
			assert.Contains(t, err.Error(), tt.file+":"+strconv.Itoa(tt.line))
		})
	}
}

func TestLoadDirReportsDuplicateUserIdsAcrossFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.toml": minimal, "b.toml": minimal})

	_, err := userconfig.LoadDir(dir)

	var problems userconfig.Problems
	require.ErrorAs(t, err, &problems)
	require.Len(t, problems, 1)
	assert.Equal(t, filepath.Join(dir, "b.toml"), problems[0].File, "the later file is the one at fault")
	assert.Equal(t, 2, problems[0].Line)
	assert.Contains(t, problems[0].Message, `User Id "alex" is already used by `+filepath.Join(dir, "a.toml"))
}

func TestLoadDirReportsTheProblemsOfAllFilesAtOnce(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.toml": strings.Replace(minimal, `"24h"`, `"never"`, 1),
		"b.toml": strings.Replace(strings.Replace(minimal, `"alex"`, `"bea"`, 1), `"24h"`, `"soon"`, 1),
	})

	_, err := userconfig.LoadDir(dir)

	var problems userconfig.Problems
	require.ErrorAs(t, err, &problems)
	assert.Len(t, problems, 2)
}

func TestLoadDirIgnoresEverythingThatIsNotAConfigFileDirectlyInTheFolder(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"alex.toml":      minimal,
		".hidden.toml":   "not toml at all",
		".#alex.toml":    "not toml at all",
		"alex.toml~":     "not toml at all",
		"#alex.toml#":    "not toml at all",
		"notes.txt":      "not toml at all",
		"sub/other.toml": "not toml at all",
		"alex.toml.swp":  "not toml at all",
	})

	snapshot, err := userconfig.LoadDir(dir)

	require.NoError(t, err)
	assert.Len(t, snapshot.Users(), 1)
}

func TestLoadDirFailsClearlyWhenTheFolderIsMissing(t *testing.T) {
	_, err := userconfig.LoadDir(filepath.Join(t.TempDir(), "missing"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "users folder")
	assert.Contains(t, err.Error(), "missing")
}

func TestUsersAreOrderedByDisplayName(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"1.toml": strings.Replace(strings.Replace(minimal, `"alex"`, `"zed"`, 1), `"Alex"`, `"Zed"`, 1),
		"2.toml": strings.Replace(strings.Replace(minimal, `"alex"`, `"bea"`, 1), `"Alex"`, `"bea"`, 1),
		"3.toml": minimal,
	})

	snapshot, err := userconfig.LoadDir(dir)

	require.NoError(t, err)
	var names []string
	for _, u := range snapshot.Users() {
		names = append(names, u.DisplayName)
	}
	assert.Equal(t, []string{"Alex", "bea", "Zed"}, names)
	bea, ok := snapshot.User("bea")
	assert.True(t, ok)
	assert.Equal(t, "bea", bea.DisplayName)
	_, ok = snapshot.User("nobody")
	assert.False(t, ok)
}

func TestLoadDirNamesWhatIsMissing(t *testing.T) {
	tests := []struct {
		name    string
		content string
		message string
	}{
		{"no user section", "# nothing yet\n", "[user] section"},
		{"no user id", strings.Replace(minimal, `id = "alex"`, "", 1), "needs an id"},
		{"no user display name", strings.Replace(minimal, `display_name = "Alex"`, "", 1), "needs a display_name"},
		{"no subscription kind", strings.Replace(minimal, `kind = "rss"`, "", 1), "needs a kind"},
		{"no max lookback", strings.Replace(minimal, `max_lookback = "24h"`, "", 1), "needs a max_lookback"},
		{"no subscriptions in a cast profile", strings.Split(minimal, "\n[cast_profiles.morning.subscriptions")[0] + "\n", "at least one Subscription"},
		{"schedule without weekdays", minimal + "[[cast_profiles.morning.schedules]]\ntime = \"08:00\"\n", "needs weekdays"},
		{"wrongly typed value", inProfile("enabled = \"yes\"\n"), "enabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"a.toml": tt.content})

			_, err := userconfig.LoadDir(dir)

			var problems userconfig.Problems
			require.ErrorAs(t, err, &problems)
			assert.Contains(t, problems[0].Message, tt.message)
			assert.NotZero(t, problems[0].Line)
		})
	}
}

func TestSwitchedOffSchedulesAreKept(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.toml": minimal + "[[cast_profiles.morning.schedules]]\nweekdays = [\"sat\"]\ntime = \"09:30\"\nenabled = false\n"})

	snapshot, err := userconfig.LoadDir(dir)

	require.NoError(t, err)
	alex, _ := snapshot.User("alex")
	require.Len(t, alex.CastProfiles[0].Schedules, 1)
	assert.False(t, alex.CastProfiles[0].Schedules[0].Enabled)
	assert.Equal(t, 9, alex.CastProfiles[0].Schedules[0].Hour)
	assert.Equal(t, 30, alex.CastProfiles[0].Schedules[0].Minute)
}

func TestDuplicateUserIdIsReportedEvenWhenTheFileHasOtherProblems(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.toml": minimal, "b.toml": minimal + "colour = \"blue\"\n"})

	_, err := userconfig.LoadDir(dir)

	var problems userconfig.Problems
	require.ErrorAs(t, err, &problems)
	assert.Len(t, problems, 2, "the unknown key and the duplicate Id show up in one run")
}

func TestAScheduleIsDueAtItsWallClockTimeAlsoWhenDaylightSavingTimeEnds(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	daily := userconfig.Schedule{
		Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
		Hour:     8, Minute: 0, Enabled: true,
	}

	// Saturday 24 October 2026, 08:30 in Berlin; the clocks go back during the night.
	next := daily.Next(time.Date(2026, time.October, 24, 6, 30, 0, 0, time.UTC), berlin)

	assert.True(t, next.Equal(time.Date(2026, time.October, 25, 7, 0, 0, 0, time.UTC)), "08:00 is 07:00 UTC after the change, no longer 06:00, got %s", next.UTC())
}

func TestAScheduleSkipsTheDaysItDoesNotNameAndNeverMeetsItsOwnInstantTwice(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	fridays := userconfig.Schedule{Weekdays: []time.Weekday{time.Friday}, Hour: 8, Minute: 30, Enabled: true}
	friday := time.Date(2026, time.October, 30, 8, 30, 0, 0, berlin)

	assert.True(t, friday.Equal(fridays.Next(time.Date(2026, time.October, 29, 8, 0, 0, 0, berlin), berlin)), "from Thursday to Friday")
	assert.True(t, friday.AddDate(0, 0, 7).Equal(fridays.Next(friday, berlin)), "at the instant itself, the next one is a week later")
}

func TestAScheduleKeepsItsWallClockTimeWhenDaylightSavingTimeStarts(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	daily := userconfig.Schedule{
		Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
		Hour:     8, Minute: 0, Enabled: true,
	}

	// Saturday 28 March 2026, 08:30 in Berlin; the clocks go forward during the night.
	next := daily.Next(time.Date(2026, time.March, 28, 7, 30, 0, 0, time.UTC), berlin)

	assert.True(t, next.Equal(time.Date(2026, time.March, 29, 6, 0, 0, 0, time.UTC)), "08:00 is 06:00 UTC after the change, no longer 07:00, got %s", next.UTC())
}

func TestAScheduleInTheHourThatDoesNotExistIsDueOnceThatDay(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	night := userconfig.Schedule{
		Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
		Hour:     2, Minute: 30, Enabled: true,
	}

	// 02:30 does not exist on Sunday 29 March 2026 in Berlin.
	first := night.Next(time.Date(2026, time.March, 29, 0, 0, 0, 0, berlin), berlin)
	second := night.Next(first, berlin)

	assert.Equal(t, 29, first.In(berlin).Day(), "it is due on that day, at the closest time there is")
	assert.Equal(t, 30, second.In(berlin).Day(), "and not a second time that day")
	assert.True(t, second.Equal(time.Date(2026, time.March, 30, 2, 30, 0, 0, berlin)))
}

func TestASubscriptionListsTheQueryParametersToIgnore(t *testing.T) {
	file := strings.Replace(minimal, `kind = "rss"`, "kind = \"rss\"\ndrop_query_params = [\"wt_mc\", \"utm_source\"]", 1)
	dir := writeFiles(t, map[string]string{"a.toml": file})

	snapshot, err := userconfig.LoadDir(dir)

	require.NoError(t, err)
	alex, _ := snapshot.User("alex")
	assert.Equal(t, []string{"wt_mc", "utm_source"}, alex.CastProfiles[0].Subscriptions[0].DropQueryParams)
}

func TestDropQueryParamsThatAreNotAListOfNamesAreReportedWithPosition(t *testing.T) {
	tests := []struct {
		name, value string
		message     string
	}{
		{"a single string", `"wt_mc"`, "drop_query_params"},
		{"numbers", `[1, 2]`, "drop_query_params"},
		{"an empty name", `["wt_mc", ""]`, "empty name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := strings.Replace(minimal, `kind = "rss"`, "kind = \"rss\"\ndrop_query_params = "+tt.value, 1)
			dir := writeFiles(t, map[string]string{"a.toml": file})

			_, err := userconfig.LoadDir(dir)

			var problems userconfig.Problems
			require.ErrorAs(t, err, &problems)
			assert.Contains(t, problems[0].Message, tt.message)
			assert.Equal(t, 12, problems[0].Line, "the line of the key")
			assert.NotZero(t, problems[0].Column)
		})
	}
}
