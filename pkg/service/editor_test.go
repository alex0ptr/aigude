package service_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const commentedAlex = "# my shows\n" + alexFile + "\n# end of file\n"

func startEditing(t *testing.T) *servicetest.Harness {
	t.Helper()
	return servicetest.Start(t,
		servicetest.WithUserConfigFile("alex.toml", commentedAlex),
		servicetest.WithUserConfigFile("bea.toml", beaFile))
}

// openEditor returns the page of the editor and the hash it carries.
func openEditor(t *testing.T, h *servicetest.Harness, headers map[string]string) (servicetest.Page, string) {
	t.Helper()
	page := h.GetWithHeaders(t, "/config", headers)
	require.Equal(t, http.StatusOK, page.Status)
	hash := regexp.MustCompile(`name="hash" value="([0-9a-f]+)"`).FindStringSubmatch(page.Body)
	require.NotNil(t, hash, "the editor remembers the content it was opened with")
	return page, hash[1]
}

func save(t *testing.T, h *servicetest.Harness, text, hash string) servicetest.Page {
	t.Helper()
	// Browsers send the lines of a textarea separated by CRLF.
	return h.Send(t, http.MethodPost, "/config", nil, url.Values{"text": {strings.ReplaceAll(text, "\n", "\r\n")}, "hash": {hash}})
}

func fileText(t *testing.T, h *servicetest.Harness, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.Settings.UsersDir, name))
	require.NoError(t, err)
	return string(data)
}

func TestEditorShowsTheFileOfTheSelectedUserExactlyAsItIsOnDisk(t *testing.T) {
	h := startEditing(t)

	page, _ := openEditor(t, h, nil)

	assert.Contains(t, page.Body, "# my shows")
	assert.Contains(t, page.Body, "# end of file")
	assert.Contains(t, page.Body, "alex.toml")
	assert.NotContains(t, page.Body, `id = &#34;bea&#34;`, "only the selected User's file")
	h.SelectUser(t, "bea")
	other, _ := openEditor(t, h, nil)
	assert.Contains(t, other.Body, `id = &#34;bea&#34;`)
}

func TestTheEditorLoadsTheScriptThatHighlightsItsText(t *testing.T) {
	h := startEditing(t)

	page, _ := openEditor(t, h, nil)
	script := h.Get(t, "/static/config-editor.js")

	assert.Contains(t, page.Body, "/static/config-editor.js")
	assert.Equal(t, http.StatusOK, script.Status)
	assert.Contains(t, script.Body, "config-text", "the script decorates the textarea of the editor")
}

func TestSavingValidTextWritesTheFileKeepsCommentsAndShowsItAtOnce(t *testing.T) {
	h := startEditing(t)
	_, hash := openEditor(t, h, nil)
	edited := strings.Replace(commentedAlex, "Morning briefing", "Early bird show", 1)

	saved := save(t, h, edited, hash)

	assert.Equal(t, http.StatusOK, saved.Status)
	assert.Equal(t, edited, fileText(t, h, "alex.toml"), "comments and order stay, line endings are the file's")
	assert.Contains(t, h.Get(t, "/").Body, "Early bird show", "no waiting for the file watcher")
	assert.Contains(t, saved.Body, "Saved")
}

func TestSavingTextWithAMistakeShowsItsPositionAndWritesNothing(t *testing.T) {
	h := startEditing(t)
	_, hash := openEditor(t, h, nil)
	broken := commentedAlex + "colour = \"blue\"\n"

	saved := save(t, h, broken, hash)

	assert.Equal(t, http.StatusUnprocessableEntity, saved.Status)
	assert.Regexp(t, `alex\.toml:\d+:\d+: unknown key`, saved.Body)
	assert.Contains(t, saved.Body, "colour", "the edited text stays in the editor")
	assert.Equal(t, commentedAlex, fileText(t, h, "alex.toml"))
}

func TestSavingAfterTheFileChangedOnDiskWarnsInsteadOfOverwriting(t *testing.T) {
	h := startEditing(t)
	_, hash := openEditor(t, h, nil)
	outside := commentedAlex + "# written by an editor\n"
	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", outside)

	saved := save(t, h, strings.Replace(commentedAlex, "Morning briefing", "Mine", 1), hash)

	assert.Equal(t, http.StatusConflict, saved.Status)
	assert.Contains(t, saved.Body, "changed on disk")
	assert.Equal(t, outside, fileText(t, h, "alex.toml"))
}

func TestPinnedUserEditsOnlyTheirOwnFile(t *testing.T) {
	h := startEditing(t)

	page, _ := openEditor(t, h, pinned("bea"))

	assert.Contains(t, page.Body, "bea.toml")
	assert.NotContains(t, page.Body, "# my shows")
}

func TestVisitorsAreRefusedTheEditorAndDoNotSeeALinkToIt(t *testing.T) {
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", commentedAlex), withDemoUser())

	editor := h.GetWithHeaders(t, "/config", visitor())
	saving := h.PostWithHeaders(t, "/config", visitor())
	index := h.GetWithHeaders(t, "/", visitor())

	assert.Equal(t, http.StatusForbidden, editor.Status)
	assert.Equal(t, http.StatusForbidden, saving.Status)
	assert.NotContains(t, index.Body, `href="/config"`)
	assert.Contains(t, h.Get(t, "/").Body, `href="/config"`, "the person running the service finds it")
}
