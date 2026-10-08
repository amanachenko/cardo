# Data model — current state

> Edited in place; describes reality. **Status 2026-09-25:** bronze, silver and gold are built for
> both collection paths. The collector path's are ClickHouse only, and they have been checked against
> two real Claude Code sessions.

Three layers ([ADR-0010](../adr/0010-layered-schema.md), whose layering
[ADR-0023](../adr/0023-hook-payload-allowlist.md) and [ADR-0025](../adr/0025-artifact-names-kept-with-guardrails.md)
uphold). Two storage targets: ClickHouse
(reference) and a file store read by DuckDB (evaluation mode)
([ADR-0009](../adr/0009-storage-agnostic-clickhouse-reference.md)). The file store covers the Admin
API path only ([ADR-0028](../adr/0028-file-store-is-admin-api-only.md)).

## bronze — raw, not normalized

Nothing is normalized or aggregated at this layer. What "raw" means differs by path, and the
difference is deliberate.

### Admin API path — `bronze_actor_day` (ClickHouse) / `bronze/source=*/*.jsonl` (file store)

One row per actor per day, written by `cardo poll`. `payload` is the API's record **verbatim**,
except that the actor's email has been deleted from it. The Admin API returns aggregates and no
content, so verbatim is safe.

| Column | Notes |
|---|---|
| `source`, `day` | partition key; a day is replaced atomically, never appended to |
| `pseudonym`, `salt_version` | salted SHA256 of the email, and which salt ([ADR-0006](../adr/0006-pseudonymization.md)). **Never the email** (INV-2) |
| `tier` | always `1` today |
| `actor_type`, `org_id`, `customer_type`, `terminal_type` | lifted out of the payload for filtering |
| `payload` | the record, verbatim minus identity |
| `unknown_fields` | fields the adapter does not model yet — the drift signal ([ADR-0014](../adr/0014-version-drift.md)) |

### Collector path — `bronze_hook_events`, `bronze_otel_logs`, `bronze_otel_metrics_sum`

Written by the OpenTelemetry Collector's ClickHouse exporter. The DDL is ours
(`sql/clickhouse/004_bronze_collector.sql`), and the columns are the exporter's own, because it
names them in its INSERT. Everything of interest lives in three maps:

| Where | What |
|---|---|
| `LogAttributes['user.pseudonym']`, `Attributes['user.pseudonym']` | OTel only. The same construction as the poller, so one engineer has one pseudonym across both paths. The collector hashes the email where it finds it. Claude Code 2.1.281 sends it on each record and data point, so that is where the pseudonym is. An email sent on the resource would leave it in `ResourceAttributes`, and silver reads both |
| `ResourceAttributes['cardo.tier']`, `['cardo.salt_version']` | Every row, on both tables |
| `ResourceAttributes['cardo.cohort']` | OTel only: the team, from the bundle's `OTEL_RESOURCE_ATTRIBUTES` |
| `cardo.repo.class`, `cardo.repo` | OTel only, on whichever map Claude Code put the repository on (not yet observed). `org` with `host/owner/name` for a repository matching `CARDO_ORG_REPOS`; `external` with the host alone, or empty, for any other. Absent outside a repository, or from a client too old to send it. Never the URL ([ADR-0035](../adr/0035-repository-identity-classified.md)). No view reads it yet |
| `LogAttributes` / `Attributes` | OTel: Claude Code's attributes, minus identity, `vcs.*`, paths and content. Hooks: the allowlist, plus `cardo.received_keys` |
| `EventName` | `claude_code.<event>` for OTel logs; the hook event name for hooks |
| `AggregationTemporality` | Metrics only. `1`, delta, because the bundle pins it: each point is the change since the previous export, so a count is the sum of its points. A `2` would be a running total, repeated on every export |
| `Body` | Always empty. It is the one field a log record lets carry arbitrary text, so the collector empties it rather than trusting it |

**Hook rows carry no identity.** The payloads never had one. A hook row reaches a person only
through `LogAttributes['session_id']`, which equals `LogAttributes['session.id']` on that
session's OTel rows, and those carry the pseudonym. If OTel is off, hook rows are anonymous
session-level events.

**Hook bronze is not verbatim** ([ADR-0023](../adr/0023-hook-payload-allowlist.md)). Payloads carry
prompt text, command lines and paths by design, so only an allowlist is kept. The *names* of every
field received are kept in `cardo.received_keys`, so a new field is visible as drift even though
its value was dropped.

- **Derived before the content goes:** `prompt_length`, `instructions_file`, `instructions_scope`,
  and `instructions_name`. The last is kept only for the organization's own files
  ([ADR-0026](../adr/0026-stale-instructions-by-versioned-name.md)).
- **Renamed from generic names, for one event each:** `session_end_reason` (from `reason`),
  `compaction_reason` (from `trigger`), `config_source` and `model_switch_source` (from `source`).
- **Artifact names are stored as sent** ([ADR-0025](../adr/0025-artifact-names-kept-with-guardrails.md)):
  `command_name`, `agent_type`, and MCP `tool_name` on hooks; `skill.name` on OTel. The exception is
  the strict mode, `CARDO_ARTIFACT_NAMES=org-only`.
- **The cost of a model switch:** `requested_model`, `context_tokens`, `prompt_cache_warm`,
  `cache_ttl` and `estimated_cache_write_usd` on `PostModelSwitch`, and on `PreModelSwitch` from
  bundles deployed before ADR-0039, each kept only when its value has the expected type
  ([ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md)).

**Attribute values are strings.** The exporter stores every attribute in a `Map(String, String)`,
so numbers and booleans arrive as text and silver casts them.

**Timestamps.** OTel rows carry the client's event time. Hook payloads carry no timestamp, so hook
rows carry the collector's receive time. That is a different clock. On the reference stack it was
1 to 3 s behind the client, and not steady
([note](../research/2026-09-24-second-real-session.md)). Hook times order hook rows among
themselves. **A duration never has one end on each clock.**

## silver — canonical event types

### Built (Admin API path)

| View | Grain |
|---|---|
| `silver_actor_day` | one row per actor per day: sessions, lines, commits, PRs, edit decisions |
| `silver_actor_day_model` | one row per actor per day per model: tokens and cost |

These are their own grain. A daily aggregate can never answer session-level questions, and it does
not pretend to be the session model below.

### Built (collector path)

`sql/clickhouse/006_silver_collector.sql`, ClickHouse only
([ADR-0028](../adr/0028-file-store-is-admin-api-only.md)). Deliberately small: this is the
vendor-neutral layer ([ADR-0004](../adr/0004-claude-code-first-neutral-schema.md)). Every view
carries `pseudonym` and `cohort`, taken from the session's OTel rows for anything a hook row
reports. Both are NULL or empty for a session that sent no OTel.

| View | Grain | Fed by |
|---|---|---|
| `silver_session` | one row per session | Every OTel log row, `claude_code.session.count` (for `start_type` only, since a metric's time is its export's) and every hook row; `SessionEnd` for `end_reason`. It starts at its first OTel event, and at its first hook row only if it sent no OTel. The `SessionStart` hook never runs in 2.1.281 |
| `silver_api_request` | one row per API request | `claude_code.api_request`. `context_tokens` = `input_tokens + cache_read_tokens + cache_creation_tokens`, the size of the conversation on a main-thread request (`query_source=repl_main_thread`). `cost_usd` is Claude Code's, in dollars |
| `silver_turn` | one row per user prompt, built-in commands included | `claude_code.user_prompt` (command, the laptop's time) and the `UserPromptSubmit` hook (`permission_mode`), joined on `prompt.id` = `prompt_id`. `first_context_tokens` and `peak_context_tokens` come from the prompt's main-thread requests |
| `silver_tool_call` | one row per tool call | `claude_code.tool_decision` and `claude_code.tool_result`, joined on `tool_use_id`. `is_edit` for Edit, MultiEdit, Write and NotebookEdit; `mcp_server` from `mcp__<server>__<tool>` |
| `silver_policy_decision` | one row per permission prompt | `claude_code.hook_execution_start` with `hook_name=PermissionRequest:<tool>`, as-of joined to the next `claude_code.tool_decision` with a person's `source` for the same session, prompt and tool. `wait_ms` is NULL for a prompt nobody answered. The hook row adds `permission_mode` and the asking subagent. A session with no `hook_execution_start` falls back to the hook row's time, with `clock = 'two_clocks'` |
| `silver_context_event` | one row per compaction or model switch | Compaction: OTel `claude_code.compaction` (`tokens_before`, `tokens_after`), or the `PreCompact` hook alone for a session that sent no OTel compaction. Switch: the `PostModelSwitch` or `PreModelSwitch` hook's fields, timed by its `hook_execution_start`, and `next_request_*`, the next main-thread request on the new model, which is what the switch cost. Both models are compared with the date and `[1m]` removed, since the switch's `to_model` may be dated. Not for a switch made before the session's first main-thread request: that request is the session starting, and the hook's `context_tokens` is 0 ([note](../research/2026-10-06-model-switch-observed.md)). Only switches someone asked for: a `PostModelSwitch` from a fallback (`auto`) or a resume is left out. Defined in `009`, which redefines `008`'s view |
| `silver_artifact_load` | one row per artifact use | `instructions`: each `InstructionsLoaded`. `skill`: `UserPromptExpansion` and `skill.name` on API requests, one row per prompt and name. `subagent`: each `SubagentStart`, never `SubagentStop`. `mcp`: tool calls on `mcp__<server>__*`, one row per prompt and server. `is_org` applies the organization's pattern; an instructions file with a stored name is always the organization's. `is_builtin` marks Claude Code's own subagents and commands |
| `silver_session_cohort` | one row per session | The cohort each session is reported under: its label, or `other` below the minimum group size that day ([ADR-0029](../adr/0029-minimum-group-size.md)) |

**`PermissionDenied` is not the end of a permission wait.** It fires only when auto mode denies a
call. The end of a wait a person answered is the `tool_decision` event
([note](../research/2026-09-23-hooks-otel-collector-surfaces.md)), on the client's clock, so the
start is taken from the client too. `hook_execution_start` is undocumented, which is why the
fallback exists and labels itself. `PermissionDenied` is not yet read by any view. It is the only
thing that says a denial was auto mode's. OTel records it as `tool_decision` `reject` with
`source=config`, the source of any permission rule or mode, and the hook joins it on `tool_use_id`
([note](../research/2026-10-06-first-permission-denied.md)).

**Compaction token counts are not context sizes.** OTel's `pre_tokens` was 6,505 against a
51,788-token context in the session observed, apparently the conversation without the fixed
prefix. Compare it with other compactions, not with `context_tokens`.

Vendor-specific richness lives in the attribute maps, not flattened away.

## gold — marts

### Built (Admin API path)

`gold_fleet_adoption`, `gold_tool_acceptance`, `gold_model_mix`, `gold_cost_daily`,
`gold_schema_drift`. All are cohort-level: CI fails any `gold_*` view that selects a pseudonym
(INV-3).

### Built (collector path)

`sql/clickhouse/007_gold_collector.sql`. Each is cohort-level, and each applies the minimum group
size ([ADR-0029](../adr/0029-minimum-group-size.md)).

| Mart | Grain | Answers |
|---|---|---|
| `gold_artifact_usage` | week, kind, artifact | Is what the organization shipped being used, by what share of the fleet? What have engineers built that several of them use? |
| `gold_instructions_versions` | week, file family, version | Which version of each of the organization's instructions files people load, and how many are on a stale one |
| `gold_friction_daily` | day, cohort | The friction index's three components: edit rejection rate, permission waits and compactions per session, each beside its volume ([ADR-0008](../adr/0008-outcome-variable.md)) |
| `gold_context_daily` | day, cohort | The context each session starts from, the context each prompt grows to, spend, and model switches' measured cost beside Claude Code's estimate |

A cost-and-capacity mart with the model mix and effort distribution by cohort is not built.
`gold_context_daily` has spend by cohort; the Admin API path has the model mix.

**What a gold view may name ([ADR-0029](../adr/0029-minimum-group-size.md)).** Two kinds of label
are shown only once enough distinct people are in them. Below that, their rows are counted as
`other`.

- **A home-grown artifact name** needs that many people in the week.
- **A cohort label** needs that many people with a session that day.

The minimum is five, read from `settings_effective`. An operator may raise it and never lower it.
Always shown, however few people use them:

- the organization's own names (`CARDO_ORG_ARTIFACTS`);
- Claude Code's built-in subagents;
- fleet-wide totals.

A session with no pseudonym counts as nobody, so it cannot lift a label over the threshold. No
view counts home-grown artifacts per person (INV-3).

**Instructions files are identified by versioned name, not by a path or a hash.** Paths are never
stored, and Claude Code 2.1.281 sends no content hash
([ADR-0026](../adr/0026-stale-instructions-by-versioned-name.md)). The organization names what it
ships with a version (`acme-security-v3.md`). "Stale" means a version below the highest one anyone
loaded while the data was retained. Files that are not the organization's are counted by kind and
never named.

**People cannot be added up.** `people` and `active_people` are distinct counts for their row's
week or day. A dashboard showing a longer range shows the busiest week or day, never a sum.
Sessions, uses and prompts can be summed.

**Every rate is published beside its volume.** Without one it rewards timidity: a session with no
friction may be a session that accomplished nothing. This is a product constraint, not a styling
preference ([ADR-0008](../adr/0008-outcome-variable.md)). Each event is reported on the day its
session started.

## Retention

Enforced by ClickHouse TTL on every bronze table ([ADR-0016](../adr/0016-retention.md)). Only tier 1
exists today:

| Tier | TTL | Status |
|---|---|---|
| T0 | 1 year | no tier-0 data is collected yet |
| T1 | 90 days | `bronze_actor_day` and all three collector tables |
| T2 | 30 days | no tier-2 data is collected yet |
| Gold marts carrying no pseudonym | indefinite | **not yet true**: the gold marts, on both paths, are views, so they expire with bronze ([risks.md](../../risks.md) #10) |
| `cardo.settings` | none | configuration, not telemetry: the organization's pattern and the minimum group size |

## Future join point

`tool_call` and `session` would carry a commit SHA where one is observable, so delivery outcomes
(cycle time, rework, revert rate) can be joined later without a schema change. **That integration is
not built** ([ADR-0008](../adr/0008-outcome-variable.md)).
