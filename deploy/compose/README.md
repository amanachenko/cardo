# Reference stack — ClickHouse + Grafana + collector

The evaluation deployment from [ADR-0009](../../docs/adr/0009-storage-agnostic-clickhouse-reference.md):
one ClickHouse, one Grafana, one org-edge collector, one machine. It exists so a platform team can
see the whole pipeline working before being asked to approve anything, so one engineer can point
their own Claude Code at it without asking anyone, and so the SQL in `sql/clickhouse/` and the
collector config have somewhere to be tested for real.

It is **not** a production topology. No replication, no backups, no TLS, no auth in front of
Grafana beyond an admin password.

## Run it

```bash
cp .env.example .env          # change both passwords; set CARDO_SALT for the collector
docker compose up -d
```

| Service | Address | Notes |
|---|---|---|
| ClickHouse HTTP | `127.0.0.1:8124` | what `cardo poll -store clickhouse` writes to |
| ClickHouse native | `127.0.0.1:9001` | for `clickhouse-client` |
| Grafana | `127.0.0.1:3001` | dashboards provisioned from `dashboards/` |
| Collector, OTLP/HTTP | `127.0.0.1:4319` | Claude Code's native telemetry |
| Collector, hooks | `127.0.0.1:8089` | the hook pack, `POST /v1/hooks` |
| `migrate` | — | one-shot: builds `cardo`, applies the schema and the two view settings, exits. The collector waits for it |

`docker compose up` builds the `cardo` image the first time, which pulls a Go toolchain image once.
After pulling new code, run `docker compose up -d --build migrate`, because the image embeds the
migrations.

Grafana has two dashboards. **Cardo — fleet adoption** reads the Admin API data.
**Cardo — enablement** reads the collector's data. It shows:

- whether the organization's artifacts are used;
- who is on which version of its instructions files;
- what engineers built that several now use;
- friction, context size and model-switch costs.

It names a home-grown artifact or a cohort only once five people are in it (ADR-0029). Set
`CARDO_ORG_ARTIFACTS` in `.env` so it can tell what your organization shipped. `CARDO_MIN_GROUP_SIZE`
raises the five, and nothing lowers it.

**To send your own Claude Code sessions here**, see "Trying it on your own machine" in
[`deploy/managed-settings/README.md`](../managed-settings/README.md). It is one `--settings` flag and
nothing is installed. Without `CARDO_SALT` in `.env` the collector still starts, but it refuses every
OTel batch and says why in `docker compose logs collector`.

To load the Admin API path instead, point the poller at it:

```bash
export CARDO_SALT=$(openssl rand -hex 32)      # custody belongs with your security team
export CARDO_ADMIN_KEY=sk-ant-admin01-...
export CARDO_CLICKHOUSE_URL=http://127.0.0.1:8124
export CARDO_CLICKHOUSE_USER=cardo CARDO_CLICKHOUSE_PASSWORD=...
cardo poll -store clickhouse -days 30
```

The schema is applied automatically on every run, from migrations embedded in the binary.

No key yet? `make sample-seed` writes invented data under `source='sample'` so the dashboards have
something to draw. `make sample-drop` removes it.

## Three things in here that are deliberate

**Every published port is bound to loopback.** This stack holds pseudonymous engineering telemetry.
A `0.0.0.0` binding would put it on every network the host is attached to, including hotel wifi.
INV-7 is a property of the deployment as much as of the code.

**Non-default host ports** (8124, 3001 rather than 8123, 3000), so this can coexist with whatever
already took the obvious ports on a developer's machine. Inside the compose network the containers
still use the standard ones.

**`restart: unless-stopped`, not `always`.** With `always`, `docker compose stop` is undone by the
next Docker Desktop launch and the stack quietly runs forever. With `unless-stopped` a stop sticks —
which is also what lets Docker Desktop idle its VM, since that only engages when no container is
running. Prefer `make stack-stop` over leaving it up.

## Getting at ClickHouse directly

You do not need to for normal use — Grafana is the interface, and the poller applies the schema
itself. When you do want a SQL prompt, there are two and neither needs anything installed:

**The built-in web UI**, which ships inside ClickHouse itself: <http://127.0.0.1:8124/play>. The
page loads without credentials and asks for them on the first query; use the ClickHouse user and
password from `.env`, *not* the Grafana ones. It is a query box and a result grid, which is all
that is wanted here.

**The CLI**, already inside the container:

```bash
docker compose exec clickhouse clickhouse-client --user cardo --password "$CLICKHOUSE_PASSWORD"
```

Useful first queries, all of them cohort-level by construction (INV-3):

```sql
SELECT * FROM cardo.gold_fleet_adoption ORDER BY day DESC LIMIT 10;
SELECT * FROM cardo.gold_cost_daily ORDER BY day DESC LIMIT 10;
SELECT * FROM cardo.gold_schema_drift;        -- fields the adapter does not model yet
SELECT version, applied_at FROM cardo.schema_migrations FINAL ORDER BY version;
```

## ClickHouse idle cost

Two overlays in `clickhouse/config.d/`, for two different problems. Both exist because a stock
ClickHouse is configured for a production cluster under continuous ingest, and this stack is one
node holding tens of kilobytes that is written to once a day.

**`10-cardo-quiet.xml` — how much it writes about itself.** A stock single-node ClickHouse writes
and merges its own diagnostics continuously whether or not anyone is talking to it: on one measured
instance, 894 MiB of `system.*` tables against 1.65 MiB of real data over 26 days, every merge in a
sampled hour being on a system table and none on the application's. Those tables ship with no TTL,
so it accumulates indefinitely, and under Docker Desktop each write crosses the VM boundary and
surfaces on the host as battery drain. `query_log` and `error_log` are kept, capped at three days.

**`20-cardo-idle.xml` — how often it wakes up.** Silencing the logs left about 5% of one core
burning on a server with no traffic at all. Sampling `utime+stime` per thread out of `/proc` and
then taking stack traces of whichever threads were busy found the cause, and it was not any of the
things that looked likely:

```
                                        CPU (30s sample, idle)   memory   threads
stock, logs already silenced                   4.63% of a core   274 MiB      755
+ timer and pool tuning                        4.63% of a core   119 MiB      121
+ async_insert_threads=0                       0.40% of a core    96 MiB      106
+ memory_worker_period_ms=2000                 0.17% of a core    96 MiB      106
```

Every busy thread was parked inside `DB::AsynchronousInsertQueue`: sixteen of them, the default
`async_insert_threads`, each waking every 200 ms to check a queue that coalesces small concurrent
inserts from many clients. Cardo writes one batch per source per day, so there is nothing to
coalesce. Note the second row — the timer and pool changes, which were the obvious suspects and are
individually defensible, bought nothing measurable. They are kept because they are correct for this
deployment, not because they were the answer.

`async_insert` defaults to **on** in ClickHouse 26.x, which is also why `cardo` now sends
`async_insert=0` with its own inserts rather than trusting a server it does not control. That change
is about correctness rather than cost, and the reasoning is in `Insert` in
`internal/store/clickhouse/client.go`.

Each file's header carries the measurement behind it and the diagnostic capability it costs.

The collector needed no tuning: measured at 0.02–0.08% of one core and 46 MiB idle. Its exporter
batches on a 10-second timer, so it does not wake ClickHouse more often than that.

> Adding a new overlay file means **two** edits: the file, and a `volumes:` line in
> `docker-compose.yml`. `config.d` is mounted a file at a time so it does not shadow the image's own
> `docker_related_config.xml`, so an unmounted overlay is simply ignored — the server starts clean,
> logs nothing and runs on defaults. `TestDeployOverlaysAreAllMounted` fails if one is missing.

## Retention deletes data

`bronze_actor_day` carries `TTL day + INTERVAL 90 DAY DELETE`, per
[ADR-0016](../../docs/adr/0016-retention.md): with a stable salt, retention rather than hashing is
what actually bounds exposure. Rows older than 90 days are removed on a background merge.

Because the gold marts are views over bronze rather than materialized tables, **trends older than
90 days disappear along with the rows**. ADR-0016 intends aggregates to be kept indefinitely and
that part is not built yet — see [risks.md](../../risks.md) #10.
