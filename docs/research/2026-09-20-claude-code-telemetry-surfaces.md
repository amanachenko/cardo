# Claude Code telemetry surfaces — snapshot 2026-09-20

> **This is a dated snapshot and is never edited.** Claude Code's telemetry surface drifts; several
> attributes below are already version-gated. If this is stale, write a new dated note — do not amend
> this one. See [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** official Claude Code docs (code.claude.com) and Claude platform docs
> (platform.claude.com), fetched 2026-09-20. Documentation version: Claude Code v2.1.269+;
> API version 2023-06-01.

## Summary

Four independent surfaces. The initial project assumption — that telemetry can only be gathered locally
via an installed collector — is **half right**: the deepest signal is local, but a meaningful slice is
available server-side with nothing deployed.

| Surface | Requires install? | Granularity | Notes |
|---|---|---|---|
| Native OpenTelemetry | No (config only) | Per-event | Metrics, logs, beta traces |
| Hooks | No (config only, via HTTP hooks) | Per-event | ~30 events, several can block |
| Session transcripts on disk | Reading local files | Complete | Undocumented, unstable, contains source code |
| Admin Analytics API | No | Daily per user | Claude API direct only |

---

## 1. Native OpenTelemetry

**Enable:** `CLAUDE_CODE_ENABLE_TELEMETRY=1`. Default on for Claude API; **off** for
Bedrock/Vertex/Foundry.

**Exporters:** `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER`, `OTEL_TRACES_EXPORTER` accepting
`otlp` | `prometheus` | `console` | `none`. OTLP over gRPC, HTTP/JSON or HTTP/Protobuf. Auth via
`OTEL_EXPORTER_OTLP_HEADERS`; mTLS supported. Works behind proxies and LLM gateways.

**Export intervals:** metrics 60 s, logs 5 s, traces 5 s — all tunable.

### Metrics

| Metric | Unit | Notable attributes |
|---|---|---|
| `claude_code.session.count` | sessions | `start_type` |
| `claude_code.lines_of_code.count` | lines | `type` (added/removed), `model` |
| `claude_code.pull_request.count` | PRs | — |
| `claude_code.commit.count` | commits | — |
| `claude_code.cost.usage` | USD | `model`, `query_source`, `speed`, `effort`, `agent.name`, **`skill.name`**, `plugin.name`, `mcp_server.name`, `mcp_tool.name` |
| `claude_code.token.usage` | tokens | `type` (input/output/cacheRead/cacheCreation) plus all of the above |
| `claude_code.code_edit_tool.decision` | decisions | `tool_name`, **`decision` (accept/reject)**, `source`, `language` |
| `claude_code.active_time.total` | seconds | `type` (user/cli) |

**`skill.name` on cost and token metrics is why skill usage needs no hook.**

### Identity attributes

| Attribute | Controllable? |
|---|---|
| `session.id` | `OTEL_METRICS_INCLUDE_SESSION_ID` (default **true**) |
| `user.account_uuid`, `user.account_id` | `OTEL_METRICS_INCLUDE_ACCOUNT_UUID` (default **true**) |
| `user.id` (anon install ID) | always |
| **`user.email`** | **always, when available — no control exists** |
| **`organization.id`** | **always, when authenticated — no control exists** |
| `vcs.repository.url.full`, `vcs.owner.name`, `vcs.repository.name` | `OTEL_METRICS_INCLUDE_REPOSITORY` (default false, v2.1.269+) |
| `terminal.type` | always when detected |

**This is the constraint behind [ADR-0006](../adr/0006-pseudonymization.md):** email cannot be
suppressed at source, so pseudonymization must happen in transit.

### Log events

| Event | Key attributes | Redaction gate |
|---|---|---|
| `claude_code.user_prompt` | `prompt_length`, `prompt`, `command_name`, `command_source` | `OTEL_LOG_USER_PROMPTS` |
| `claude_code.assistant_response` | `response_length`, `response`, `model`, `request_id` | `OTEL_LOG_ASSISTANT_RESPONSES` |
| `claude_code.tool_result` | `tool_name`, `success`, `duration_ms`, `error_type`, `decision_source`, `mcp_server_scope`, payload sizes | `OTEL_LOG_TOOL_DETAILS` |
| `claude_code.tool_decision` | `tool_name`, `decision_type`, `decision_source` | `OTEL_LOG_TOOL_DETAILS` |
| `claude_code.api_request` | `request_id`, `model`, `status_code`, `duration_ms`, token counts | `OTEL_LOG_RAW_API_BODIES` |
| `claude_code.api_error` | as above plus error detail | `OTEL_LOG_RAW_API_BODIES` |

All events carry `prompt.id` (links every event from one user prompt), `event.sequence`,
`event.timestamp`, `message.uuid` (v2.1.214+), `client_request_id` (v2.1.214+), `workspace.host_paths`.

### Redaction defaults — all favourable

| Flag | Default |
|---|---|
| `OTEL_LOG_USER_PROMPTS` | **0 — redacted** |
| `OTEL_LOG_ASSISTANT_RESPONSES` | **0 — redacted** |
| `OTEL_LOG_TOOL_DETAILS` | **0 — redacted** |
| `OTEL_LOG_TOOL_CONTENT` | **0 — omitted** |
| `OTEL_LOG_RAW_API_BODIES` | **0 — redacted** |

Content size cap `CLAUDE_CODE_OTEL_CONTENT_MAX_LENGTH` defaults to 60 KB.

### Traces (beta)

`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`. Span hierarchy: `claude_code.interaction` (root, per turn) →
`claude_code.llm_request` (includes `ttft_ms`, `agent_id`, `parent_agent_id`) and `claude_code.tool` →
**`claude_code.tool.blocked_on_user`** (permission-prompt wait time, with `decision`) and
`claude_code.tool.execution`.

`tool.blocked_on_user` is the cleanest permission-wall-time signal, but it is beta-gated — see
[ADR-0008](../adr/0008-outcome-variable.md) for why v0.1 derives it from hook timestamps instead.

---

## 2. Hooks

~30 events. Common payload present on every hook:

```json
{
  "session_id": "...", "prompt_id": "...", "transcript_path": "...", "cwd": "...",
  "scratchpad_dir": "...",
  "permission_mode": "default|plan|auto|dontAsk|acceptEdits|bypassPermissions",
  "hook_event_name": "...", "effort": {"level": "low|medium|high|xhigh|max"},
  "agent_id": "...", "agent_type": "..."
}
```

**`permission_mode` includes `plan`, and it is on every hook payload** — so plan-mode usage and effort
level are observable per event.

Events relevant to Cardo: `SessionStart`, `SessionEnd`, `UserPromptSubmit` (carries `prompt_text`),
`UserPromptExpansion` (`command_name`, `expanded_prompt`), `PermissionRequest`, `PermissionDenied`,
`PreCompact` (`current_context_tokens`, `target_tokens`), `PostCompact` (`tokens_before`,
`tokens_after`), **`InstructionsLoaded` (`file_path`, `content_hash`)**, `SubagentStart`,
`SubagentStop`, `PreModelSwitch` / `PostModelSwitch`, `ConfigChange`, `CwdChanged`, `FileChanged`,
`PostToolUse`, `PostToolUseFailure`, `Stop`, `StopFailure`, `Elicitation`, `TaskCreated`,
`TaskCompleted`, `WorktreeCreate` / `WorktreeRemove`.

**Blocking capability:** `PreToolUse`, `UserPromptSubmit`, `UserPromptExpansion`, `PermissionRequest`,
`PreModelSwitch` and `PreCompact` can deny (exit 2, or a `permissionDecision` payload). The rest are
observe-only.

**Handler types:** `command`, **`http`** (with `url`, `headers`, `allowedEnvVars`), `mcp_tool`,
`prompt`, `agent`. HTTP handlers support `async: true`.

**The `http` handler type plus `async: true` is what makes zero-local-binary collection possible**
([ADR-0011](../adr/0011-hook-transport-http.md)).

---

## 3. Managed settings

| Platform | Path |
|---|---|
| macOS | `/Library/Application Support/ClaudeCode/managed-settings.json` |
| Linux / WSL | `/etc/claude-code/managed-settings.json` |
| Windows | `C:\Program Files\ClaudeCode\managed-settings.json` (also `HKLM\SOFTWARE\Policies\ClaudeCode`) |

Precedence: **managed > command line > project local > project shared > user > defaults.**
Server-managed settings are fetched at startup and polled hourly (Claude API direct only).

Admin-lockable keys relevant here: `env` (forces any environment variable, including
`CLAUDE_CODE_ENABLE_TELEMETRY` and the OTLP endpoint and headers), `allowManagedHooksOnly`,
`allowedHttpHookUrls`, `httpHookAllowedEnvVars`, `disableAllHooks`, `allowManagedPermissionRulesOnly`,
`allowManagedMcpServersOnly`, `deniedMcpServers`, `permissions.allow` / `.deny`,
`disableBypassPermissionsMode`, `availableModels`, `maxEffortLevel`, `forceLoginOrgUUID`,
`forceLoginMethod`, `requiredMinimumVersion` / `requiredMaximumVersion`, `disableSideloadFlags`,
`strictPluginOnlyCustomization`.

**The prevention controls (`forceLoginOrgUUID`, `allowManagedMcpServersOnly`, `deniedMcpServers`,
`permissions.deny`, `disableBypassPermissionsMode`) are why most of the security wish-list is better
prevented than detected** — [ADR-0003](../adr/0003-efficiency-before-security.md).

---

## 4. Session transcripts on disk

JSONL, one record per line. Record types include user and assistant messages, `tool_use`,
`tool_result`, `metadata` (session id, model, effort, cwd, git branch), `compact`, `checkpoint`.

Retention governed by `cleanupPeriodDays`, **default 30**.

**The documentation states the format is internal and subject to change**, and recommends `/export` or
`/resume` for programmatic access rather than parsing the files.

Contains source code and tool output. **Cardo never reads these** — INV-1,
[ADR-0005](../adr/0005-collection-mechanism.md).

---

## 5. Admin APIs

**Claude Code Analytics API** — `GET /v1/organizations/usage_report/claude_code`. Admin API key, or
OAuth with `org:admin`. **Single day per request** (YYYY-MM-DD), cursor pagination, up to ~1 hour data
delay.

Returns, per actor per day: `actor` (`email_address` or `api_key_name`), `organization_id`,
`customer_type`, `terminal_type`, `num_sessions`, `lines_of_code.added` / `.removed`,
`commits_by_claude_code`, `pull_requests_by_claude_code`, accept and reject counts for `edit_tool`,
`multi_edit_tool`, `write_tool`, `notebook_edit_tool`, and a per-model breakdown of
`tokens.input` / `.output` / `.cache_read` / `.cache_creation` plus `estimated_cost`.

Does **not** cover per-turn detail, skill invocations, slash commands, permission decisions or plan
mode.

**Usage & Cost API** — `/v1/organizations/usage_report/messages` and `/v1/organizations/cost_report`.
Daily, hourly or minute buckets; grouped by model, api_key, workspace, service_tier, context_window,
inference_geo. No Claude Code-specific dimensions.

### Provider differences — the basis for [ADR-0018](../adr/0018-provider-scope.md)

| Capability | Claude API | Bedrock | Vertex | Foundry | Claude Platform on AWS |
|---|---|---|---|---|---|
| Claude Code Analytics API | yes | no | no | no | no |
| Usage & Cost API | yes | no | no | no | no |
| Server-managed settings | yes | no | no | no | no |
| OTel export | yes (default on) | yes (opt-in) | yes (opt-in) | yes (opt-in) | yes (opt-in) |

---

## 6. Signal availability matrix

| Signal | OTel | Hooks | Transcript | Admin API |
|---|---|---|---|---|
| Session lifecycle | yes | yes | yes | yes (daily) |
| Cost and tokens | yes | — | yes | yes (daily) |
| Edit accept / reject | yes | yes | yes | yes (daily) |
| Skill invoked | **yes** (`skill.name`) | — | partial | no |
| Subagent spawn | yes | yes | yes | no |
| MCP tool executed | yes (with `OTEL_LOG_TOOL_DETAILS`) | — | yes | no |
| **Permission prompt decisions** | no | **yes** | no | no |
| **Context compaction + token counts** | no | **yes** | partial | no |
| **Plan mode** | no | **yes** (`permission_mode`) | no | no |
| **Slash command used** | no | **yes** (`UserPromptExpansion`) | partial | no |
| **Instructions / rules loaded + content hash** | no | **yes** (`InstructionsLoaded`) | no | no |
| Effort level | yes | yes | partial | no |
| MCP server *inventory* (configured but unused) | no | partial (`ConfigChange`) | no | no |
| MCP server lifecycle (connect / disconnect / error) | no | no | no | no |

**The hooks-only rows are the product's differentiation** — no competitor surfaced in the prior-art
survey collects any of them.

---

## Flagged uncertainties

1. **Windows transcript path.** The source material gave `%APPDATA%\Claude\projects\...`. **Directly
   contradicted by observation on this machine**, where the path is
   `C:\Users\<user>\.claude\projects\<slug>\`. Treat the `%APPDATA%` claim as unverified. It does not
   affect Cardo (INV-1 forbids reading these), but a future session should not trust the original claim.
2. **Plan mode availability.** The source material's summary matrix asserted plan mode is not observable
   on any surface, while its own hook documentation lists `permission_mode` with a `plan` value on every
   hook payload. Resolved here in favour of the payload evidence: **plan mode is observable via hooks.**
   Verify empirically during Phase 2 before building a dashboard on it.
3. **`webhookevent` receiver and OTTL `SHA256` / `env` availability** in the collector-contrib version
   pinned for deployment was not verified against a running collector. Verify before relying on
   [ADR-0012](../adr/0012-ingest-implementation.md)'s pure-config claim.
4. **Hook payload field names** were taken from documentation, not from observed payloads. The Phase 2
   fixture corpus ([ADR-0014](../adr/0014-version-drift.md)) becomes the authority once it exists.

## Source URLs

- https://code.claude.com/docs/en/monitoring-usage
- https://code.claude.com/docs/en/hooks
- https://code.claude.com/docs/en/sessions
- https://code.claude.com/docs/en/settings
- https://code.claude.com/docs/en/managed-settings
- https://code.claude.com/docs/en/data-usage
- https://platform.claude.com/docs/en/manage-claude/claude-code-analytics-api
- https://platform.claude.com/docs/en/manage-claude/usage-cost-api
