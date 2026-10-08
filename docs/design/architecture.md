# Architecture — current state

> **This document describes reality, not intent.** It is edited in place as the system changes. When it
> disagrees with the plan or with an ADR's stated future, reality wins and the ADR stays as the
> historical record. Intent lives in ADRs.

**Status as of 2026-09-25:** Phase 1 is built, and Phase 2 is built: collection, and the views and
dashboard over it. The collection half has seen a real Claude Code, and the views have been checked
against those two real sessions.

- **Admin API path (Phase 1).** Poller, pseudonymization, both storage targets, the SQL layers over
  each, and a Grafana dashboard. The ClickHouse half is verified against a real server; the DuckDB
  half against a generated store. No adapter has parsed a live response: an individual account
  gets 403 on every Admin endpoint ([note](../research/2026-09-23-admin-api-individual-account.md)).
- **Collector path (Phase 2, collection).** The managed-settings bundle, the twelve-event hook
  pack, and the org-edge collector config writing to three ClickHouse bronze tables. Verified
  against a running collector 0.161.0 and ClickHouse 26.6, including byte-for-byte pseudonym
  parity with the poller, and **against two real Claude Code 2.1.281 sessions** on an individual
  account ([first](../research/2026-09-24-first-real-hook-payloads.md),
  [second](../research/2026-09-24-second-real-session.md)), and since then on 2.1.289 to 2.1.291
  ([note](../research/2026-10-06-http-hooks-wait.md)). All twelve hooked events have arrived, the
  last being `PermissionDenied` on 2.1.290
  ([note](../research/2026-10-06-first-permission-denied.md)). OTel arrives on an individual
  account. **Claude Code waits for every HTTP hook**, so each one gives up after one second
  ([ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md)).
- **Collector path (Phase 2, analysis).** Seven silver views and four gold marts over the
  collector's bronze, and an enablement dashboard. They are verified three ways:
  - against a live ClickHouse with seeded rows, including each rule broken on purpose;
  - against the collector's fixture corpus, run through the real collector;
  - against the two real sessions, whose permission waits, model-switch costs and context sizes
    match the research notes.

  A view names a home-grown artifact or a cohort only once five people are in it
  ([ADR-0029](../adr/0029-minimum-group-size.md)). The views are ClickHouse only
  ([ADR-0028](../adr/0028-file-store-is-admin-api-only.md)).

**Decided on 2026-09-25 and not built.** A design review settled who Cardo is for and what each of
them needs ([ADR-0030](../adr/0030-stakeholders-and-questions.md) to
[ADR-0037](../adr/0037-policy-evidence.md)). The main changes:

- an efficiency family replaces the friction index: its first version reads only what is already
  collected, with metrics pinned to delta, and its views are not built;
- a coach page for each engineer;
- repository identity, classified at the collector: collection is built (below), and no view
  reads it yet;
- teams from a directory file;
- service runs labelled apart from people;
- policy evidence.

**Decided on 2026-10-08 and not built.** Spread is read by how an artifact arrived: for one the
organization rolled out, it is the rollout's reach, and whether the artifact helps is measured in
the same people before and after
([ADR-0040](../adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)). The coach suggests only
what the fleet has shown ([ADR-0041](../adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)).

None of it is described below until it exists. The order it is built in is
[`docs/roadmap.md`](../roadmap.md).

**A gap in what exists: any Grafana login can read every row.** The Grafana data source connects
as the main ClickHouse user, and a `Viewer` can send its own SQL through `/api/ds/query`. This was
measured on 2026-09-25 ([note](../research/2026-09-25-grafana-access-and-shared-links.md)). Only
the operator has a login today, so nothing is exposed. It must be fixed before anyone else gets
one ([risks.md](../../risks.md) #15).

---

## What runs today

```
  Anthropic Admin API
  (Claude Code Analytics)
          |
          | HTTPS, Admin API key, one UTC day per request, cursor-paginated
          v
  +-----------------------------------------------+
  | cardo poll                     cmd/cardo/      |
  |                                                |
  |  console adapter    internal/source/console/   |
  |    - tolerant parsing, drift reported          |
  |    - verbatim payload retained                 |
  |           |                                    |
  |           v                                    |
  |  pseudonymizer      internal/pseudonym/        |
  |    - salted SHA256, salt_version recorded      |
  |    - actor fields deleted from the payload     |
  |    - identity-leak tripwire (fails the run)    |
  |           |                                    |
  |           v   [ only pseudonym.Scrubbed        |
  |               crosses this line ]              |
  |  store              internal/store/jsonl/      |
  +-----------------------------------------------+
          |
          v
          |
          +--------------------------+
          |                          |
          v                          v
  data/bronze/source=console/   ClickHouse  internal/store/clickhouse/
    YYYY-MM-DD.jsonl              cardo.bronze_actor_day
  (Hive-partitioned NDJSON)       (migrations embedded in the binary)
          |                          |
          v                          v
  DuckDB                          ClickHouse
  sql/duckdb/010 -> 020 -> 030    sql/clickhouse/001 -> 002 -> 003
                                     |
                                     v
                                  Grafana  dashboards/cardo-fleet.json
```

Both storage targets exist because ADR-0009 requires both to work: ClickHouse for a real
deployment, and the file store so a five-engineer pilot needs nothing approved by anyone. They hold
the same columns — enforced by a test, not by discipline — and the two SQL dialects publish the
same view and column names, so a dashboard cannot tell which engine it is reading.

The ClickHouse write is atomic per day. Rows land in a staging table and are moved across with
`ALTER TABLE ... REPLACE PARTITION`, so a reader sees either the previous version of a day or the
new one. Deleting the day and re-inserting would leave it empty if the process died in between,
and an empty day on a dashboard is indistinguishable from a day nobody worked.

The store is replace-per-day rather than append, because both analytics APIs revise recent days and
an appending poller would silently double every total.

### The collector path

```
  developer machine                         reference stack / organization's network
  +---------------------------+
  | Claude Code               |   OTLP/HTTP   +----------------------------------------------+
  |  + managed-settings.json  |-------------->| otelcol-contrib 0.161.0   deploy/collector/   |
  |    env:   native OTel on, |               |                                              |
  |           content flags 0 |   POST        |  logs/hooks    webhook_event receiver         |
  |    hooks: 12 events,      |-------------->|    allowlist + derived facts, raw body gone   |
  |           HTTP, 1 s max   |  /v1/hooks    |    names of every received field kept         |
  |                           |               |  logs/otel, metrics/otel    otlp receiver     |
  | nothing installed         |               |    refuse weak salt -> SHA256 pseudonym ->    |
  +---------------------------+               |    classify repository -> drop ids, URLs,     |
                                              |    paths, content                             |
                                              |  every pipeline: tier tag, INV-2 tripwire     |
                                              +----------------------+-----------------------+
                                                                     | INSERT only
                                                                     v
                              ClickHouse  cardo.bronze_hook_events, bronze_otel_logs,
                                          bronze_otel_metrics_sum   (DDL: sql/clickhouse/004,
                                          applied by `cardo migrate`; 90-day TTL)
                                                                     |
                                     silver views, sql/clickhouse/006: session, api_request, turn,
                                     tool_call, policy_decision, context_event, artifact_load
                                                                     |
                                     gold views, sql/clickhouse/007: artifact_usage,
                                     instructions_versions, friction_daily, context_daily
                                       reading cardo.settings (005): the org pattern and the
                                       minimum group size, written by `cardo migrate`
                                                                     |
                                                                     v
                                     Grafana  dashboards/cardo-enablement.json
```

The views are plain views, computed on every read ([risks.md](../../risks.md) #14), and exist in
the ClickHouse dialect only ([ADR-0028](../adr/0028-file-store-is-admin-api-only.md)). Four things
shape them, each measured from a real session:

- **Hook rows have no identity.** They reach a pseudonym and a cohort through their session's
  OTel rows.
- **Hook rows carry the collector's clock.** A duration takes both ends from the laptop's clock
  (OTel), and a row that had to fall back says so in a `clock` column.
- **Hook and OTel prompt ids are the same value.**
- **Every attribute is a string**, cast in silver.

**What a view may name ([ADR-0029](../adr/0029-minimum-group-size.md)).** The minimum group size
applies to two kinds of label:

- **Home-grown artifacts.** A command, skill, subagent or MCP server that does not match the
  organization's pattern is named only once that many people used it that week.
- **Cohorts.** A cohort is named only on a day at least that many people had sessions in it.

Everything below the threshold is counted as `other`. The minimum is five, and an operator may
raise it but not lower it. The organization's own names, Claude Code's built-in subagents and
fleet-wide totals are always shown. The views read the pattern and the size from `cardo.settings`,
which `cardo migrate` writes from `CARDO_ORG_ARTIFACTS` and `CARDO_MIN_GROUP_SIZE`.

Hook payloads carry no identity. A hook row is tied to a person only through `session_id`, which
matches the `session.id` on that session's OTel records, and those carry the pseudonym. Every
processor fails closed (`error_mode: propagate`). A missing, short or non-hex salt refuses OTel
batches with a log line saying why, rather than storing reversible pseudonyms.

**Repositories ([ADR-0035](../adr/0035-repository-identity-classified.md)).** The bundle asks
Claude Code for the repository a session works in, and the collector reduces the URL before
anything is stored. One that matches `CARDO_ORG_REPOS` is kept as `host/owner/name` in
`cardo.repo`, with `cardo.repo.class=org`. Any other is `external`, with only its host, and a blank
pattern makes every repository external. The URL is normalized first, so one pattern covers every
form a git remote takes, and credentials in it are dropped with the scheme. The `vcs.*` attributes
themselves are then deleted. A session outside any repository has neither key. The attributes have
never been seen from a real Claude Code, so which records carry them, and in what URL form, is
still to be confirmed. No view reads them yet.

## Target shape

```
developer machine                     organization's infrastructure
+------------------------+            +--------------------------------------+
| Claude Code            |            | OTel Collector (contrib)             |
|  - native OTel --------+-- OTLP --->|  receivers: otlp, webhookevent       |
|  - managed hook pack   |            |  processors: transform (OTTL)        |
|    (type:"http",       +-- HTTPS -->|    - SHA256(email+salt) -> pseudonym  |
|     timeout:1)         |            |    - delete_key(user.email)          |
|                        |            |    - tier tagging                    |
| NO INSTALLED BINARY    |            |  exporters: clickhouse               |
+------------------------+            +---------------+----------------------+
        ^                                             v
        | managed-settings.json                +-----------------+
        | (MDM / GPO / Intune)                 | ClickHouse      |
        |  - CLAUDE_CODE_ENABLE_TELEMETRY      |  bronze: raw    |
        |  - OTEL_EXPORTER_OTLP_ENDPOINT       |  silver: views  |
        |  - 12 HTTP hooks, 1 s each at most   |  gold:   marts  |
        |  - OTEL_LOG_* all at 0 (redacted)    +--------+--------+
        |  - and nothing else (ADR-0039)                v
                                               +-----------------+
  Anthropic Admin API ---> cardo poller ------>| Grafana         |
  (zero-install tier)                          +-----------------+
```

## Components

| Component | Path | Status | ADR |
|---|---|---|---|
| Managed settings bundle | `deploy/managed-settings/` | **built** — production template and a local-evaluation variant | [0007](../adr/0007-enrollment-posture.md), [0039](../adr/0039-hooks-wait-at-most-one-second.md) |
| Hook pack (12 HTTP hooks) | inside the bundle's `hooks` block | **built**, observed from Claude Code 2.1.281 to 2.1.291: 11 of 12 events have arrived | [0005](../adr/0005-collection-mechanism.md), [0039](../adr/0039-hooks-wait-at-most-one-second.md) |
| Collector config | `deploy/collector/` | **built**, verified against a running collector 0.161.0 and two real Claude Code sessions; repository classification verified against the collector only | [0006](../adr/0006-pseudonymization.md), [0012](../adr/0012-ingest-implementation.md), [0025](../adr/0025-artifact-names-kept-with-guardrails.md), [0026](../adr/0026-stale-instructions-by-versioned-name.md), [0035](../adr/0035-repository-identity-classified.md), [0039](../adr/0039-hooks-wait-at-most-one-second.md) |
| Collector bronze tables | `sql/clickhouse/004_bronze_collector.sql` | **built** | [0016](../adr/0016-retention.md), [0025](../adr/0025-artifact-names-kept-with-guardrails.md) |
| Silver views over collector data | `sql/clickhouse/006_silver_collector.sql`, `008_silver_model_switch.sql`, `009_silver_model_switch_cost.sql` | **built** — seven views; verified against seeded rows, the fixture corpus through the collector, and two real sessions | [0025](../adr/0025-artifact-names-kept-with-guardrails.md), [0028](../adr/0028-file-store-is-admin-api-only.md) |
| Gold marts over collector data | `sql/clickhouse/007_gold_collector.sql` | **built** — artifact usage, instructions versions, friction, context; the minimum group size applied | [0008](../adr/0008-outcome-variable.md), [0026](../adr/0026-stale-instructions-by-versioned-name.md), [0029](../adr/0029-minimum-group-size.md), [0039](../adr/0039-hooks-wait-at-most-one-second.md) |
| View settings | `sql/clickhouse/005_settings.sql`, `internal/store/clickhouse/settings.go` | **built** — the org pattern and the minimum group size, written by `cardo migrate` | [0029](../adr/0029-minimum-group-size.md) |
| `cardo poll` CLI | `cmd/cardo/` | **built** | [0017](../adr/0017-v01-scope.md) |
| `cardo migrate` CLI | `cmd/cardo/` | **built** — the schema, and the two view settings; the collector path needs it because the exporter never creates tables | [0019](../adr/0019-sql-tooling.md), [0029](../adr/0029-minimum-group-size.md) |
| Console adapter (Claude Code Analytics API) | `internal/source/console/` | **built**, unverified against live API | [0017](../adr/0017-v01-scope.md), [0021](../adr/0021-analytics-source-scope.md) |
| Enterprise adapter (Claude Enterprise Analytics API) | — | not built; in scope for v1 | [0021](../adr/0021-analytics-source-scope.md), risks #8 |
| Pseudonymizer | `internal/pseudonym/` | **built** | [0006](../adr/0006-pseudonymization.md) |
| File store (no-infrastructure mode) | `internal/store/jsonl/` | **built** | [0009](../adr/0009-storage-agnostic-clickhouse-reference.md) |
| ClickHouse store + migration runner | `internal/store/clickhouse/` | **built**, verified against ClickHouse 26.6 | [0009](../adr/0009-storage-agnostic-clickhouse-reference.md), [0019](../adr/0019-sql-tooling.md), [0022](../adr/0022-clickhouse-access.md) |
| SQL layers (DuckDB) | `sql/duckdb/` | **built**, verified against a generated store | [0010](../adr/0010-layered-schema.md) |
| SQL layers (ClickHouse) | `sql/clickhouse/` | **built**, verified against a live server | [0010](../adr/0010-layered-schema.md), [0016](../adr/0016-retention.md) |
| Fixture corpus + contract tests | `test/fixtures/` | **built**. Console: from documentation. Hooks: field names observed from 2.1.281 with synthetic values, plus the documented shapes | [0014](../adr/0014-version-drift.md) |
| Invariant tests | `test/invariants_test.go`, `test/deploy_test.go`, `test/collector_test.go`, `test/bundle_test.go` | **built** — INV-1 through INV-7 | `invariants.md` |
| Reference stack | `deploy/compose/` | **built** — ClickHouse + Grafana + collector, loopback-bound; an opt-in overlay serves the collector's two ports over the network through a TLS proxy (Caddy), never run with a team | [0009](../adr/0009-storage-agnostic-clickhouse-reference.md) |
| Dashboards | `dashboards/` | **built** — the fleet dashboard (Admin API, 9 panels) and the enablement dashboard (collector, 13 panels) | [0009](../adr/0009-storage-agnostic-clickhouse-reference.md), [0008](../adr/0008-outcome-variable.md), [0029](../adr/0029-minimum-group-size.md) |

## The hook pack

12 events. This list is the mandatory-tier boundary and changing it requires an ADR (INV-4).
[ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md) took out `SessionStart`, which Claude Code
runs no HTTP hook for, and read model switches from `PostModelSwitch` instead of `PreModelSwitch`.
Each hook gives up after one second, because Claude Code waits for it.

What each event gives Cardo, as observed from Claude Code 2.1.281 in two sessions
([first](../research/2026-09-24-first-real-hook-payloads.md),
[second](../research/2026-09-24-second-real-session.md)), and `PostModelSwitch` from 2.1.289
([note](../research/2026-10-06-http-hooks-wait.md)):

| Event | Why it is here |
|---|---|
| `SessionEnd` | Session boundary and end reason. Arrives even on `/exit` |
| `UserPromptSubmit` | **Metadata only**: length and `permission_mode`. Never the text (INV-5) |
| `UserPromptExpansion` | Slash command name and source: which commands are used, the organization's and engineers' own |
| `PermissionRequest` | Start of permission-wall timing. Carries the tool and, inside a subagent, the agent. **No `tool_use_id`** |
| `PermissionDenied` | Auto-mode denials only. It does **not** fire when a person answers a prompt. Carries the tool and `tool_use_id`, which OTel's `tool_decision` (`reject`, `source=config`) shares. Not yet read by any view |
| `PreCompact` | Compaction trigger. The token counts are on OTel's `claude_code.compaction` event, not here |
| `PostCompact` | Compaction trigger |
| `InstructionsLoaded` | Load reason, memory type, kind of file, and the name of the organization's own files ([ADR-0026](../adr/0026-stale-instructions-by-versioned-name.md)). Fires for files a `CLAUDE.md` imports with `@` (`load_reason=include`) and for path-scoped rules when a matching file is read (`path_glob_match`). **No content hash is sent** |
| `SubagentStart` | Subagent usage |
| `SubagentStop` | Subagent end. Also fires for Claude Code's own helpers, prompt suggestion and compaction, with no `SubagentStart`: count subagents by pairing the two on `agent_id` |
| `PostModelSwitch` | Model policy, and what the switch cost: cache warmth and Claude Code's own estimate of the cache rewrite. Read after the switch, because a timed-out `PreModelSwitch` hook blocks it; only switches someone asked for are counted, not a fallback or a resume ([ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md)) |
| `ConfigChange` | Which settings source changed during a session |

**Skill usage needs no hook**: `skill.name` is already an attribute on the native
`claude_code.cost.usage` and `claude_code.token.usage` metrics, and it carries the real name of a
custom command even with every content flag off.

**Permission-wall time** runs from the moment Claude Code runs the `PermissionRequest` hooks to
the OTel `claude_code.tool_decision` event. **Both ends are on the client's clock.**

- **The start** is Claude Code's own `claude_code.hook_execution_start` with
  `hook_name=PermissionRequest:<tool>`. It fires on every permission prompt, because the bundle
  registers a `PermissionRequest` hook.
- **The end** is the next `claude_code.tool_decision` whose `source` is a person's.
- **The join** is on session, prompt and tool, since the hook carries no `tool_use_id`.

It gave 12.3 s and 10.6 s on the two real prompts observed. It does *not* use the
`tool.blocked_on_user` span, which requires `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`
([ADR-0008](../adr/0008-outcome-variable.md)).

**The `PermissionRequest` hook row's own time is not used.** Hook payloads carry no timestamp, so
that row carries the collector's receive time, on a different clock. On the reference stack it was
1 to 3 s behind the client and not steady, which had added about 1.3 s to each of those waits
([note](../research/2026-09-24-second-real-session.md)). `hook_execution_start` is undocumented.
If it disappears, the hook row's time is the fallback, and a wait computed from it must say that
it spans two clocks.

It is built as `silver_policy_decision`, and each row says which clock its start came from.

**What each hook stores** is the allowlist in `deploy/collector/config.yaml`, not the payload
Claude Code sends: never prompt text, tool input, model output or paths
([ADR-0023](../adr/0023-hook-payload-allowlist.md), upheld by
[ADR-0025](../adr/0025-artifact-names-kept-with-guardrails.md)). Facts that Claude Code sends under
generic names (`reason`, `trigger`, `source`, `prompt`) are mapped for one named event onto a
specific name. Names of commands, subagents and MCP servers are kept as sent. The views show a
name only once enough people use it. `CARDO_ARTIFACT_NAMES=org-only` is the strict mode, which
stores every name not matching `CARDO_ORG_ARTIFACTS` as `custom`, on both paths.

The hook receiver accepts bodies up to 16 MiB. Its default of 100 KiB lost two real `SubagentStop`
events, refused with a 400 that the collector does not log. It waits up to 10 s to receive a
request and to answer it. Its default of 500 ms lost a real `UserPromptSubmit` from a Claude Code
busy running two other hooks, and Claude Code showed "socket hang up" under the prompt.

## How the invariants are enforced, not merely stated

| Invariant | Mechanism |
|---|---|
| **INV-1** no transcript reads | CI scans every non-test Go file for `.claude/projects` and related paths |
| **INV-2** no email persisted | The store accepts only `pseudonym.Scrubbed`, a type with no exported constructor — the sole way to obtain one is `Hasher.Scrub`. A behavioural test then runs the whole pipeline over a payload containing an address and greps every byte written to disk |
| **INV-2** defence in depth | After scrubbing, the payload is searched for the identifier and for anything shaped like an email address, with the collector tripwire's pattern. A match aborts the run rather than storing the row: a partial scrub is the exact failure the invariant exists to prevent. A behavioural test puts someone else's address in a field the adapter does not model and requires the poll to stop with nothing of it on disk |
| **INV-3** cohort-only | CI parses `sql/` and fails any `gold_*` view, in either dialect, that selects a pseudonym. `COUNT(DISTINCT pseudonym)` is allowed; selecting it is not. Until 2026-09-25 the check matched only the unqualified `view gold_`, so it never read a ClickHouse view; it now fails if it finds none |
| **INV-3** minimum group size | A live test seeds seven people, with cohorts of five and two, and requires the view to name the five and fold the two into `other`. It also requires the size to be raiseable and to stay at five when a lower value is written by hand. `cardo migrate` refuses a value below five ([ADR-0029](../adr/0029-minimum-group-size.md)) |
| **INV-5** no content | The bronze column set is an allowlist checked against a real written row |
| **INV-7** no phone-home | CI scans for outbound hosts and allows only `api.anthropic.com` |
| **INV-3** on dashboards | CI parses every panel query in `dashboards/`. A panel may not name a pseudonym, and may not read bronze or silver directly — that is where "just one per-person panel" would actually be added, and it touches no SQL. It governs what a dashboard shows, not what a Grafana login can query: that is bounded only by the data source's database user ([risks.md](../../risks.md) #15) |
| **INV-5** across both stores | The bronze column set is one list checked against three things: the test itself, a real row written by the JSONL store, and the ClickHouse DDL. A column added to one store and not the other fails here |
| **ADR-0008** volume denominator (carried into [ADR-0031](../adr/0031-what-works-means.md)) | CI fails any dashboard panel charting a rate without its volume as an output column beside it. It covers the acceptance and edit rejection rates with proposals, permission waits with the prompts answered, compactions per session with sessions, and an artifact's share with the active people. A name elsewhere in the SQL does not count, because each rate is computed from its volume. The constraint is a product decision, so it is enforced like one |
| **INV-2** on the collector path | CI runs the real collector (`collector` job), sends OTel carrying a mixed-case email on the resource, the record and the data point, and requires the stored pseudonym to equal the poller's for the same salt. A second test puts an email in an attribute nothing knows about, on a log record beside a clean one and on a data point, and requires the tripwire to refuse each request and store nothing from either, while a clean request sent after them is stored |
| **INV-5** on hook payloads | Every observed and documented hook shape goes through the running collector carrying marker strings in each content field, including one past 100 KiB; CI byte-searches what reached ClickHouse for the markers. The allowlist itself is pinned in a test, so adding a field is a two-place change |
| **INV-5** on generic hook fields | A statement that reads `reason`, `trigger`, `source`, `prompt`, `message` or `error` must name the event it applies to. Prose under an enum's name, and text under a numeric name, are dropped, and live fixtures check both |
| **ADR-0025** strict naming | Every statement reading `CARDO_ARTIFACT_NAMES` must use the fail-closed form, and CI runs the real collector in both modes |
| **ADR-0035** repositories | Every match on `CARDO_ORG_REPOS` must rule out a blank pattern. CI runs the real collector with a pattern and without one. It sends each form a git remote takes, credentials included, on the resource, the record and the data point. Organization repositories must be kept as `host/owner/name` and every other reduced to its host. No `vcs.*` key and no credential may be stored |
| **INV-5** in the bundle | Every content flag must be present and `"0"`. Repository identity must be on, because the collector classifies it |
| Delta temporality | The bundle must pin metrics to delta, because a count is the sum of a metric's points |
| **INV-4** hook pack | The bundle must hook exactly the twelve published events, and never `SessionStart` or `PreModelSwitch` |
| **INV-6** | Nothing under `deploy/` or `dashboards/` may mention `requiredMaximumVersion` |
| **INV-7** on the collector | Every exporter must be ClickHouse; no extensions; the collector's own metrics off |
| Fail-closed collector | Every processor must use `error_mode: propagate`; identity pipelines must start with the salt refusal and the hash, and every pipeline must end with the tripwire |
| **ADR-0039** | The bundle may set only `env` (telemetry variables) and `hooks`; each hook only `type: "http"`, `url` and a `timeout` of at most one second |
| Slow hook senders | The collector's hook receiver must wait 10 s for a request. CI sends one with its headers late and one with its body late, and both must be answered and stored |

Each of these was verified to fail when the invariant is deliberately broken, rather than merely
observed to pass. For the collector that meant breaking the running config, not just the file:
`cwd` put back on the allowlist, and the pseudonym's 0x1F separator removed. That exercise is also
how it came out that Go caches live test results, so every live test now runs with `-count=1`.
After the first real session, the new guards were broken the same way: the body limit removed, the
strict-mode check written fail-open, a generic field read for every event, and the enum and type
guards removed. The dashboard and column-set checks above were verified the same way: a
`gold_leaderboard` panel, a rate panel with its denominator removed, and a mismatched column list
each failed before the tripwire was deleted.

The collector-path views were broken the same way, in the live ClickHouse, one rule at a time, and
each broken rule failed a test:

- the name threshold removed;
- the cohort threshold removed;
- the floor of five removed from `settings_effective`;
- permission waits and model switches timed from the hook row.

The volume check was run against each friction panel and the fleet panel with its volume removed.
It first passed them all, because each rate is computed from its own volume. That is why it now
requires an output column. The INV-3 check was run against a pseudonym planted in a ClickHouse gold
view, which the old pattern had never read.

## What deliberately does not exist

- **No agent, daemon or binary on developer machines.** The entire client side is a settings file
  ([ADR-0011](../adr/0011-hook-transport-http.md)).
- **No policy in the bundle.** No `allowManagedHooksOnly`, no `allowedHttpHookUrls`, no permission
  rules, no version pins. Installing Cardo changes nothing about Claude Code for an engineer except
  that telemetry is sent, and each hooked event waits for the collector's answer, a second at most
  ([ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md)).
- **No raw hook payload anywhere.** Not in bronze, not in a debug log, not in the fixtures, which
  carry field names observed from a real Claude Code and synthetic values. Drift is detected from
  field *names* ([ADR-0023](../adr/0023-hook-payload-allowlist.md)).
- **No DuckDB mode for the collector path.** The collector writes to ClickHouse only, and its views
  exist in the ClickHouse dialect only. ADR-0009's file store serves the Admin API path
  ([ADR-0028](../adr/0028-file-store-is-admin-api-only.md)).
- **No transcript reader** (INV-1).
- **No hosted service, no phone-home** (INV-7).
- **No custom code on the hook ingest path** while OTTL holds
  ([ADR-0012](../adr/0012-ingest-implementation.md)).
- **No unsalted mode and no default salt.** `cardo poll` refuses to start without `CARDO_SALT`, and
  rejects one shorter than 32 characters or not hex, as the collector does. Email addresses are
  low-entropy and guessable, so a weak salt yields pseudonyms that are trivially reversible by
  anyone holding them — a store that looks pseudonymous and is not is worse than one that does not
  run ([ADR-0006](../adr/0006-pseudonymization.md)).
- **No `-base-url` flag.** The endpoint is fixed at `api.anthropic.com`, so the binary cannot be
  pointed at an arbitrary collector (INV-7). Tests override it through an unexported option.
- **No materialized gold marts, and therefore no trends older than 90 days.** ADR-0016 retains
  tier-1 rows for 90 days and intends derived aggregates to survive indefinitely. The gold marts
  are views over bronze, so when bronze expires the aggregates computed from it expire with it,
  silently, on a background merge. The fix is a cohort-only rollup table written by the poller;
  it is not built ([risks.md](../../risks.md) #10). The collector-path marts have the same limit,
  and no poller runs on that path to write a rollup.
- **No session-grain views from this source.** The analytics APIs report a daily aggregate per actor
  and can never answer session-level questions. `silver_actor_day` is its own grain and does not
  pretend to be the session model in [data-model.md](data-model.md)
  ([ADR-0017](../adr/0017-v01-scope.md)).
