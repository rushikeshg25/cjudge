# Operating cjudge

## Production rollout

1. Provision PostgreSQL 17+ with TLS, encrypted storage, managed failover, backups and
   WAL archiving. Use a dedicated database and migration owner. API/worker/operator
   logins must not be superusers, database owners, or members of each other's roles.
2. Run `cjudge migrate` with the owner login, then `psql ... -v ON_ERROR_STOP=1 -f deploy/roles.sql`.
   Provision separate login credentials via your secret manager and grant the matching
   `cjudge_api`, `cjudge_worker`, or `cjudge_operator` group role. Do not grant DDL rights.
3. Store per-process connection URLs in root-managed secret files. Require
   `sslmode=verify-full` and a trusted CA. Set `DATABASE_URL_FILE` in each service's
   environment. Protect operator access and capture credential output only once.
4. Provision dedicated Linux worker hosts with cgroups, Docker and gVisor `runsc`.
   Build the pinned toolchain images, scan/sign them in your release system, and load
   approved images on every worker. Workers never pull images while executing jobs.
5. Run the real sandbox suite with `CJUDGE_TEST_RUNTIME=runsc` on these exact hosts.
   Run a load test with representative language/test mixes, not just hello world.
6. Start workers, confirm fresh heartbeats, then start API replicas behind TLS ingress.
   Sample [systemd units](../deploy/systemd) and [nginx config](../deploy/nginx.conf)
   require site-specific users, certificates and secret paths before installation.
7. Publish a canary problem and run `scripts/smoke.py`. Check accepted results, metrics,
   audit transitions, image identities and bounded queue age before enabling traffic.

Compose is a local single-host demo with an owner DB credential; it is not this topology.
Production deployments should use the separate `api` and `worker` Docker build targets
or the same release binary under separate Unix accounts.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `DATABASE_URL` / `DATABASE_URL_FILE` | required, mutually exclusive | PostgreSQL connection secret |
| `CJUDGE_LISTEN` | `:8080` | API listener; bind loopback/private address behind ingress |
| `CJUDGE_WORKERS` | `2` | Concurrent execution slots, 1..64; size per host memory/CPU |
| `CJUDGE_QUEUE_LIMIT` | `1000` | Global queued + running capacity |
| `CJUDGE_REQUESTS_PER_MINUTE` | `120` | Shared per-principal fixed-minute API budget |
| `CJUDGE_MAX_ATTEMPTS` | `3` | Maximum infrastructure attempts, 1..10 |
| `CJUDGE_LEASE` | `30s` | Lease TTL, 6s..5m; renewal every TTL/3 |
| `CJUDGE_JOB_TIMEOUT` | `5m` | Whole-job deadline, 10s..30m |
| `CJUDGE_POLL` | `500ms` | Empty-queue polling interval |
| `CJUDGE_SHUTDOWN` | `20s` | Graceful service shutdown budget |
| `CJUDGE_WORKSPACE` | `/tmp/cjudge` | Dedicated absolute directory visible at same path to Docker daemon |
| `CJUDGE_DOCKER` | `docker` | Docker CLI executable |
| `CJUDGE_RUNTIME` | `runsc` | Required installed sandbox runtime; `runc` only for trusted demos |
| `CJUDGE_CPP_IMAGE` | `cjudge-cpp:1` | Preloaded C++ image (resolved to immutable local ID at startup) |
| `CJUDGE_PYTHON_IMAGE` | `cjudge-python:1` | Preloaded Python image |
| `CJUDGE_GO_IMAGE` | `cjudge-go:1` | Preloaded Go image |

Use the same queue/attempt/rate policy across replicas. Database pools are bounded to
16 connections per process; budget PostgreSQL max_connections for replicas plus
operator, migration and monitoring connections. Transactions time out after 10s at
the statement level; idle transactions after 15s. Do not lengthen lease or job limits
to hide a saturated database or execution host.

## Signals and alerts

`/healthz` reports process liveness. `/readyz` checks the database and required schema
migrations, not execution capacity. A worker heartbeats every 5s and is considered
live for 20s. Missing workers must alert even while API readiness is green.

Scrape `/metrics` on a private network with an administrator bearer token. Keep this
credential in a metrics-system secret file, never a URL. Protect it as a publication
credential as well; v1 does not yet have a metrics-only token scope. Counters are per
API process; job/worker gauges reflect the shared database and must not be summed
across API replicas. Example alerts: [deploy/alerts.yml](../deploy/alerts.yml).

Initial targets, **not certified SLOs**: 99.9% API availability/month, p95 submission
admission under 250ms at the tested burst rate, and p95 queue wait under 30s at planned
sustainable throughput. Alert when workers=0 with queued work, oldest queue age>60s
for 5m, 5xx ratio>1% for 5m, or host free disk/RAM is below the capacity reserve. Monitor
system_error/compile_error rates from completion logs and audit queries by image ID.

## Recovery procedures

**Queue grows:** verify live-worker gauge, Docker preflight, disk, CPU/memory pressure,
and DB lock/claim latency. Stop or throttle admission at ingress if capacity is
exhausted. Add slots only within measured host headroom; add dedicated worker hosts
before oversubscribing existing hosts. Never manually clear leases of live workers.

**Worker crashes:** restart it; expired claims recover automatically. Startup and
30-second maintenance reap only cjudge-labelled containers after their declared
deadline plus 60s. Stale `job-*` workspaces are removed after 32m, longer than the
maximum job timeout. If the daemon is down, cleanup resumes after daemon recovery.
Do not run broad `docker system prune` on a shared host.

**DB outage:** pause admission if needed, restore connectivity/promote the standby,
and let leases recover. Running sandboxes cancel when renewals fail. Clients reuse
idempotency keys after ambiguous responses. Monitor final-attempt recovery and audit
history before deciding whether a system-error submission should be resubmitted.

**Credential leak:** use the operator command `cjudge revoke-principal --id ID`; all
the principal's keys become invalid on subsequent requests. Provision a replacement
principal and distribute its key through a secret channel. Revocation does not cancel
previously admitted jobs. For a worker/DB credential compromise, isolate the host,
rotate that login, and review audit records and database access logs.

**Bad image release:** stop new claims, load the previous approved image, restart
workers (image IDs are resolved at startup), and canary again. In-flight jobs are
canceled/retried during shutdown. Existing terminal results remain immutable; create
new submissions with new idempotency keys to re-evaluate them. Record image IDs when
comparing verdicts.

## Backup, restore and retention

Choose and test an RPO/RTO with the operator; an initial planning target is RPO<=5m,
RTO<=30m. Use continuous WAL archiving/PITR for this target. A nightly logical dump
alone cannot meet it. Encrypt backups, restrict access (they contain source/tests and
credential hashes), and retain copies outside the database host/account.

For a logical recovery rehearsal, use PostgreSQL client tools with a secret-backed
connection (e.g. `PGSERVICE`/`.pgpass`); never paste passwords into command history:

```sh
pg_dump --format=custom --file=cjudge.backup "$BACKUP_DATABASE_URL"
# Provision a separate empty recovery database first.
pg_restore --no-owner --exit-on-error --dbname="$RESTORE_DATABASE_URL" cjudge.backup
```

Reapply `deploy/roles.sql` with the recovery owner and provision login memberships.
Validate problem/submission/audit counts, required migrations, key revocation state,
and the three-language canary before routing traffic. The old primary/workers must
remain isolated during a restore test. Stored running leases expire and recover when
new workers start. Record measured restore duration and last recoverable timestamp.

v1 does not automatically delete customer source or test data. Set a retention policy
before launch. Use an owner-run, bounded maintenance job to archive finished rows,
delete only finished submissions older than the policy, and prune stale heartbeat
rows. Never delete queued/running submissions. Deletion also removes idempotency
protection for those keys; document that window to clients. Archive audit events
independently before retention deletes and periodically test restore from that archive.

## Rollout and rollback

Build and scan immutable artifacts; pin deployed images/digests and version strings.
Apply additive migrations once using the owner job before rolling API/workers. New
binaries refuse startup against an older schema. Stop workers with SIGTERM and allow
at least 45s supervisor grace; hard termination is recoverable but adds retry load.
Use expand/contract migrations for future schema changes; v1 migrations have no
destructive down path. Roll back binaries against a compatible expanded schema, not
by deleting schema objects. For an incompatible migration, restore to a separate DB
and explicitly reconcile admitted work before switching traffic.

CI checks Go vulnerabilities and executes integration/isolation tests. It does not
replace OS/image vulnerability scans, registry signing, gVisor qualification, a
production restore drill, or representative load testing. Review base-image digest
updates from Dependabot and rerun language/sandbox tests for every approved update.

Workspace roots must be real directories owned by the worker's effective UID,
with mode `0700`. Startup, evaluation, and cleanup reject insecure existing roots
and ancestors writable by unrelated users (trusted sticky temporary directories
are permitted). Provision the root before starting Compose, whose worker runs as
UID 0; native/systemd deployments must use their worker UID instead. Do not share
workspace roots between unrelated accounts or grant access through filesystem
ACLs. System-owned parent aliases such as macOS `/tmp` are supported; a symlink
at the workspace root is rejected. Changing ownership/mode of an existing root
requires first checking its contents and stopping workers.

Reapply `deploy/roles.sql` as the owner after migrations or grant changes. It
atomically clears direct schema/table/column/sequence grants on the three managed
group roles before installing the allowlist. This repairs older broad grants;
it does not remove privileges inherited through other roles, PUBLIC privileges,
object ownership, or superuser attributes. Provision dedicated non-superuser
login users with only the intended group membership, and audit those external
sources of authority separately.
