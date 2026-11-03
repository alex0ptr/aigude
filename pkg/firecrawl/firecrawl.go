// Package firecrawl implements business.Crawler with Firecrawl's v2 scrape endpoint, through a small client of its
// own: one POST with a bearer key, Markdown as the answer.
package firecrawl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg"
)

// DefaultBaseURL is where Firecrawl's API lives.
const DefaultBaseURL = "https://api.firecrawl.dev"

// requestTimeout is how long one retrieval may take, scraping included.
const requestTimeout = 90 * time.Second

// Client implements business.Crawler.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

var _ business.Crawler = (*Client)(nil)

// ClientOpts are the optional settings of a Client.
type ClientOpts struct {
	// APIKey is sent as bearer key. Without one no Authorization header is sent, as a self-hosted Firecrawl can run
	// without authentication.
	APIKey string
	// BaseURL replaces DefaultBaseURL, for example for a self-hosted Firecrawl or a test server.
	BaseURL string
}

// NewClient creates a Crawler that scrapes pages through Firecrawl.
func NewClient(opts ClientOpts) *Client {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	slog.Debug("Creating Firecrawl client", "baseURL", baseURL, "authenticated", opts.APIKey != "")
	return &Client{apiKey: opts.APIKey, baseURL: strings.TrimRight(baseURL, "/"), http: pkg.NewHTTPClient(requestTimeout)}
}

// scrapeRequest is the body of POST /v2/scrape.
type scrapeRequest struct {
	URL     string   `json:"url"`
	Formats []string `json:"formats"`
}

// scrapeResponse is what the endpoint answers; failures carry only an error text.
type scrapeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Markdown string `json:"markdown"`
		Metadata struct {
			// Title is a string, or a list of strings when the page names several.
			Title json.RawMessage `json:"title"`
		} `json:"metadata"`
	} `json:"data"`
}

// Crawl scrapes the page at the address and returns its title and Markdown text. A successful retrieval counts as one
// page of Usage, also when the page turns out to have no text.
func (c *Client) Crawl(url string) (business.Page, business.Usage, error) {
	slog.Info("Crawling page", "url", url)
	fail := func(err error) (business.Page, business.Usage, error) {
		return business.Page{}, nil, fmt.Errorf("crawl %s: %w", url, err)
	}

	body, err := json.Marshal(scrapeRequest{URL: url, Formats: []string{"markdown"}})
	if err != nil {
		return fail(err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v2/scrape", bytes.NewReader(body))
	if err != nil {
		return fail(err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}

	var decoded scrapeResponse
	decodeErr := json.Unmarshal(answer, &decoded)
	if resp.StatusCode != http.StatusOK || decodeErr != nil || !decoded.Success {
		return fail(fmt.Errorf("firecrawl answered %d: %s", resp.StatusCode, reasonOf(decoded, answer)))
	}
	usage := business.Usage{{Kind: business.UsageCrawl, Unit: business.UnitPages, Quantity: 1}}
	if strings.TrimSpace(decoded.Data.Markdown) == "" {
		_, _, err := fail(errors.New("the page has no text"))
		return business.Page{}, usage, err
	}
	page := business.Page{Title: titleOf(decoded.Data.Metadata.Title), Text: decoded.Data.Markdown}
	slog.Debug("Crawled page", "url", url, "title", page.Title, "characters", len(page.Text))
	return page, usage, nil
}

// reasonOf is the error text Firecrawl gave, or the start of what it sent instead.
func reasonOf(decoded scrapeResponse, answer []byte) string {
	if decoded.Error != "" {
		return decoded.Error
	}
	const most = 200
	text := strings.TrimSpace(string(answer))
	if len(text) > most {
		text = text[:most]
	}
	return text
}

// titleOf reads a title that is a string or a list of strings.
func titleOf(raw json.RawMessage) string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil && len(many) > 0 {
		return many[0]
	}
	return ""
}
