//go:build integration

package firecrawl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// liveClientOpts reads the address from AIGUDE_FIRECRAWL_URL (default: the hosted service) and the key from
// FIRECRAWL_API_KEY, which only the hosted service needs.
func liveClientOpts(t *testing.T) ClientOpts {
	t.Helper()
	opts := ClientOpts{APIKey: os.Getenv("FIRECRAWL_API_KEY"), BaseURL: os.Getenv("AIGUDE_FIRECRAWL_URL")}
	if opts.APIKey == "" && opts.BaseURL == "" {
		t.Skip("FIRECRAWL_API_KEY environment variable not set (not needed when AIGUDE_FIRECRAWL_URL names another address)")
	}
	return opts
}

// TestCrawlLive retrieves a real page through the configured Firecrawl; on the hosted service it costs one credit.
func TestCrawlLive(t *testing.T) {
	page, _, err := NewClient(liveClientOpts(t)).Crawl("https://example.com")

	require.NoError(t, err)
	require.NotEmpty(t, page.Title)
	require.Contains(t, page.Text, "documentation examples")
	fmt.Printf("title: %q, %d characters\n", page.Title, len(page.Text))
}

// TestScrapeAnswerCarriesCreditsUsedLive prints whether the answer of the scrape endpoint carries
// data.metadata.creditsUsed, the finding the Usage of the crawl depends on.
func TestScrapeAnswerCarriesCreditsUsedLive(t *testing.T) {
	opts := liveClientOpts(t)
	client := NewClient(opts)
	body, err := json.Marshal(scrapeRequest{URL: "https://example.com", Formats: []string{"markdown"}})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, client.baseURL+"/v2/scrape", bytes.NewReader(body))
	require.NoError(t, err)
	if opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opts.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.http.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(answer))

	var decoded struct {
		Data struct {
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(answer, &decoded))
	credits, carried := decoded.Data.Metadata["creditsUsed"]
	fmt.Printf("finding for the Usage of the crawl: data.metadata.creditsUsed present: %t, value: %s\n", carried, credits)
}
