# V1 validation record — 2026-09-29

This records checks actually run during implementation. It is not a production
availability, throughput, sandbox-escape, or contest-timing certification.

## Environment

- Host: macOS arm64; Go 1.26.8 selected by the module toolchain directive.
- PostgreSQL: isolated local PostgreSQL 17 cluster; tests use temporary schemas/roles.
- Docker: Desktop Linux daemon 28.4.0, local `runc` runtime.
- Images: final digest-pinned C++, Python, Go, PostgreSQL and service build bases.
- Service version: `1.0.0`; native binary and separate API/worker images built.

## Passed

| Check | Evidence/scope |
| --- | --- |
| Unit + race suite | `go test -race ./...` across API, config, domain, judge, sandbox, worker |
| Real database + race suite | `go test -race -tags=integration ./...` with an explicit disposable test DSN |
| Static checks | `go vet ./...`, `gofmt`, `git diff --check` |
| Go vulnerability gate | `govulncheck@v1.8.0 ./...`: **No vulnerabilities found** after upgrading Go/pgx/x/text |
| Concurrent idempotency | 20 simultaneous identical requests return one durable job |
| Admission correctness | Concurrent requests cannot exceed configured queue capacity |
| Concurrent claims | 20 workers claim 20 distinct queued jobs |
| Stale-attempt fencing | Old lease cannot renew, retry or publish after recovery |
| Recovery + attempt budget | Expired final attempts become system error; retry delay and terminal exhaustion verified |
| Auth/privacy | Revoked keys rejected, ownership enforced, tests/source absent from API results |
| Role boundaries | API cannot read private tests/mint keys/mutate audit; worker cannot mint keys or modify problems; normal operations succeed |
| Audit and immutability | Expected enqueue/claim/finish events exist without source; finished verdict rewrites rejected by PostgreSQL |
| Real language execution | Accepted C++20, Python 3, and Go programs in fresh Docker containers |
| Real failure verdicts | Wrong answer, C++/Python compile errors, runtime error, time limit, memory limit, output limit |
| Real isolation checks | UID 65532, read-only host/root paths, no outbound connection, no Docker socket or DB environment |
| Native end-to-end smoke | HTTP publication + idempotent submission + PostgreSQL + worker + Docker: all three languages accepted |
| Packaged end-to-end smoke | Separate Compose API/worker images, migrations, DB health, localhost ingress, shared workspace: all three languages accepted |
| Runtime fail-closed behavior | Default worker exits with `required Docker runtime "runsc" unavailable` on this host |
| Metrics | Protected endpoint returned queue gauges and a live worker heartbeat |
| Deployment configuration | `docker compose config --quiet`; actual stack startup and API health check |

The database suite was rerun after adding the terminal-state trigger and narrower
INSERT grants. The sandbox suite was rerun with final patched language images.
The final native build reports `1.0.0`. Test processes and the isolated Compose test
database are disposable and are not a deployed production service.

## Findings corrected during real integration

- Docker unmounts tmpfs on process exit: compiler artifacts now stream out while the
  container is alive, with bounded archive capture and fixed-path regular-file extraction.
- An internal-only Compose network did not publish the host API port: only the API
  now joins an additional ingress network; the DB and workers stay internal.
- Docker Desktop start/attach overhead exceeded the original three-second demo
  deadline for Python in the packaged stack. The smoke problem now uses ten seconds.
  This is consistent with v1's documented wall-clock policy, not evidence of CPU-time
  parity with Codeforces. Calibrate or replace the accounting policy before running
  tightly timed public contests.

## Not verified here

- gVisor/runsc behavior: it is not installed in this Docker Desktop environment.
- Production Linux/systemd/nginx deployment, TLS and network-policy configuration.
- Container/OS vulnerability scanning and registry signing (the clean scan above is
  for Go source/dependencies, not a blanket image-security claim).
- High-availability PostgreSQL failover, PITR, measured RPO/RTO or a restore drill.
- Sustained workload throughput, p95/p99 SLOs, strict fairness, and hostile multi-tenant
  sandbox escape resistance. Unit tests and a local smoke test cannot establish these.
- Hosted GitHub Actions execution: workflows are included but no branch/PR was pushed.

Complete these deployment checks using [operations.md](operations.md) before public
traffic. The architecture supports multiple API replicas and worker hosts; the local
verification used one API and two worker slots, not a production-scale benchmark.
