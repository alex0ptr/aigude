package pkg

import (
	"net/http"
	"time"

	"github.com/alex0ptr/aigude/pkg/version"
)

// UserAgent is how AI Gude introduces itself to the servers it fetches from.
var UserAgent = "aigude/" + version.String()

// UserAgentTransport wraps an http.RoundTripper and sets the aigude User-Agent
// header on every outgoing request, overriding any User-Agent the caller set.
type UserAgentTransport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *UserAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", UserAgent)
	return base.RoundTrip(req)
}

// NewHTTPClient creates an http.Client with the given timeout that identifies
// itself as UserAgent on every request. Adapters fetching external content
// use it for their requests.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &UserAgentTransport{},
	}
}
