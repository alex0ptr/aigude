package business

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSourceNumbersCountFromOneAndOutsideTheEpisodeIsNoSource(t *testing.T) {
	episode := Episode{Sources: []Source{{Title: "first"}, {Title: "second"}}}

	for number, want := range map[int]string{1: "first", 2: "second", 0: "", 3: "", -1: ""} {
		got, ok := episode.Source(number)

		assert.Equal(t, want != "", ok, number)
		assert.Equal(t, want, got.Title, number)
	}
}
