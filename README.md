# cjudge v1

A Go backend for a Codeforces-style programming judge: publish problems with private
tests, submit C++20/Python 3/Go source, and retrieve asynchronous verdicts.

The API, durable PostgreSQL queue, and container workers have separate responsibilities
and deployment privileges. Workers use expiring leases and fenced attempts, so a stale
worker cannot replace a recovered job's result. Compilation and each test run inside
fresh, resource-limited containers. Production workers default to the `runsc` runtime.

## What v1 includes

- Immutable problems, private test sets, exact/token checkers, and submission ownership.
- Hashed bearer credentials, revocation, administrator-only publication, shared rate limits.
- Atomic idempotency and queue admission, per-principal backlog caps, concurrent claims,
  lease renewal, bounded retries, crash recovery, and graceful shutdown.
- Non-root sandboxes with no networking, read-only mounts, capped RAM/PIDs/CPU/output,
  compiler tmpfs, bounded executable transfer, and orphan cleanup.
- Separate database roles, append-only runtime audit records, schema migrations,
  structured logs, worker heartbeats, and protected Prometheus metrics.
- Digest-pinned images, local Compose stack, systemd/ingress examples, CI, real
  PostgreSQL concurrency tests, and real Docker verdict/isolation tests.

This is the **judge backend**, not a complete contest platform. Contests, standings,
ratings, a frontend, interactive problems, custom checkers, and distributed filesystem
storage are outside v1. See [architecture](docs/architecture.md) and
[production topology](docs/production.md) for the implemented boundaries and limits.

## Local demo

Requires Docker with at least 4 GiB available; image downloads consume several GiB.
Only use the local `runc` configuration with code you trust. Production uses dedicated
Linux workers and `runsc`; see [operations](docs/operations.md).

```sh
cp .env.example .env
# Set a random POSTGRES_PASSWORD in .env (URL-encode reserved URL characters).
# Use a new, dedicated directory matching CJUDGE_WORKSPACE in .env.
mkdir -m 700 /tmp/cjudge-work
sh scripts/build-images.sh
docker compose build
# One-time provisioning for the Compose worker's UID (also handles Docker Desktop).
docker compose run --rm --no-deps --user 0:0 --cap-add CHOWN --entrypoint sh worker \
  -c 'test -d "$CJUDGE_WORKSPACE" && test ! -L "$CJUDGE_WORKSPACE" && chown 0:0 "$CJUDGE_WORKSPACE" && chmod 700 "$CJUDGE_WORKSPACE"'
docker compose up -d
docker compose run --rm migrate create-principal --name local-admin --admin
```

The last command prints an ID and a token **once**. Store the token securely; the DB
contains only its hash. Export it as `CJUDGE_ADMIN_TOKEN` in your shell, then:

```sh
python3 scripts/smoke.py
```

The smoke client publishes an A+B problem, checks idempotency, and waits for accepted
verdicts in all three languages. API: `http://localhost:8080`; health: `/healthz`;
readiness: `/readyz`; administrator metrics: `/metrics`.

```sh
docker compose run --rm migrate create-principal --name contestant
docker compose run --rm migrate revoke-principal --id PRINCIPAL_ID
docker compose logs -f api worker
docker compose down             # preserves the database volume
```

Compose uses one database owner credential for a convenient isolated demo. Production
must use the roles in [deploy/roles.sql](deploy/roles.sql), verified TLS to PostgreSQL,
and separate hosts for API and execution. Never expose the Docker socket over TCP or
mount it into the API or a contestant container.

## Native development

Go **1.26.8+** and PostgreSQL 17+. The Go directive selects the patched toolchain.

```sh
make build
export DATABASE_URL='postgres://USER:PASSWORD@localhost/cjudge?sslmode=disable'
bin/cjudge migrate
bin/cjudge create-principal --name admin --admin
bin/cjudge api
# A second terminal, using the same database:
CJUDGE_RUNTIME=runc CJUDGE_WORKSPACE=/tmp/cjudge-native-work bin/cjudge worker
```

For production, use `DATABASE_URL_FILE` to load a mounted secret instead of placing
credentials in the process environment. All configuration is documented in
[operations](docs/operations.md#configuration).

## API

Every `/v1` request requires `Authorization: Bearer TOKEN`. Creation requests use
`Content-Type: application/json`. Submission POSTs require `Idempotency-Key`.

| Method | Endpoint | Purpose |
| --- | --- | --- |
| POST | `/v1/problems` | Publish a problem and its tests (admin) |
| GET | `/v1/problems` | List public metadata |
| GET | `/v1/problems/{id}` | Read metadata without hidden tests |
| POST | `/v1/submissions` | Enqueue source; returns 202, or 200 on replay |
| GET | `/v1/submissions` | List the caller's submissions |
| GET | `/v1/submissions/{id}` | Poll owned state/result |

Lists use `limit=1..100` and `before=next_cursor`, in descending ID order (not creation
time). Reusing a key with changed source/problem/language returns 409. Retry temporary
503 responses with the same key and exponential backoff. Respect 429 `Retry-After`.
Responses never contain submitted source or hidden test data. Runtime stdout/stderr
are withheld; bounded compiler diagnostics are returned.

The machine-readable contract is [api/openapi.yaml](api/openapi.yaml). See
[scripts/smoke.py](scripts/smoke.py) for a dependency-free client.

## Verification

```sh
make test race vet
CJUDGE_TEST_DATABASE_URL='postgres://USER:PASSWORD@localhost/cjudge_test?sslmode=disable' make integration
CJUDGE_TEST_DOCKER=1 CJUDGE_TEST_RUNTIME=runc go test -tags=sandboxintegration -timeout=10m ./internal/judge ./internal/sandbox
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

Database integration tests create temporary schemas and roles; use a disposable DB
with role-creation privileges. Docker tests execute bounded test programs and require
the three built images. Repeat them with `CJUDGE_TEST_RUNTIME=runsc` on production
worker hosts before accepting public submissions. See [validation](docs/validation.md)
for this release's actual results and unverified deployment assumptions.
