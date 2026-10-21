# chora-wbl

Work-Based Learning for the Chora platform: internship listings and applications,
apprenticeship programmes, employer rosters, competency milestones with dual sign-off, and
work-evidence submissions.

This is the smallest service in the stack and the easiest to get wrong, because it looks like a
normal database-backed service and is not one. **Every byte of state lives in process memory, and
nothing it emits ever leaves the process.** Read [What it is](#what-it-is) before planning
anything around it.

Every command, port and variable below was checked against the code in this repository and
against a running `chora-stack`.

## What it is

A standalone Go service with no external dependencies at all. `cmd/server/main.go` wires exactly
two things:

```go
store := inmem.NewStore()   // in-memory composite store
bus   := events.NewBus()    // in-memory event bus
```

There is no `os.Getenv` for a DSN, no NATS client, no S3 client. `go.mod` pulls in
`chora-common` and `chora-contracts` for tracing and types, nothing more. The consequence is
worth stating plainly, because it changes what "deploy" can mean:

| Property | Reality |
| --- | --- |
| Persistence | **none** — a restart returns every entity to zero |
| Multi-replica | **unsafe** — two replicas have two disjoint datasets |
| Event delivery | **none** — envelopes are built and validated, then dropped |
| Migrations | **inert** — `migrations/0001_initial.sql` is a placeholder nothing applies |
| Health | works, but says nothing about data |

The service boots with no dependencies, which is genuinely useful for a demo or an integration
test, and misleading as a production deployment. The header of `migrations/0001_initial.sql`
states the intent: WBL is a sub-context of Content Delivery that will move into the
`chora_delivery` database at M12. Until that hand-off lands, this is what you have.

### What the stack runs

| Feature | State in the stack | Consequence |
| --- | --- | --- |
| HTTP API | **live** | all routes answer on `8080`, real state machines, real validation |
| Store | **in-memory** | `GET /api/internships` returns `{"items":[]}` on every boot |
| Event bus | **in-memory** | no event reaches NATS; there is no NATS wiring to enable |
| Tracing | **wired** | OTLP exporter to `otel-collector:4317` |
| Database | **none** | nothing to configure, nothing to migrate |

## Prerequisites

| Route | What you need |
| --- | --- |
| In the stack | A working `chora-stack` deployment, only so the gateway can reach it. The service itself needs nothing. |
| Standalone | Go **1.26.1** (`go.mod` declares it). Nothing else — no database, no broker, no object store. |

To build and run by hand:

```sh
go build ./...
go run ./cmd/server
```

That is a complete deployment. A `.env` is optional and carries no secrets.

## Credentials

**None.** This service reads no API key, no password and no token.

| Credential | Needed | Notes |
| --- | --- | --- |
| Database password | no | no database |
| NATS credentials | no | no broker client |
| Object-storage keys | no | no uploads |
| Provider API keys | no | no model calls |

The only configuration is `PORT`, a source-project label, and optional tracing settings. If a
deployment checklist asks you for a credential for WBL, the checklist is wrong.

## Run it in the stack

The Compose service is `wbl`, and its environment block is three variables:

```text
PORT                          8080
CHORA_SOURCE_PROJECT          chora-local
OTEL_EXPORTER_OTLP_ENDPOINT   http://otel-collector:4317
```

From `chora-stack`:

```sh
docker compose up -d wbl
```

It has **no `depends_on`** and declares no health dependency on anything — accurate, given it
needs nothing. It exposes `8080` only, with no host port, so `localhost:8080` is not this
service; reach it from another container or through the gateway.

The `compose.yml` comment for the block says the same thing this README does: *"No database: the
WBL sub-context runs on in-memory adapters until its M12 hand-off into chora_delivery."*

Logs:

```sh
docker compose logs -f wbl
```

## Run it standalone

Nothing to prepare:

```sh
export PORT=8080
export CHORA_SOURCE_PROJECT=chora-local
go run ./cmd/server
```

Expect exactly two lines before it starts serving:

```text
otel: otlp exporter (endpoint=…) service=chora-wbl version=0.1.0
service=chora-wbl version=0.1.0 listening on :8080
```

With `OTEL_EXPORTER=stdout` — or with no endpoint set — the first line is replaced by a stdout
exporter and spans print to the terminal. Both are correct.

Because the store starts empty, a standalone instance is only useful once you POST to it. The
first useful call is `POST /api/internships`; see [API surface](#api-surface).

## Configuration

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `PORT` | `8080` | this repo (`os.Getenv`) | HTTP listen port |
| `CHORA_SOURCE_PROJECT` | `chora-local` | this repo (`os.Getenv`) | `source_project` on emitted event envelopes |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset → stdout | `chora-common/otel` | OTLP/gRPC collector endpoint |
| `OTEL_EXPORTER` | unset | `chora-common/otel` | set to exactly `stdout` to force the stdout exporter |
| `DEPLOYMENT_ENVIRONMENT` | `dev` | `chora-common/otel` | `deployment.environment` resource attribute |
| `CLOUD_REGION` | unset | `chora-common/otel` | `cloud.region` resource attribute, when set |

Only the first two are read by code in this repository. The four tracing variables are read
inside the pinned `chora-common` module (`otel/otel.go`), which is why grepping this repo for them
finds only comments — they still work. The stack's log line
`otel: otlp exporter (endpoint=http://otel-collector:4317)` is the proof.

## Verify it works

### It is up

```sh
docker compose exec -T nats wget -qO- http://wbl:8080/healthz
docker compose exec -T nats wget -qO- http://wbl:8080/readyz
```

Observed against the running stack:

```json
{"status":"ok"}
{"status":"ok"}
```

`/healthz`, `/healthz/`, `/health` all answer, as do `/readyz` and `/readyz/` — the trailing-slash
aliases are registered deliberately, so a proxy that appends a slash does not 404.

### The actor guard rejects a bare request

```sh
docker compose exec -T nats wget -qO- http://wbl:8080/api/internships
```

`400 X-Tenant-Id required`. The guard checks `X-Tenant-Id` first, then `gcid`, and returns `400`
for either — not `401`, which matters if you have a retry rule keyed on status code.

### An authenticated call reaches the store

```sh
docker compose exec -T nats wget -qO- \
  --header='X-Tenant-Id: 00000000-0000-7000-8000-000000000001' \
  --header='gcid: GCID-LOCAL-1' \
  http://wbl:8080/api/internships
```

```json
{"items":[]}
```

That empty array is the correct answer and the most informative one: the service is healthy and
the store is empty because it just booted. The same call returns your data only until the next
restart.

### Write something, then prove it is in memory only

```sh
docker compose exec -T nats wget -qO- --post-data='{"employer_id":"00000000-0000-7000-8000-000000000001","title":"Platform Engineering Internship","description":"six months","duration_weeks":26,"skills":["go"]}' \
  --header='Content-Type: application/json' \
  --header='X-Tenant-Id: 00000000-0000-7000-8000-000000000001' \
  --header='gcid: GCID-LOCAL-1' \
  http://wbl:8080/api/internships
```

Then `docker compose restart wbl` and repeat the `GET`: the item is gone. That is the whole
persistence story in one experiment.

### Traces reached the collector

```sh
docker compose logs wbl --tail=5
```

A request line with `trace_id=… span_id=… tenant_id=… gcid=… path=… status=… duration_ms=…` is
the pass; it also proves the actor headers were parsed and are being logged.

## Troubleshooting

- **Data disappeared after a restart** — by design; there is no persistence layer. Nothing to
  fix here short of the M12 hand-off into `chora_delivery`.
- **Two replicas return different data** — expected. Each process has its own store. Run exactly
  one replica, and do not put this behind a load balancer that spreads writes.
- **Nothing appears in NATS** — nothing will. The event bus is in-process; there is no NATS
  client compiled in, so there is no variable to set that would change this.
- **`400` instead of `401`** — the actor guard uses `400` for both missing headers. If you see
  `400`, check for `X-Tenant-Id` first, then `gcid` (lowercase, not `X-Chora-GCID`).
- **`404` on `/api/applications`** — only the collection-with-id form is routed
  (`/api/applications/{id}/accept` etc.); there is no bare `/api/applications` endpoint. The
  same asymmetry applies to `/api/internships/` and `/api/evidence/`, which are sub-path
  handlers while their collections are the bare paths.
- **The migration never ran** — correct, and there is no runner for it. `migrations/0001_initial.sql`
  is a placeholder skeleton for the future `chora_delivery` hand-off. Do not apply it and expect
  the service to start using a database; nothing reads it.
- **Spans go to the terminal instead of the collector** — `OTEL_EXPORTER_OTLP_ENDPOINT` is unset,
  or `OTEL_EXPORTER=stdout` is set. Both fall back to `stdouttrace`.

## API surface

Health needs no headers; every `/api/*` route requires `X-Tenant-Id` **and** `gcid`.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz`, `/healthz/`, `/health` | liveness |
| `GET` | `/readyz`, `/readyz/` | readiness |
| `GET`,`POST` | `/api/internships` | list / create internship listings |
| `*` | `/api/internships/{id}/applications` | apply to a listing |
| `*` | `/api/applications/{id}/accept` | accept an application |
| `*` | `/api/applications/{id}/reject` | reject an application |
| `GET`,`POST` | `/api/apprenticeships` | list / create apprenticeship programmes |
| `GET`,`POST` | `/api/employers` | list / create employers |
| `GET` | `/api/employers/{id}/roster` | employer roster |
| `GET`,`POST` | `/api/milestones` | list / create milestones |
| `*` | `/api/milestones/{id}/complete` | learner marks a milestone complete |
| `*` | `/api/milestones/{id}/sign-off-employer` | employer attestation |
| `*` | `/api/milestones/{id}/sign-off-student` | student attestation |
| `GET`,`POST` | `/api/evidence` | list / submit work evidence |
| `*` | `/api/evidence/{id}/approve` | approve evidence |
| `*` | `/api/evidence/{id}/reject` | reject evidence |

Responses use a small error shape, `{"error": "<message>"}`. The messages are deliberately terse
and come from two sentinel errors — `invalid argument` for a bad payload and `invalid transition`
for a state-machine rejection — so a `400` tells you *which* rule failed but not which field. To
find the field, read the request against the domain constructor in `internal/domain/`.

There is **no gRPC listener** and no GraphQL endpoint — HTTP/JSON only.

## Architecture

Hexagonal, and small enough to read in one sitting:

```text
cmd/server/                      main: store + bus + OTLP + graceful shutdown
internal/domain/internship/      Listing + Application state machine
internal/domain/apprenticeship/  Program
internal/domain/employer/        Employer + Roster
internal/domain/milestone/       Milestone + dual sign-off
internal/domain/evidence/        Evidence + state machine
internal/adapter/inmem/          the composite in-memory store
internal/adapter/http/           HTTP/JSON adapter, actor guard, logging middleware
internal/events/                 in-memory envelope-conforming bus
internal/observability/          OTLP + W3C traceparent
```

Routing is a plain `http.ServeMux`. Middleware is layered as
`logging(actorRequired(handler))`, so the access log records the tenant and gcid the guard let
through — which is why a `400` line in the logs shows `tenant_id= gcid=` empty.

The four state machines are the real logic, and they are enforced in the domain, not the handler:

| Entity | States |
| --- | --- |
| Listing | `open` → `closed` |
| Application | `submitted` → `accepted` \| `rejected` |
| Evidence | `submitted` → `approved` \| `rejected` |
| Milestone | `pending` → `in_review` → `employer_signed` → `signed_off` |

Milestones carry two separate attestations (`employer_sign`, `student_sign`) and only reach
`signed_off` when both exist, which is why the employer and student sign-off routes are distinct
rather than one `sign-off`. Each attestation records who signed and when.

Observability is `chora-common`'s canonical init: one span per inbound request, W3C
`traceparent` propagation in and out, and `traceparent` echoed on every response header — so a
caller can correlate without shipping a tracing backend.

## Events

Events are emitted **in memory and nowhere else**. The bus exists so handlers and tests can
assert the taxonomy and envelope shape without standing up a broker.

| Direction | Mechanism |
| --- | --- |
| Outbound | `internal/events`, in-process; the envelope is built and validated, then dropped |
| Inbound | none — the service subscribes to nothing |
| Transport | none; no NATS client is linked |

Taxonomy: `chora.wbl.{aggregate}.{event_type}.v{N}` — for example
`chora.wbl.milestone.completed.v1`.

Every envelope carries the platform's mandatory fields:

```text
event_id (UUIDv7)   idempotency_key   tenant_id   gcid
occurred_at         published_at      traceparent tracestate
source_project      source_service    schema_version
```

`gcid` is the acting learner or employer; `source_service` is always `chora-wbl`, and
`source_project` comes from `CHORA_SOURCE_PROJECT` (`chora-local` in the stack).

The practical consequence: if a downstream service is supposed to react to a WBL milestone
completion, it will not, and no configuration change will make it. That is the M12 hand-off gap,
not a missing variable. The package docstring says the same thing —
*"Production wiring (M12+) replaces this with the shared NATS JetStream publisher"*.

## Migrations

**One file, and it is inert.** `migrations/0001_initial.sql` creates the WBL tables
(`wbl_internship_listing` and siblings) as a placeholder skeleton for the M12 hand-off into
`chora_delivery`. Nothing in this repository reads it, no Compose service applies it, and the
stack has no `wbl-migrate` one-shot — unlike every database-backed service in the platform.

Its own header explains the plan: the tables will move into `chora_delivery` with RLS policies
and PgBouncer pooling at M12, and cross-database queries are forbidden, so inter-domain
communication is by event bus only. Do not run it against `chora_wbl` and expect the service to
start using that database — the store is constructed in `main.go` and takes no DSN.
