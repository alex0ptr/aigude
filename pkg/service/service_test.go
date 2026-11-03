package service_test

import (
	"context"
	"net"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstPageShowsAnEmptyState(t *testing.T) {
	h := servicetest.Start(t)

	page := h.Get(t, "/")

	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Header.Get("Content-Type"), "text/html")
	assert.Contains(t, page.Body, "AI Gude")
	assert.Contains(t, page.Body, "Nothing to listen to yet")
	assert.Contains(t, page.Body, h.Settings.UsersDir, "the empty state tells where to put the first User Config")
}

func TestPageUsesEmbeddedPicoAndHtmxWithoutTouchingTheInternet(t *testing.T) {
	h := servicetest.Start(t)

	page := h.Get(t, "/")

	assert.NotRegexp(t, regexp.MustCompile(`(?i)(src|href)="(https?:)?//`), page.Body,
		"the page must not reference anything outside the service")
	for _, asset := range []string{"/static/pico.min.css", "/static/htmx.min.js"} {
		assert.Contains(t, page.Body, asset)
		got := h.Get(t, asset)
		assert.Equal(t, http.StatusOK, got.Status, asset)
		assert.NotEmpty(t, got.Body, asset)
	}
	assert.Contains(t, h.Get(t, "/static/pico.min.css").Header.Get("Content-Type"), "text/css")
}

func TestRunServesOnTheConfiguredAddress(t *testing.T) {
	cfg := servicetest.Settings(t)
	cfg.ListenAddr = freeAddr(t)
	svc, err := service.New(cfg, &servicetest.FakeScriptWriter{}, &servicetest.FakeTTS{}, servicetest.NewFakeCrawler(), servicetest.NewFakeClock(), servicetest.NewLogs())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + cfg.ListenAddr + "/")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "a requested shutdown is not an error")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}

func TestRunFailsClearlyWhenTheAddressIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()
	cfg := servicetest.Settings(t)
	cfg.ListenAddr = taken.Addr().String()
	svc, err := service.New(cfg, &servicetest.FakeScriptWriter{}, &servicetest.FakeTTS{}, servicetest.NewFakeCrawler(), servicetest.NewFakeClock(), servicetest.NewLogs())
	require.NoError(t, err)

	err = svc.Run(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), cfg.ListenAddr)
}

// freeAddr returns a loopback address that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}
