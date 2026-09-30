# Org-edge collector

A stock **OpenTelemetry Collector (contrib) 0.161.0** with one configuration file. No code Cardo
wrote touches an event on this path ([ADR-0012](../../docs/adr/0012-ingest-implementation.md)).
[`config.yaml`](config.yaml) is the privacy contract for everything Claude Code sends, and it is
written to be read. Start there. This page covers only how to run it.

## What it does, in order

| Pipeline | From | Processing | To |
|---|---|---|---|
| `logs/hooks` | the hook pack, `POST /v1/hooks` on 8088 | allowlist and derive, tag, INV-2 tripwire | `cardo.bronze_hook_events` |
| `logs/otel` | Claude Code OTel, OTLP/HTTP on 4318 | refuse a weak salt, pseudonymize, strip content, tag, tripwire | `cardo.bronze_otel_logs` |
| `metrics/otel` | the same | the same | `cardo.bronze_otel_metrics_sum` |

Why the hook path is an allowlist and the OTel path is not:
[ADR-0023](../../docs/adr/0023-hook-payload-allowlist.md). What it keeps of names, instructions
files and model switches: [ADR-0025](../../docs/adr/0025-artifact-names-kept-with-guardrails.md),
[ADR-0026](../../docs/adr/0026-stale-instructions-by-versioned-name.md) and
[ADR-0027](../../docs/adr/0027-model-switch-cost.md).

The hook receiver accepts bodies up to 16 MiB. Its default is 100 KiB, and it refuses anything
larger with a 400 and **no log line on the collector**. Real payloads carry whole prompts and
subagent replies, and cross it. The only trace of a lost hook is on the client, in Claude Code's own
`claude_code.hook_execution_complete` event (`num_non_blocking_error`).

## Environment

| Variable | Required | |
|---|---|---|
| `CARDO_SALT` | yes | At least 32 characters, hex (`openssl rand -hex 32`). **The same salt as `cardo poll`**, or one engineer gets two pseudonyms. Held by the security team ([ADR-0006](../../docs/adr/0006-pseudonymization.md)) |
| `CARDO_SALT_VERSION` | no | Default `v1`. Must match the poller's |
| `CARDO_CLICKHOUSE_ENDPOINT` | yes | Native protocol, for example `tcp://clickhouse:9000` |
| `CARDO_CLICKHOUSE_USER`, `CARDO_CLICKHOUSE_PASSWORD` | yes | Needs INSERT on the three bronze tables, nothing else |
| `CARDO_ORG_ARTIFACTS` | no | A regex naming your own commands, subagents, MCP servers and instructions files. It labels what you shipped, and it is the only way an instructions file's name is kept. Unset or empty names nothing |
| `CARDO_ARTIFACT_NAMES` | no | `all` (default) keeps the names of commands, subagents and MCP servers engineers define themselves. Any other value is the strict mode: every name not matching `CARDO_ORG_ARTIFACTS` is stored as `custom`, on hooks and on OTel `skill.name` |

**Without a usable salt the collector does not stop.** It refuses every OTel batch with HTTP 503,
and logs a line starting `REFUSED - CARDO_SALT is unset or shorter than 32 characters`. Hook events
carry no identity, so they keep flowing.

## Before the first start

The exporter only ever sends INSERT (`create_schema: false`), so its tables must exist first:

```bash
CARDO_CLICKHOUSE_URL=http://clickhouse:8123 CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
  cardo migrate
```

The reference stack ([`deploy/compose/`](../compose/)) does this with a one-shot service that the
collector waits for.

## Exposure

- **TLS.** Terminate it at the collector (the receivers' `tls:` block) or at a proxy in front of it.
  Hook payloads cross the network carrying prompt text before the collector discards it.
- **Network.** Reachable from developer machines and from nowhere else. The hook endpoint has no
  authentication, and a token in the bundle would not be a secret
  ([ADR-0024](../../docs/adr/0024-bundle-configures-telemetry-only.md)).
- **The collector's own telemetry is off.** No internal metrics endpoint, no extensions, warnings
  only. `TestINV7_CollectorExportsOnlyToClickHouse` fails if an exporter to anywhere but ClickHouse
  appears.

## Upgrading the collector

Treat it as a code change, not a tag bump. OTTL syntax and component names move between releases:
`webhookevent` became `webhook_event` while this was being built. The exporter's column set can
change too, and our DDL mirrors it. The procedure:

1. Bump the image tag here, in `deploy/compose/docker-compose.yml`, and in `.github/workflows/ci.yml`.
2. Diff the exporter's `internal/sqltemplates` between the two tags. A column change is a **new**
   migration in `sql/clickhouse/`, never an edit to `004`.
3. `make collector-test` against the stack, and let the CI `collector` job run.

## Idle cost

Measured at 0.02–0.08% of one core and 46 MiB with no traffic. The exporter batches on a 10-second
timer, so the collector does not wake ClickHouse more often than that.
