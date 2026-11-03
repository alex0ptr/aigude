package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validUser = `[user]
id = "alex"
display_name = "Alex"
`

// validateFolder runs `aigude validate` against a users folder holding the given files.
func validateFolder(t *testing.T, files map[string]string) (stdout, stderr string, err error) {
	t.Helper()
	t.Chdir(t.TempDir()) // no ./aigude.toml may leak in
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	var out, errOut bytes.Buffer
	cmd := newRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"validate", "--users-dir", dir})
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestValidateAcceptsAValidFolderWithoutStartingTheService(t *testing.T) {
	stdout, _, err := validateFolder(t, map[string]string{"alex.toml": validUser})

	require.NoError(t, err)
	assert.Contains(t, stdout, "1 User")
	assert.Contains(t, stdout, "Alex")
}

func TestValidateReportsEveryProblemWithFileLineAndColumnAndFails(t *testing.T) {
	_, stderr, err := validateFolder(t, map[string]string{
		"alex.toml": validUser + "colour = \"blue\"\n",
		"bea.toml":  "[user]\nid = \"Bea\"\ndisplay_name = \"Bea\"\n",
	})

	require.Error(t, err, "a non-nil error makes the process exit with a failure status")
	assert.Contains(t, stderr, "alex.toml:4:1: unknown key")
	assert.Contains(t, stderr, "bea.toml:2:1: User Id \"Bea\"")
}

func TestTheSampleUserConfigIsValid(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	cmd := newRootCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"validate", "--users-dir", filepath.Join(repoRoot(t), "hack", "users")})

	require.NoError(t, cmd.Execute())
}

// repoRoot finds the repository root from the test's source location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
