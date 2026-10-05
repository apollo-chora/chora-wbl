# chora-wbl

> Work-Based Learning (WBL) service — internships, apprenticeships, employer
> rosters, competency milestones and work-evidence submissions for the CHORA
> platform.

Standalone, provider-neutral Go service. Shared Chora modules are consumed as Go
modules (`github.com/apollo-chora/chora-common`,
`github.com/apollo-chora/chora-contracts`). Tracing uses OTLP
(`OTEL_EXPORTER_OTLP_ENDPOINT`); events are emitted as envelope-conforming
messages on the NATS JetStream bus (`chora-common/eventbus`) in production.

Per the locked architecture, WBL is a sub-context of Content Delivery and shares
the `chora_delivery` database at M12. Until that hand-off lands, the service
serves from an in-memory composite store and an in-memory event bus, so it boots
with no external dependencies.

## Hexagonal layout

```
cmd/server/                     — main (HTTP server + OTLP wiring + graceful shutdown)
internal/domain/internship/     — pure domain (Listing + Application state machine)
internal/domain/apprenticeship/ — pure domain (Program)
internal/domain/employer/       — pure domain (Employer + Roster)
internal/domain/milestone/      — pure domain (Milestone + dual sign-off)
internal/domain/evidence/       — pure domain (Evidence + state machine)
internal/adapter/inmem/         — in-memory composite store
internal/adapter/http/          — HTTP/JSON adapter
internal/events/                — envelope-conforming event bus
internal/observability/         — OTLP + W3C traceparent
```

## Endpoints

| Path                                          | Method   | Auth                | Description |
|-----------------------------------------------|----------|---------------------|-------------|
| `/healthz`, `/healthz/`, `/health`            | GET      | public              | liveness |
| `/readyz`, `/readyz/`                         | GET      | public              | readiness |
| `/api/internships`                            | POST/GET | `X-Tenant-Id`+`gcid`| post / list internship listings |
| `/api/internships/{id}/applications`          | POST     | `X-Tenant-Id`+`gcid`| apply to a listing |
| `/api/applications/{id}/accept`               | POST     | `X-Tenant-Id`+`gcid`| accept an application |
| `/api/applications/{id}/reject`               | POST     | `X-Tenant-Id`+`gcid`| reject an application |
| `/api/apprenticeships`                        | POST/GET | `X-Tenant-Id`+`gcid`| create / list apprenticeship programs |
| `/api/employers`                              | POST/GET | `X-Tenant-Id`+`gcid`| create / list employers |
| `/api/employers/{id}/roster`                  | GET      | `X-Tenant-Id`+`gcid`| employer roster view |
| `/api/milestones`                             | POST     | `X-Tenant-Id`+`gcid`| create a milestone |
| `/api/milestones/{id}/complete`               | POST     | `X-Tenant-Id`+`gcid`| mark a milestone complete |
| `/api/milestones/{id}/sign-off-employer`      | POST     | `X-Tenant-Id`+`gcid`| employer sign-off |
| `/api/milestones/{id}/sign-off-student`       | POST     | `X-Tenant-Id`+`gcid`| student sign-off |
| `/api/evidence`                               | POST     | `X-Tenant-Id`+`gcid`| submit work evidence |
| `/api/evidence/{id}/approve`                  | POST     | `X-Tenant-Id`+`gcid`| approve evidence |
| `/api/evidence/{id}/reject`                   | POST     | `X-Tenant-Id`+`gcid`| reject evidence |

Protected endpoints reject requests missing `X-Tenant-Id` or `gcid` headers.
Health endpoints bypass the actor check.

## Environment configuration

No inline URLs / secrets / identifiers in source. All values resolve from
environment variables. See `.env.example`.

| Env var | Required | Default | Purpose |
|---|---|---|---|
| `PORT` | no | `8080` | HTTP listener port |
| `CHORA_SOURCE_PROJECT` | no | `chora-local` | logical source label on emitted envelopes |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | no | (stdout) | OTLP/gRPC collector endpoint; unset streams spans to stdout |
| `OTEL_EXPORTER` | no | — | set to `stdout` to force the stdout exporter |
| `DEPLOYMENT_ENVIRONMENT` | no | `dev` | `deployment.environment` resource attribute |

## Events

Events are published as envelope-conforming messages with the subject taxonomy
`chora.wbl.{aggregate}.{event_type}.v{N}` (for example
`chora.wbl.milestone.completed.v1`). Mandatory envelope fields: `event_id`
(UUIDv7), `idempotency_key`, `tenant_id`, `gcid`, `occurred_at`, `published_at`,
`traceparent`, `tracestate`, `source_project`, `source_service`,
`schema_version`.

## Build & test

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```
