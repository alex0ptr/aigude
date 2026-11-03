package service_test

import (
	"strings"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
)

func pageContains(text string) func(servicetest.Page) bool {
	return func(p servicetest.Page) bool { return strings.Contains(p.Body, text) }
}

func TestAddingAFileMakesANewUserAppear(t *testing.T) {
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", alexFile))
	assert.NotContains(t, h.Get(t, "/").Body, "Bea")

	servicetest.WriteUserConfig(t, h.Settings, "bea.toml", beaFile)

	h.WaitFor(t, "/", "Bea in the dropdown", pageContains(`value="bea"`))
}

func TestEditingAFileChangesWhatTheUserSees(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	assert.Contains(t, h.Get(t, "/").Body, "Morning briefing")

	servicetest.WriteUserConfig(t, h.Settings, "zzz-first-file.toml",
		strings.Replace(alexFile, "Morning briefing", "Early bird show", 1))

	page := h.WaitFor(t, "/", "the renamed CastProfile", pageContains("Early bird show"))
	assert.NotContains(t, page.Body, "Morning briefing")
}

func TestRemovingAFileRemovesTheUserFromTheDropdown(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	h.SelectUser(t, "bea")
	assert.Contains(t, h.Get(t, "/").Body, "Evening recap")

	servicetest.RemoveUserConfig(t, h.Settings, "a-second-file.toml")

	page := h.WaitFor(t, "/", "Bea to vanish", func(p servicetest.Page) bool {
		return !strings.Contains(p.Body, `value="bea"`)
	})
	assert.Contains(t, page.Body, "Morning briefing", "the remembered User is gone, so the first User is shown")
	assert.Equal(t, 200, page.Status)
}

func TestAnInvalidFileKeepsTheLastValidSetupAndShowsABanner(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	assert.NotContains(t, h.Get(t, "/").Body, "Problem in the User Config")

	servicetest.WriteUserConfig(t, h.Settings, "zzz-first-file.toml", strings.Replace(alexFile, `"24h"`, `"never"`, 1))

	page := h.WaitFor(t, "/", "the banner", pageContains("zzz-first-file.toml:"))
	assert.Contains(t, page.Body, "never", "the problem says what is wrong")
	assert.Contains(t, page.Body, "Morning briefing", "Alex keeps the last valid setup")
}

func TestFixingTheFileClearsTheBanner(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	servicetest.WriteUserConfig(t, h.Settings, "zzz-first-file.toml", strings.Replace(alexFile, `"24h"`, `"never"`, 1))
	h.WaitFor(t, "/", "the banner", pageContains("zzz-first-file.toml:"))

	servicetest.WriteUserConfig(t, h.Settings, "zzz-first-file.toml",
		strings.Replace(alexFile, "Morning briefing", "Fixed briefing", 1))

	page := h.WaitFor(t, "/", "the banner to go", func(p servicetest.Page) bool {
		return strings.Contains(p.Body, "Fixed briefing") && !strings.Contains(p.Body, "Problem in the User Config")
	})
	assert.NotContains(t, page.Body, "zzz-first-file.toml")
}
