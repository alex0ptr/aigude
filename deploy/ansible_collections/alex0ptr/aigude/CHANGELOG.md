# Changelog

## Unreleased

- The Traefik route of `aigude` adds the middlewares `headers`, `rate-limit` and `in-flight`. The headers are frame, nosniff, referrer and Content-Security-Policy. They are set by the variables `aigude_traefik_rate_limit_average`, `aigude_traefik_rate_limit_burst`, `aigude_traefik_in_flight_amount` and `aigude_traefik_content_security_policy`.
- The route no longer removes the header `X-Aigude-Demo`, which AI Gude does not read.

## 0.1.0

- Roles `podman`, `traefik`, `firecrawl` and `aigude`, and an example playbook for each of the four modes.
