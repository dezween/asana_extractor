# Asana Extractor

A small Go program that talks to the [Asana API](https://developers.asana.com/docs) to retrieve **users** and **projects** for a workspace, writes each one as its own JSON file into an output folder, and repeats this on a configurable interval (e.g. every 5 minutes or every 30 seconds) while respecting Asana's API rate limits.

This is the implementation for the "Asana Extractor" exercise (see `docs/task.txt`): Task 0 (creating an Asana account, users, projects, and a personal access token) is manual and out of scope for the code — you provide your own token and workspace via environment variables.

## Architecture

The code follows a hexagonal (ports & adapters) layout, mirroring the pattern already set by the existing `internal/config` package:

```
internal/config/
  domain/    Config struct — plain data, no I/O
  port/      Loader interface
  adapter/env/   reads Config from environment variables

internal/extractor/
  domain/    User, Project — plain data, no I/O
  port/      AsanaClient, Writer interfaces
  service/   Extractor (orchestration) + RunPeriodic (generic scheduler)
  adapter/
    asana/       real HTTP client against the Asana API
    jsonwriter/  writes entities to per-GID JSON files on disk

cmd/backednsvc/main.go   composition root: wires adapters into services, parses flags, handles signals
```

- `domain` types are plain structs with zero external dependencies.
- `port` defines only the interfaces the service layer needs (`AsanaClient`, `Writer`).
- `service` (`Extractor`) depends only on those interfaces, never on concrete adapters — this is what makes it testable with simple in-memory fakes instead of a real HTTP server or filesystem.
- `adapter` packages are swappable implementations of the ports (e.g. the Asana HTTP client could be replaced by a mock, or the JSON writer by an S3 writer, without touching `service`).

This separation is why the test suite can fake `AsanaClient`/`Writer` for the service tests, and use `httptest`/`t.TempDir()` for the adapter tests, without needing a live Asana account to validate correctness.

## Setup

### 1. Get an Asana personal access token

1. Create an Asana account and workspace at [asana.com](https://asana.com) if you don't have one (add a few dummy users and at least two projects to have something to extract).
2. Go to the [Asana Developer Console](https://app.asana.com/0/developer-console) (My Settings → Apps → Developer apps, or the link above) and create a **Personal access token**. Do not use OAuth.
3. Find your workspace's GID — the simplest way is to call `GET https://app.asana.com/api/1.0/workspaces` with your token and read the `gid` from the response, or check the URL when browsing your workspace in Asana.

### 2. Configure environment variables

Copy `.env.example` to `.env` and fill in your values, then export them (this project uses stdlib only, so there is no automatic `.env` file loader — source it manually):

```sh
cp .env.example .env
# edit .env with your token/workspace gid
set -a && source .env && set +a
```

| Variable | Required | Default | Description |
|---|---|---|---|
| `ASANA_TOKEN` | yes | — | Personal access token |
| `ASANA_WORKSPACE_GID` | yes | — | Workspace GID to extract users/projects from |
| `OUTPUT_DIR` | no | `./output` | Directory where per-entity JSON files are written |
| `EXTRACT_INTERVAL` | no | `5m` | Extraction interval, parsed with `time.ParseDuration` (e.g. `5m`, `30s`) |
| `CYCLE_TIMEOUT` | no | `2m` | Per-cycle timeout bounding a single extraction cycle (`ListUsers` + all writes + `ListProjects` + all writes), parsed with `time.ParseDuration`. Assumes the workspace's full user+project pagination completes within this budget; a very large workspace may need to raise it |

## How to run

```sh
go build ./...

# every 5 minutes
go run ./cmd/backednsvc --interval=5m

# every 30 seconds
go run ./cmd/backednsvc --interval=30s

# or rely purely on env vars (EXTRACT_INTERVAL), no flag:
go run ./cmd/backednsvc
```

Equivalent Makefile targets: `make run-5m`, `make run-30s`, `make run` (env-driven), `make build` (binary in `bin/`), `make test`, `make vet`, `make clean`.

The `--interval` CLI flag, when set, overrides `EXTRACT_INTERVAL` from the environment — the two "modes" from the exercise are just two values of the same configurable interval, not separate code paths.

The extractor runs once immediately, then again on every tick, until it receives `SIGINT`/`SIGTERM` (Ctrl+C), at which point it shuts down cleanly.

Output lands at:

```
${OUTPUT_DIR}/users/{timestamp}_{user_gid}.json
${OUTPUT_DIR}/projects/{timestamp}_{project_gid}.json
```

Each file is a single pretty-printed JSON object for that entity. The timestamp prefix (UTC, sortable) means every extraction cycle writes new files instead of overwriting the previous snapshot, so `output/` accumulates a history per entity. To find the latest snapshot for a given entity, pick the lexicographically last file matching `*_{gid}.json` in its directory.

## Rate limiting

Two mechanisms work together, both built on the Go standard library only (no `golang.org/x/time/rate`):

1. **Client-side token bucket** (`internal/extractor/adapter/asana/ratelimiter.go`): a `time.Ticker`-driven bucket caps outgoing requests to a safe constant rate (`DefaultRatePerMinute = 50`/min), well under Asana's documented limits, leaving headroom for other consumers of the same token and for retries.
2. **429 backoff**: if Asana still responds `429 Too Many Requests`, the client reads the `Retry-After` header (seconds) and sleeps that long before retrying, up to 3 attempts total, after which it gives up and returns an error rather than retrying forever.

## Scaling considerations

The exercise asks us to think about scale — thousands of projects and employees — without over-engineering the actual implementation. Approach, in order of what would be implemented first:

- **Pagination** — already implemented. The client follows Asana's `limit`/`offset`/`next_page` envelope until exhausted, so list size doesn't change the code path, only the number of requests.
- **Bounded concurrency** — at thousands of projects, listing is cheap (paginated) but per-project detail calls (if added) would not be. A worker pool (fixed number of goroutines pulling from a channel of project GIDs) bounded to, say, 5-10 concurrent workers, still funneling through the same rate limiter, would parallelize work without exceeding the API's budget.
- **Incremental sync via `modified_since`** — re-fetching every user/project on every cycle is wasteful at scale. Asana's list endpoints support a `modified_since` query param; persisting the last successful sync timestamp and passing it on the next cycle would shrink each cycle's payload to just what changed, which matters far more at 30-second intervals than at 5-minute ones.
- **Sharding by workspace** — if the organization spans multiple workspaces (or the single workspace is split into logical shards by team/project-range), extraction can be distributed across multiple worker processes or a job queue (e.g. one job per workspace, or per project-GID range), each with its own rate budget, rather than a single process serializing everything.
- **Output storage** — local disk is fine for the exercise; at real scale, writing to object storage (S3/GCS) instead of local disk removes the single-host bottleneck and plays well with the sharded/multi-process model above.

None of this is implemented in code beyond pagination — the task explicitly asks to document the approach, not build for a scale this exercise doesn't exercise.

## Tests

```sh
go test ./...
```

Coverage includes:

- `internal/extractor/adapter/asana`: multi-page pagination for both users and projects (including partial results being preserved alongside an error when a later page fails), `Authorization: Bearer` header is sent, 429 + `Retry-After` triggers a correctly-timed retry, transient network errors and 5xx responses are also retried with backoff, retries are capped and surface an error, non-200 responses are reported.
- `internal/extractor/adapter/jsonwriter`: correct JSON content at the correct per-entity path, directory creation, an error for entities with an empty GID, and rejection of malicious/invalid GIDs (e.g. path-traversal attempts) before they ever reach a filesystem path.
- `internal/extractor/service`: `Extractor.Run` calls both list methods, writes every returned entity (including partial results returned alongside a list error), aggregates list errors via `errors.Join`, and continues writing remaining entities past a single write failure (table-driven for the failure scenarios); `RunPeriodic` runs immediately then on ticks and stops cleanly on context cancellation.
- `internal/config/adapter/env`: required vars enforced, defaults applied, overrides respected, invalid duration rejected, whitespace trimmed from values before validation.

All adapter tests use stdlib `net/http/httptest` and `t.TempDir()` — no live Asana account or network access is required to run the test suite.

### Mocks

`internal/extractor/service`'s tests exercise `Extractor` through mocked `port.AsanaClient` / `port.Writer` instead of hand-written fakes, using [mockery](https://vektra.github.io/mockery/)-generated mocks (`github.com/stretchr/testify/mock` under the hood). This keeps the service layer's tests honest to the port interfaces — a change to a port signature breaks mock generation, not a silently-stale hand-rolled fake.

Generated mocks live next to the interfaces they mock, under `mocks/` (e.g. `internal/extractor/port/mocks/mock_AsanaClient.go`) — generated code, not meant to be hand-edited. Config is in `.mockery.yaml` at the repo root. Regenerate after changing a port interface:

```sh
make mocks
```

### End-to-end test

`scripts/e2e-test.sh` builds the real binary and runs it as a subprocess — no mocks, hitting the actual Asana API — covering the success path and a set of failure modes:

```sh
make e2e
```

Cases covered:
- **Success path** — valid credentials, extraction completes, JSON files are written with the expected shape, graceful shutdown on `SIGINT` exits 0.
- **Missing `ASANA_TOKEN` / `ASANA_WORKSPACE_GID`** — fails fast at config load with a non-zero exit.
- **Invalid `EXTRACT_INTERVAL`** — fails fast at config load.
- **Invalid `--interval` flag** — fails at flag-parsing with exit code 2.
- **Invalid token (live 401)** — the extraction cycle fails and is logged, but the process keeps running (a bad cycle must not crash the periodic loop) and still shuts down cleanly afterwards.
- **Nonexistent workspace GID (live request)** — the cycle fails and is logged without crashing the process.

The success-path, invalid-token, and invalid-workspace cases need real `ASANA_TOKEN`/`ASANA_WORKSPACE_GID` values (read from `.env` if present) to call the live API — they're skipped with a clear message if those aren't set, so the rest of the suite still runs anywhere.
