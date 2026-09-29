# cjudge v1

cjudge is an asynchronous programming judge, inspired by the execution part of
Codeforces. The v1 scope is problems, immutable test sets, submission ownership,
idempotent enqueue, isolated compilation/execution, and durable verdicts. Contests,
ratings, a browser UI, interactive problems, and custom checker programs are outside v1.

## Runtime

```
client -> API -> PostgreSQL <- worker -> Docker sandbox
```

The API never accesses the Docker socket. Workers claim jobs using PostgreSQL
`FOR UPDATE SKIP LOCKED`, renew leases, and fence all writes with a per-attempt
token. A crashed worker's job is recovered after lease expiry. Delivery is at least
once; only the current attempt can publish a verdict. Infrastructure failures retry
within a bounded attempt budget. Contestant failures are terminal verdicts.

Problems are immutable after publication. Private tests and expected outputs are
never served by public endpoints. Tokens are high-entropy bearer credentials whose
SHA-256 hashes are stored in PostgreSQL. Every submission belongs to its principal.

Compilation and each test run use fresh containers with networking disabled,
resource limits, non-root execution, dropped capabilities, a read-only root,
bounded output, and disposable workspaces. Docker alone is not a complete hostile
multi-tenant boundary: deploy workers on dedicated hosts with a sandboxed runtime
such as gVisor, and keep credentials off those hosts where possible.

## Capacity and operations assumptions

Start with one API and two execution slots; 10 submission requests/second is a
planning assumption, not a benchmark. Each job contains at most 100 tests and is
subject to per-test and whole-job deadlines. Queue admission is bounded. PostgreSQL
is the queue and source of truth; Redis, Kafka, and Kubernetes are deferred until
measurements justify them. Revisit the queue if p95 claim time exceeds 100 ms at the
required load. Job execution throughput depends on language, test count, and limits.

Cost = database + API host + worker host-hours + backup bytes. At 10x traffic, measure
worker occupancy before adding slots; no price or demand assumptions justify an
always-on cluster. Target a recoverable local end-to-end demo on 2026-09-29. Revisit
scope if operations exceed four hours/week; export PostgreSQL and retain the local
Compose run path before shutting down any paid deployment.
