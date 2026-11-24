# Validation evidence

Each artifact below contains the command or query that produced it and a one-line interpretation. The load reports use the default `make reproduce` workload: ten sequential baseline requests followed by 200 requests with 50 in flight.

| Evidence | Command or query | Artifact and interpretation |
| --- | --- | --- |
| Baseline report | `make down && make reproduce` before source changes | [baseline-load-report.txt](baseline-load-report.txt) — the unprotected service reached 60% success and 8.01 s p95 under load. |
| Resilient report | `make down && docker compose up -d --wait && docker compose run --name resilient-loadgen loadgen` | [resilient-load-report.txt](resilient-load-report.txt) — the protected service reached 100% success and 3.79 s p95 under the identical load shape. |
| Passing tests | `docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine go test ./...` | [test-output.txt](test-output.txt) — the starter suite and the added resilience tests pass with Go 1.25. |
| Baseline slow trace | Jaeger search: `service=quotation-api`, `operation=POST /quotes`, `Last Hour` | [baseline-longest-trace.json](baseline-longest-trace.json) — an original trace records the serial partner cascade before resilience controls. |
| Circuit-open trace | Jaeger search: `resilience.circuit_open=true`, `Last Hour` | [circuit-open-trace.json](circuit-open-trace.json) — the trace marker identifies a call rejected before a partner HTTP span is created. |
| Cache-hit trace | Jaeger search: `resilience.cache_hit=true`, `Last Hour` | [cache-hit-trace.json](cache-hit-trace.json) — the cached response has no outbound insurer spans. |
| Breaker, cache, and p95 metrics | PromQL queries with a `[5m]` window | [prometheus-observations.md](prometheus-observations.md) — query text, values, and rendered transition/latency graphs are retained together. |

The trace exports use Jaeger's JSON API rather than screenshots so they remain reviewable after the in-memory Jaeger container is stopped.
