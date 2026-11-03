package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheVersionIsTheTagOrDevWithTheCommit(t *testing.T) {
	commit := "e2cf3b2c9d0f1a2b3c4d5e6f708192a3b4c5d6e7"
	tests := map[string]struct {
		tag      string
		settings []debug.BuildSetting
		want     string
	}{
		"a tag":                    {"0.1.0", nil, "0.1.0"},
		"a tag ignores the commit": {"0.1.0", []debug.BuildSetting{{Key: "vcs.revision", Value: commit}}, "0.1.0"},
		"dev with a commit":        {"dev", []debug.BuildSetting{{Key: "vcs.revision", Value: commit}, {Key: "vcs.modified", Value: "false"}}, "dev+e2cf3b2"},
		"dev with changes":         {"dev", []debug.BuildSetting{{Key: "vcs.revision", Value: commit}, {Key: "vcs.modified", Value: "true"}}, "dev+e2cf3b2-dirty"},
		"dev without a commit":     {"dev", nil, "dev"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, describe(tt.tag, tt.settings))
		})
	}
}
