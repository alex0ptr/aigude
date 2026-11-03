# Firecrawl for development

`compose.yaml` runs a Firecrawl on your machine so that development and the tests crawl without paying per page. It is
**not meant for hosting**: it has no authentication and fetches any address it is given, which is why the API listens on
`127.0.0.1:3002` only. It is a reduced form of the `docker-compose.yaml` in
[Firecrawl's repository](https://github.com/firecrawl/firecrawl) (default Postgres queue, images from ghcr.io pinned by
digest; update the digests on purpose, Firecrawl publishes no version tags for them).

The tasks use it, and need Docker:

- `task serve` starts it (`task firecrawl:up`, the first start pulls the images) and points AI Gude at it.
  `task firecrawl:down` stops it. `task serve:hosted` crawls through the hosted Firecrawl instead.
- `task test:integration` starts it with Testcontainers and crawls a page through it with the client of AI Gude. With
  Rancher Desktop, Testcontainers needs to be told where Docker is:

```bash
DOCKER_HOST=unix://$HOME/.rd/docker.sock TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock task test:integration
```
