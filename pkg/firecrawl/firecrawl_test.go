package firecrawl

import (
	"encoding/json"
	"github.com/alex0ptr/aigude/business"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serving answers every request with the status and body and returns the client for it and the last request.
func serving(t *testing.T, status int, body string) (*Client, *http.Request, *[]byte) {
	t.Helper()
	return servingWithKey(t, "secret-key", status, body)
}

// servingWithKey is serving for a client with the API key; an empty key means a client without one.
func servingWithKey(t *testing.T, apiKey string, status int, body string) (*Client, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(r.Context())
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(ClientOpts{APIKey: apiKey, BaseURL: srv.URL}), &got, &gotBody
}

func TestCrawlAsksTheScrapeEndpointForMarkdown(t *testing.T) {
	client, request, body := serving(t, 200, `{"success":true,"data":{"markdown":"# Hallo","metadata":{"title":"Titel"}}}`)

	_, _, err := client.Crawl("https://example.org/artikel")

	require.NoError(t, err)
	assert.Equal(t, "POST", request.Method)
	assert.Equal(t, "/v2/scrape", request.URL.Path)
	assert.Equal(t, "Bearer secret-key", request.Header.Get("Authorization"))
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	var sent struct {
		URL     string   `json:"url"`
		Formats []string `json:"formats"`
	}
	require.NoError(t, json.Unmarshal(*body, &sent))
	assert.Equal(t, "https://example.org/artikel", sent.URL)
	assert.Equal(t, []string{"markdown"}, sent.Formats)
}

func TestCrawlSendsNoAuthorizationWithoutAnAPIKey(t *testing.T) {
	client, request, _ := servingWithKey(t, "", 200, `{"success":true,"data":{"markdown":"# Hallo","metadata":{"title":"Titel"}}}`)

	page, _, err := client.Crawl("https://example.org/artikel")

	require.NoError(t, err)
	assert.Equal(t, "Titel", page.Title)
	assert.Equal(t, "/v2/scrape", request.URL.Path)
	assert.NotContains(t, request.Header, "Authorization", "a self-hosted Firecrawl may run without authentication")
}

func TestCrawlTakesTheFirstTitleWhenThereAreSeveral(t *testing.T) {
	client, _, _ := serving(t, 200, `{"success":true,"data":{"markdown":"x","metadata":{"title":["Erster","Zweiter"]}}}`)

	page, _, err := client.Crawl("https://example.org/a")

	require.NoError(t, err)
	assert.Equal(t, "Erster", page.Title)
}

func TestCrawlFailsWithTheReasonFirecrawlGives(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"no credits":   {402, `{"success":false,"error":"Payment required to access this resource."}`, "Payment required"},
		"rate limited": {429, `{"error":"Request rate limit exceeded. Please wait and try again later."}`, "rate limit"},
		"server error": {500, `{"success":false,"error":"An unexpected error occurred on the server."}`, "unexpected error"},
		"not json":     {502, `<html>bad gateway</html>`, "502"},
		"refused":      {200, `{"success":false,"error":"Site not supported"}`, "Site not supported"},
	} {
		t.Run(name, func(t *testing.T) {
			client, _, _ := serving(t, tc.status, tc.body)

			_, _, err := client.Crawl("https://example.org/a")

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCrawlFailsWhenThePageHasNoText(t *testing.T) {
	client, _, _ := serving(t, 200, `{"success":true,"data":{"markdown":"  ","metadata":{"title":"Leer"}}}`)

	_, _, err := client.Crawl("https://example.org/a")

	require.ErrorContains(t, err, "no text")
}

func TestCrawlReportsOnePageForASuccessfulRetrieval(t *testing.T) {
	client, _, _ := serving(t, 200, `{"success":true,"data":{"markdown":"# Hallo","metadata":{"title":"Titel"}}}`)

	_, usage, err := client.Crawl("https://example.org/a")

	require.NoError(t, err)
	assert.Equal(t, business.Usage{{Kind: business.UsageCrawl, Unit: business.UnitPages, Quantity: 1}}, usage)
}

func TestCrawlReportsNothingForAFailedRetrieval(t *testing.T) {
	client, _, _ := serving(t, 500, `{"success":false,"error":"boom"}`)

	_, usage, err := client.Crawl("https://example.org/a")

	require.Error(t, err)
	assert.Empty(t, usage)
}
