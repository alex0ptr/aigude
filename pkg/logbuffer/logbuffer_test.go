package logbuffer_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/alex0ptr/aigude/pkg/logbuffer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinesHoldLevelMessageAndAttributes(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)

	slog.New(buffer).Info("Episode requested", "user", "alex", "waiting", 2)

	lines := buffer.Lines()
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "INFO")
	assert.Contains(t, lines[0], "Episode requested")
	assert.Contains(t, lines[0], "user=alex")
	assert.Contains(t, lines[0], "waiting=2")
}

func TestOnlyTheNewestLinesAreKeptOldestFirst(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 3)
	logger := slog.New(buffer)

	for i := 1; i <= 5; i++ {
		logger.Info(fmt.Sprintf("line %d", i))
	}

	lines := buffer.Lines()
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "line 3")
	assert.Contains(t, lines[2], "line 5")
}

func TestRecordsBelowTheLevelOfTheNormalHandlerAreNotKept(t *testing.T) {
	var out bytes.Buffer
	buffer := logbuffer.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}), 10)

	slog.New(buffer).Debug("Details nobody asked for")

	assert.Empty(t, buffer.Lines())
}

func TestAttributesAndGroupsOfDerivedLoggersAreInTheLines(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)

	slog.New(buffer).With("component", "scheduler").WithGroup("run").Info("started", "profile", "morning")

	lines := buffer.Lines()
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "component=scheduler")
	assert.Contains(t, lines[0], "run.profile=morning")
}

type pair struct{ a, b int }

func (p pair) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("a", p.a), slog.Int("b", p.b))
}

func TestAValueThatLogsAsAGroupIsShownOneAttributePerLine(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)

	slog.New(buffer).Info("Episode ready", "usage", pair{a: 1, b: 2})

	lines := buffer.Lines()
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "usage.a=1")
	assert.Contains(t, lines[0], "usage.b=2")
}

func TestSinceReturnsOnlyTheLinesAfterTheCursor(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)
	logger := slog.New(buffer)
	logger.Info("line 1")
	_, cursor, _ := buffer.Since("")

	logger.Info("line 2")
	logger.Info("line 3")
	lines, next, reset := buffer.Since(cursor)

	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "line 2")
	assert.Contains(t, lines[1], "line 3")
	assert.False(t, reset)
	again, _, _ := buffer.Since(next)
	assert.Empty(t, again, "nothing new since the last cursor")
}

func TestSinceStartsOverWhenTheCursorStandsForNoPosition(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)
	slog.New(buffer).Info("kept")
	otherRun := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 10)
	_, cursorOfOtherRun, _ := otherRun.Since("")

	for name, cursor := range map[string]string{"empty": "", "unreadable": "nonsense", "of another run": cursorOfOtherRun} {
		lines, _, reset := buffer.Since(cursor)

		assert.True(t, reset, name)
		require.Len(t, lines, 1, name)
		assert.Contains(t, lines[0], "kept", name)
	}
}

func TestSinceReturnsOnlyKeptLinesWhenMoreWereAddedThanFit(t *testing.T) {
	buffer := logbuffer.New(slog.NewTextHandler(&bytes.Buffer{}, nil), 2)
	logger := slog.New(buffer)
	_, cursor, _ := buffer.Since("")

	for i := 1; i <= 5; i++ {
		logger.Info(fmt.Sprintf("line %d", i))
	}
	lines, _, reset := buffer.Since(cursor)

	assert.False(t, reset)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "line 4")
	assert.Contains(t, lines[1], "line 5")
}
