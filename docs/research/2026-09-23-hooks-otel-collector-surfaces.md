# Hooks, OTel and the collector — snapshot 2026-09-23

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code documentation (code.claude.com `hooks`, `monitoring-usage`), fetched
> 2026-09-23, with Claude Code 2.1.281 installed. OpenTelemetry Collector contrib **v0.161.0**
> (released 2026-09-16), from its component READMEs and the ClickHouse exporter's DDL templates at
> that tag. **Measured** below means observed against a running `otel/opentelemetry-collector-contrib:0.161.0`
> writing to ClickHouse 26.6 on Docker Desktop, with synthetic payloads.

## What this note can and cannot claim

**No real Claude Code payload has been observed.** The plan was to capture one by running a
headless `claude -p` session with the hook pack pointed at a throwaway collector. Launching a nested
Claude Code session from inside an agent session was blocked by the agent's permission classifier.
That is a decision for the operator to make, and it has not been made. Everything below about
Claude Code's payloads is therefore **documentation**. Everything about the collector is
**measurement**. The two are marked.

## 1. Hook payloads have drifted in three days — documentation

Against the [2026-09-20 snapshot](2026-09-20-claude-code-telemetry-surfaces.md):

| Event | 2026-09-20 note | Documented 2026-09-23 |
|---|---|---|
| `SessionStart` | `source` | `session_start_reason` (`startup`, `resume`, `clear`, `compact`, `fork`), plus `model` (v2.1.196+) |
| `SessionEnd` | — | `session_end_reason` (`clear`, `resume`, `logout`, `prompt_input_exit`, `other`) |
| `UserPromptSubmit` | `prompt_text` | `user_input` |
| `PreCompact` | `current_context_tokens`, `target_tokens` | `compaction_reason` only |
| `PostCompact` | `tokens_before`, `tokens_after` | `compaction_reason` only |
| `InstructionsLoaded` | `file_path`, `content_hash` | `load_reason`, `file_path`, `content_hash` (`"sha256:<hex>"`) |
| `PermissionDenied` | "end of permission-wall timing" | fires **only when auto mode denies a tool call** |
| `ConfigChange` | — | `config_source` (`user_settings`, `project_settings`, `local_settings`, `policy_settings`, `skills`) |
| `PreModelSwitch` | — | `from_model`, `to_model`; a timed-out hook **blocks the switch** |

Common fields: `session_id`, `prompt_id` (v2.1.196+, absent before the first prompt; matches OTel
`prompt.id`), `transcript_path`, `cwd`, `scratchpad_dir` (v2.1.257+), `permission_mode`, `effort`
(only in tool-use contexts), `hook_event_name`. `agent_id` and `agent_type` appear only inside
subagents. **No hook payload carries a timestamp.**

The WebFetch summaries this is drawn from may omit fields. In particular, the compaction token
counts may still exist and simply not have been summarized. Cardo's allowlist keeps both the old
and the new names, since all of them are content-free, and `cardo.received_keys` will settle it on
the first real payload.

### Consequences for the design

- **Permission-wall time cannot end at `PermissionDenied`.** In default mode, the end of a
  permission wait is the OTel `claude_code.tool_decision` event (accept or reject, with
  `decision_source`), joined to the `PermissionRequest` hook on `tool_use_id`, which both carry.
  The start is the hook's receive time at the collector; the end is the client's own event
  timestamp. The two clocks differ by whatever NTP leaves between a laptop and the collector.
  That is acceptable for waits measured in seconds, and worth stating wherever the number is shown.
- `PermissionDenied` is still worth having: it is auto-mode friction specifically.

## 2. HTTP hooks — documentation

- POST, `Content-Type: application/json`, and the body is the hook's JSON input. With `async: true`
  the response is discarded and the `timeout` is not enforced.
- `headers` values support `$VAR` / `${VAR}`, but only for names listed in `allowedEnvVars`.
- **`allowedHttpHookUrls`**, when defined at *any* level, restricts HTTP hooks from *every* source
  to the merged list. Defining it where it was not defined before is a restriction, not an
  addition. This is the basis for [ADR-0024](../adr/0024-bundle-configures-telemetry-only.md).
- **`allowManagedHooksOnly`** blocks user, project, local and plugin hooks. The exception is plugins
  force-enabled in managed `enabledPlugins`. It also disables command-sourced plugins and narrows
  `statusLine` to managed settings.
- **`disableAllHooks` cannot disable managed hooks** unless it is itself set at the managed level.
- Settings-file hooks can run under `claude -p`. No table says which *events* fire headlessly.

## 3. Native OTel — documentation

- `user.email` "cannot be suppressed if the user is authenticated", on Pro and Max logins as well.
  `user.account_uuid` and `user.account_id` are always on events;
  `OTEL_METRICS_INCLUDE_ACCOUNT_UUID` controls them on metrics only.
- **New since 2026-09-20:** `OTEL_LOG_MANAGED_SETTINGS` (v2.1.274+) emits the redacted managed
  settings plus a SHA-256 digest. It is a natural future input for fleet conformance.
- `OTEL_LOG_TOOL_DETAILS` also gates the *names* of custom and plugin commands, and of
  user-configured MCP servers and tools. `agent.name` is `custom` for user-defined agents;
  `skill.name` is `third-party` for third-party skills. This is the basis for ADR-0023's name
  redaction.
- When `OTEL_EXPORTER_OTLP_ENDPOINT` is set in managed settings, Claude Code **removes the
  developer's own per-signal endpoints** at startup. Managing OTel takes over any personal OTel
  export.
- `OTEL_RESOURCE_ATTRIBUTES` is copied onto every metric data point by default
  (`OTEL_METRICS_INCLUDE_RESOURCE_ATTRIBUTES=true`). It is US-ASCII, with no spaces, commas or
  quotes.
- **Whether OTel is emitted at all on a Pro or Max account is not stated.** Unmeasured; see
  [risks.md](../../risks.md) #12.

## 4. The collector — measured unless marked

| Question | Answer |
|---|---|
| Receiver name | **`webhook_event`**. `webhookevent` still works as a deprecated alias with a warning (documentation) |
| What the receiver produces | One log record per request, raw body as a string, no timestamp (only observed time) |
| Does `ParseJSON` handle escaped Windows paths? | Yes. An earlier failure was Git Bash rewriting the test's own argument |
| Non-JSON body, with `error_mode: propagate` | Refused: **HTTP 500**, nothing stored |
| Does OTTL have `env()`? | **No.** [ADR-0012](../adr/0012-ingest-implementation.md)'s evidence line claimed it (documentation, confirmed by the function list). The salt comes in through the collector's config-time `${env:CARDO_SALT}` substitution instead |
| Do OTTL string literals process escapes? | Yes: `"\u001f"` is the 0x1F control character. Proven by pseudonym parity with the Go hasher |
| `SHA256(...)` output | Lowercase hex, identical to Go's `hex.EncodeToString(sha256.Sum256(...))` |
| `${env:VAR:-default}` with `$` in the default | **Rejected at startup** ("unsupported characters") |
| An unset `${env:VAR}` | Expands to empty, with a startup **warning** only |
| An empty regex in `IsMatch` | **Matches everything.** A blank `CARDO_ORG_ARTIFACTS` kept every custom name until the statements special-cased empty |
| `Keys(map)` into an attribute | Stored as a JSON array string |
| `String(map)` in a filter condition | Works; serializes the map, which the INV-2 tripwire relies on |
| OTLP batch refused by `error_mode: propagate` | **HTTP 503** to the sender |
| Exporter with `create_schema: false` and only the `sum` metric table | Starts and inserts. The other four metric tables are not needed while nothing sends them |
| Exporter `async_insert` default | **true** (documentation). Set false explicitly, for the reason `cardo` sends `async_insert=0` |
| Idle cost, `docker stats` | **0.02–0.08% of one core, 46 MiB** |

### The pseudonym matches across paths

For salt `0123456789abcdef0123456789abcdef` and the email `" Alice.Smith@Example.COM"`, the
collector stored `a9f33f7d5f24662c01735c2078b89e2c300bdef198934691990bec0b142580bd` on the
resource, the log record and the metric data point. That is exactly
SHA256(salt ‖ 0x1F ‖ `alice.smith@example.com`), the poller's construction. With the separator
removed from the collector's statements, the test comparing the two failed, as it should.

One asymmetry remains. OTTL's `Trim` strips spaces only, where Go's `TrimSpace` also strips tabs
and newlines. An email carrying a tab would hash differently on the two paths. Claude Code has no
plausible way to emit one.

## 5. A testing hazard found on the way — measured

**Go caches test results without knowing about the network.** The cache is keyed on the binary,
flags, environment and the files a test opened. A live test run against a *changed* collector
replayed the previous run's PASS, marked `(cached)`, and a deliberately broken config appeared to
pass. The same applies to the existing ClickHouse integration tests, including in CI, where
`setup-go` restores the build cache that holds test results, and to `make sample-seed`, whose
second run seeds nothing. Every live target now passes `-count=1`.

## What was not established

- Any real hook payload, and therefore which of the fields above actually arrive in 2.1.281.
- Which hook events fire under `claude -p`.
- Whether OTel is emitted on a Pro account.
- Whether `memory_type`, `command_source` or `expansion_type` exist. The allowlist includes them
  on the strength of older documentation and of Claude Code's OTel using `command_source`.

The next step that settles all four is one engineer running Claude Code with
`deploy/managed-settings/local-evaluation.json` against the reference stack. No raw payload is
needed: `cardo.received_keys` records every field name that arrived, and the allowlist keeps the
values that matter.
