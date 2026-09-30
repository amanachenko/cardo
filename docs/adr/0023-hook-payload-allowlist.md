# ADR-0023 — Hook payloads are reduced to an allowlist at the collector

**Status:** Superseded by [ADR-0025](0025-artifact-names-kept-with-guardrails.md)
**Date:** 2026-09-23
**Evidence:** `2026-09-23-hooks-otel-collector-surfaces.md` (documentation and measurements against
a running otelcol-contrib 0.161.0 and ClickHouse 26.6).
**Supersedes:** [ADR-0010](0010-layered-schema.md).

## Context

[ADR-0010](0010-layered-schema.md) defined bronze as *"raw `claude_code.*` events and hook payloads,
persisted verbatim, nothing dropped."* Two other parts of the record already disagreed with it.
[ADR-0005](0005-collection-mechanism.md) put `UserPromptSubmit` in the pack as *"metadata only —
length, `permission_mode`, `effort`; never the text"*. INV-5 says tiers 0 and 1 hold no prompt text,
code, file paths or tool parameter values. Verbatim storage and "never the text" cannot both be true
of the same payload.

Building the collector made the conflict concrete. A hook payload is not telemetry. It is the input
Claude Code hands a local script so the script can act on it, and it carries content by design:

| Field | On | Carries |
|---|---|---|
| `user_input` | `UserPromptSubmit` | the prompt text |
| `tool_input` | `PermissionRequest`, `PermissionDenied` | the command line, the file being written |
| `expanded_prompt` | `UserPromptExpansion` | what a slash command expanded to |
| `last_assistant_message` | `SubagentStop` | model output |
| `file_path` | `InstructionsLoaded` | where the engineer is working |
| `cwd`, `transcript_path` | every event | the same, again |

Native OTel is the opposite case: every content field is behind an `OTEL_LOG_*` flag and redacted
unless the flag is set. Nothing in a hook payload is redacted at source.

There was a second, smaller finding. Claude Code's own OTel redacts the names of user-defined
commands, subagents and MCP servers to `custom` by default, gated behind `OTEL_LOG_TOOL_DETAILS`.
The hook payloads carry the same names unredacted. A hook pack that stored them would quietly be
more invasive than the native telemetry engineers were told about.

## Decision

**1. The ADR-0010 layering stands unchanged.** Bronze, silver and gold, a deliberately small
canonical layer, and silver and gold expressed as SQL views over bronze, all remain as decided.
Bronze on the Admin API path is still the verbatim response. What changes is what bronze means for
hook payloads, below.

**2. Hook payloads pass through an allowlist at the collector** (`transform/hooks` in
`deploy/collector/config.yaml`). Only listed fields are stored, and the raw request body is
discarded before export. The list is pinned in `test/collector_test.go`. Adding a field to it is
adding to the mandatory tier, so it needs an ADR first (INV-4).

**3. The name of every received field is recorded, never its value.** Each row carries
`cardo.received_keys`, the payload's top-level keys. A field added in a future Claude Code release
is dropped until someone decides otherwise, but it shows up as drift instead of disappearing.
That keeps [ADR-0014](0014-version-drift.md)'s intent: breakage is visible, not silent.

**4. Some facts are derived from content, and then the content is dropped:**

| Stored | Derived from |
|---|---|
| `prompt_length` | `user_input` |
| `instructions_file`: `CLAUDE.md`, `CLAUDE.local.md`, `AGENTS.md`, `rule` or `other` | `file_path` |
| `instructions_scope`: `managed` when the file is the organization's own managed instructions | `file_path` |

`InstructionsLoaded` is the artifact-analytics keystone. The artifact is identified by its
`content_hash`, which the platform team knows for everything it ships, not by where it sits on
someone's disk.

**5. User-defined command, subagent and MCP names are reduced to `custom`** unless they match
`CARDO_ORG_ARTIFACTS`, a regex the platform team sets to name its own artifacts. That mirrors
Claude Code's OTel defaults, and it still lets Cardo measure the artifacts it exists to measure: the
organization's own. Built-in subagent types are kept. An unset or empty pattern names nothing.

**6. The OTel path is a denylist plus pseudonymization, not an allowlist.** It is redacted at
source, the bundle pins every content flag off, and new attributes are generally safe by
construction. The collector hashes `user.email` with exactly the poller's construction,
SHA256(salt ‖ 0x1F ‖ lowercase(trimmed email)). It then drops account identifiers,
`host.name`, `vcs.*`, workspace paths, and every content attribute a flag could switch on.

**7. Every pipeline fails closed.** Every processor uses `error_mode: propagate`. A missing or short
salt refuses every OTel batch, with a log line naming the cause. A final filter drops any record
still carrying something shaped like an email address.

## Consequences

- INV-5 holds by construction on the hook path, rather than depending on nobody ever adding a
  content field upstream.
- A new hook field's *value* is lost until reviewed. That is the right way round for a mandatory
  tier. Its *name* arrives at once, so the review has something to start from.
- Bronze for hooks is no longer something a derived field can be backfilled from, if the field was
  never kept. ADR-0010 gave that property up for hook payloads specifically. It survives for OTel
  and the Admin API.
- MCP server, subagent and command analytics require the organization to name its artifacts. An
  organization that never sets `CARDO_ORG_ARTIFACTS` sees `custom` everywhere. That is the correct
  default, and the deployment README says so.
- The allowlist is a union across events, so every name on it must be content-free in *every*
  event that carries it. Generic names such as `reason`, `message` and `error` are excluded for
  exactly that reason, even where one event's use of them would be harmless.
- OTTL is carrying about thirty statements and is still readable. The naming redaction was the
  first part that needed care: an empty regex matches everything. [risks.md](../../risks.md) #4
  still stands.

## Rejected alternatives and why

- **Verbatim, as ADR-0010 said.** Incompatible with INV-5 on the first event in the pack, and in
  conflict with ADR-0005's own description of `UserPromptSubmit`.
- **A denylist of known content fields.** It keeps new benign fields automatically. But hooks are
  designed to carry content, so the next content-bearing field a release adds would be stored until
  someone noticed. For a mandatory tier, a leak that waits to be noticed is the wrong failure.
- **Per-event allowlists**, thirteen lists each gated on `hook_event_name`. More precise, and three
  times as much configuration for a reviewer to audit. The union works as long as generic names stay
  off it, and the pinned test enforces that.
- **Store a salted hash of `file_path`.** It would allow "distinct files" counts. But it is still a
  per-path identifier, and anyone holding the salt can dictionary-attack common paths. The derived
  kind and scope, plus the content hash, answer the product's questions without it.
- **Keep custom names verbatim.** More analytical reach, and more invasive than the native
  telemetry engineers were told about, through a side channel they are not told about.
- **Hash custom names with the salt.** It would allow counting distinct custom artifacts without
  naming them. It is plausible for v0.2. Deferred because no current question needs it, and it
  would put a second use of the salt into configuration.
