//go:build integration

package firecrawl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tccompose "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

// selfHostedCompose is the example of hack/firecrawl for running a Firecrawl of your own.
const selfHostedCompose = "../../hack/firecrawl/compose.yaml"

// startSelfHostedFirecrawl starts the example stack of hack/firecrawl without authentication, on a free port of the
// loopback interface, and returns its address. The stack is removed when the test ends.
func startSelfHostedFirecrawl(t *testing.T) string {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	stack, err := tccompose.NewDockerCompose(selfHostedCompose)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stack.Down(context.Background(), tccompose.RemoveOrphans(true), tccompose.RemoveVolumes(true)))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	t.Log("Starting a self-hosted Firecrawl; the first run pulls the images")
	require.NoError(t, stack.
		WithEnv(map[string]string{"FIRECRAWL_PUBLISH": "127.0.0.1:"}).
		WaitForService("api", wait.ForListeningPort("3002/tcp").WithStartupTimeout(10*time.Minute)).
		Up(ctx, tccompose.Wait(true)))

	api, err := stack.ServiceContainer(ctx, "api")
	require.NoError(t, err)
	host, err := api.Host(ctx)
	require.NoError(t, err)
	port, err := api.MappedPort(ctx, "3002/tcp")
	require.NoError(t, err)
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

// TestCrawlThroughASelfHostedFirecrawl crawls a real page through the Firecrawl of hack/firecrawl, without an API key.
func TestCrawlThroughASelfHostedFirecrawl(t *testing.T) {
	address := startSelfHostedFirecrawl(t)
	client := NewClient(ClientOpts{BaseURL: address})

	var page struct{ title, text string }
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got, _, err := client.Crawl("https://example.com")
		if !assert.NoError(c, err) {
			return
		}
		page.title, page.text = got.Title, got.Text
	}, 3*time.Minute, 5*time.Second, "the self-hosted Firecrawl answers the scrape endpoint the client uses")

	assert.NotEmpty(t, page.title)
	assert.Contains(t, page.text, "documentation examples")
}
