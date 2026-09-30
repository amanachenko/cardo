# ADR-0005 — Collect via native OTel plus a managed hook pack; never read transcripts

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`.

## Context

The initial assumption was that this telemetry can only be gathered locally, requiring a lightweight
collector on each machine. That is half right. Four surfaces exist:

1. **Native OpenTelemetry** — metrics carrying `skill.name`, `agent.name`, `mcp_server.name`,
   `mcp_tool.name`, `model`, `effort`, `speed`, plus `code_edit_tool.decision` (accept/reject) and
   `active_time`; structured log events for `user_prompt`, `tool_result`, `tool_decision`, `api_request`;
   and a beta trace mode. Much of the efficiency story needs **zero custom code**.
2. **Hooks** — ~30 events covering the remaining gaps, notably `InstructionsLoaded` (file path plus
   **content hash**), `PreCompact`/`PostCompact` with token counts, `PermissionRequest`/
   `PermissionDenied`, `UserPromptExpansion` (slash command name), `ConfigChange`. Every payload carries
   `permission_mode` (values include `plan`) and `effort.level`.
3. **Session transcripts on disk** — the richest source. Undocumented, explicitly unstable format;
   contains source code and tool output.
4. **The Admin Analytics API** — daily per-user aggregates with nothing deployed at all (ADR-0017).

## Decision

Native OTel plus a small managed hook pack of 13 events. **Session transcripts are never read** — this
is INV-1, and it is stated loudly in the README.

The 13 events: `SessionStart`, `SessionEnd`, `UserPromptSubmit` (metadata only — length,
`permission_mode`, `effort`; never the text), `UserPromptExpansion`, `PermissionRequest`,
`PermissionDenied`, `PreCompact`, `PostCompact`, `InstructionsLoaded`, `SubagentStart`, `SubagentStop`,
`PreModelSwitch`, `ConfigChange`.

Skill usage needs **no** hook — `skill.name` is already an attribute on the native cost and token
metrics.

## Consequences

- The hook pack is the differentiator: plan mode, permission friction, compaction behaviour, slash
  command usage and rules-loading come from here, and no competitor has any of it.
- Content redaction defaults are favourable and we keep them: `OTEL_LOG_USER_PROMPTS`,
  `OTEL_LOG_ASSISTANT_RESPONSES`, `OTEL_LOG_TOOL_DETAILS` and `OTEL_LOG_RAW_API_BODIES` all default to
  redacted and all stay at `0` in the shipped bundle. This gives a concrete trust artifact: a short,
  auditable env block, with a note on what each flag would have enabled had we set it.
- Refusing transcripts costs real signal. That cost is the point — it is what makes "it does not read
  your code" a claim rather than a promise. **It will be tempting to breach this for debugging. Do not.**

## Rejected alternatives and why

- **OTel only.** Smallest trust footprint and nothing custom on the laptop, but loses permission
  decisions, compaction, plan mode and rules-loading — i.e. most of the differentiated signal.
- **OTel + hooks + transcript tailing.** Maximum signal. Reads files containing source code in an
  undocumented, unstable format — the single thing that would let a sceptical engineer say it reads my
  code, and the marginal signal is not worth that.
- **Admin Analytics API only.** Zero install, daily aggregates, no session detail. Retained as a
  complementary tier (ADR-0017), not as the mechanism.
