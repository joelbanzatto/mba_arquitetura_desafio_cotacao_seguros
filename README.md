# Resilient Insurance Quotation PoC

This repository contains a Solution Architecture Document (SAD) and a working proof of concept for a multi-tenant auto-insurance quotation platform. The original challenge brief is preserved in [docs/enunciado.md](docs/enunciado.md).

## Delivered artifacts

- [Solution Architecture Document](docs/sad.md)
- [Measured evidence](docs/evidencias/README.md)
- Redis-backed aggregate quotation cache, scoped by tenant and a normalized-risk digest
- Per-partner circuit breakers with a 2-second partner timeout
- Visible partial-response fallback when at least one insurer is unavailable
- OpenTelemetry resilience metrics and trace markers

The core flow protects the partner boundary, not the price. A cache hit returns a previously received partner quote without calling an insurer. On a cache miss, each insurer is queried serially behind its own circuit breaker; unavailable insurers are identified in the successful partial response, and no price is fabricated.

## Run the environment

Docker Compose v2 is the supported local environment. It builds Go 1.25 containers, Redis, Jaeger, Prometheus, the OpenTelemetry Collector, and the three fixed partner mocks.

```bash
make up
make ps
```

The API is available at `http://localhost:8080`, Jaeger at `http://localhost:16686`, and Prometheus at `http://localhost:9090`.

Run the reproducible load scenario:

```bash
make down && make reproduce
```

Run the test suite locally when Go 1.25 is installed:

```bash
make test
make smoke
```

When Go is not installed on the host, the same suite can run in the required toolchain container:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine go test ./...
```

## Response contract

Successful live responses have `"source":"partner"`; cached responses have `"source":"cache"` and a `cache_age_seconds` value. A partial fallback remains HTTP 200 but sets `"degraded":true` and lists `unavailable_partners`. A complete partner outage remains a 502 because no real quote is available.

Example:

```json
{
  "tenant_id": "corretora-a",
  "quotes": [{"partner": "partner-slow", "premium_cents": 123456}],
  "source": "partner",
  "degraded": true,
  "unavailable_partners": ["partner-flaky"]
}
```

## Resilience configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `REDIS_ADDRESS` | `localhost:6379` | Redis endpoint for the aggregate cache |
| `QUOTE_CACHE_TTL` | `1h` | Maximum reuse time for a complete quotation |
| `PARTNER_TIMEOUT` | `2s` | Maximum duration of one external insurer call |
| `BREAKER_CONSECUTIVE_ERRORS` | `3` | Consecutive failed calls before opening a partner breaker |
| `BREAKER_OPEN_TIMEOUT` | `30s` | Cooldown before a breaker becomes half-open |
| `BREAKER_HALF_OPEN_REQUESTS` | `2` | Successful probes required to close a half-open breaker |

The Redis key is `quotation:v1:tenant:<tenant_id>:risk:<sha256(normalized-risk)>`. Including the tenant is mandatory isolation; hashing the normalized risk avoids storing raw CPF or plate values in the key.

## Telemetry

The application emits these Prometheus-exported metrics:

- `quotation_breaker_transitions_total{partner,from_state,to_state}`
- `quotation_cache_requests_total{result="hit|miss|error"}`
- `quotation_partner_quote_duration_seconds{partner}`

The request trace receives `resilience.circuit_open=true` when a call is short-circuited and `resilience.cache_hit=true` for a cache-served response. Neither trace marker nor any metric label includes CPF, plate, or quote ID.

## Deliberately not implemented

This PoC intentionally excludes parallel aggregation, retries, bulkheads, rate limiting, persistent audit storage, cross-replica breaker state, and cache invalidation before TTL expiry. The SAD records the operational effect and next-step proposal for each limitation.
