package service_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alexFile = `
[user]
id = "alex"
display_name = "Alex"

[cast_profiles.morning]
display_name = "Morning briefing"
max_lookback = "24h"

[cast_profiles.morning.subscriptions.heise]
display_name = "heise online"
kind = "rss"
url = "https://example.org/heise.xml"
`

const beaFile = `
[user]
id = "bea"
display_name = "Bea"

[cast_profiles.evening]
display_name = "Evening recap"
max_lookback = "24h"

[cast_profiles.evening.subscriptions.tagesschau]
display_name = "Tagesschau"
kind = "rss"
url = "https://example.org/tagesschau.xml"
`

const demoFile = `
[user]
id = "demo"
display_name = "Demo"

[cast_profiles.morning]
display_name = "Morning briefing"
max_lookback = "24h"

[cast_profiles.morning.subscriptions.heise]
display_name = "heise online"
kind = "rss"
url = "https://example.org/heise.xml"
`

// withDemoUser adds the demo User, whose pages are shown in Demo Mode.
func withDemoUser() servicetest.Option {
	return servicetest.WithUserConfigFile("demo.toml", demoFile)
}

func twoUsers() []servicetest.Option {
	return []servicetest.Option{
		servicetest.WithUserConfigFile("zzz-first-file.toml", alexFile),
		servicetest.WithUserConfigFile("a-second-file.toml", beaFile),
	}
}

func TestPageShowsTheFirstUsersCastProfilesAndSubscriptionsByDisplayName(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	page := h.Get(t, "/")

	assert.Contains(t, page.Body, "Morning briefing")
	assert.Contains(t, page.Body, "heise online")
	assert.NotContains(t, page.Body, "Evening recap", "the other User's CastProfiles stay out")
	assert.NotContains(t, page.Body, "Tagesschau")
	assert.NotContains(t, page.Body, ">morning<", "Ids are not what the interface shows")
}

func TestChosenUserIsRememberedAcrossRequests(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	landed := h.SelectUser(t, "bea")
	later := h.Get(t, "/")

	for _, page := range []servicetest.Page{landed, later} {
		assert.Equal(t, http.StatusOK, page.Status)
		assert.Contains(t, page.Body, "Evening recap")
		assert.Contains(t, page.Body, "Tagesschau")
		assert.NotContains(t, page.Body, "Morning briefing")
		assert.Regexp(t, regexp.MustCompile(`<option[^>]*value="bea"[^>]*selected`), page.Body)
	}
}

func TestUnknownRememberedUserFallsBackToTheFirstUser(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	h.SetCookie(t, "aigude_user", "gone")

	page := h.Get(t, "/")

	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Body, "Morning briefing")
}

func TestDisplayNamesAreEscaped(t *testing.T) {
	file := `[user]
id = "mallory"
display_name = "<script>alert(1)</script>"
`
	h := servicetest.Start(t, servicetest.WithUserConfigFile("m.toml", file))

	page := h.Get(t, "/")

	assert.NotContains(t, page.Body, "<script>alert(1)</script>")
	assert.Contains(t, page.Body, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestUserWithoutCastProfilesGetsAHint(t *testing.T) {
	file := "[user]\nid = \"new\"\ndisplay_name = \"Newcomer\"\n"
	h := servicetest.Start(t, servicetest.WithUserConfigFile("n.toml", file))

	page := h.Get(t, "/")

	assert.Contains(t, page.Body, "Newcomer")
	assert.Contains(t, page.Body, "No CastProfiles yet")
}

func TestServiceRefusesToStartWithAnInvalidUserConfigFile(t *testing.T) {
	cfg := servicetest.Settings(t)
	servicetest.WriteUserConfig(t, cfg, "broken.toml", alexFile+"colour = \"blue\"\n")

	_, err := service.New(cfg, &servicetest.FakeScriptWriter{}, &servicetest.FakeTTS{}, servicetest.NewFakeCrawler(), servicetest.NewFakeClock(), servicetest.NewLogs())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken.toml:")
	assert.Contains(t, err.Error(), "unknown key")
}

func TestServiceRefusesToStartWhenTheUsersFolderIsMissing(t *testing.T) {
	cfg := servicetest.Settings(t)
	cfg.UsersDir += "-missing"

	_, err := service.New(cfg, &servicetest.FakeScriptWriter{}, &servicetest.FakeTTS{}, servicetest.NewFakeCrawler(), servicetest.NewFakeClock(), servicetest.NewLogs())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "users folder")
}
