# ADR-0025 — Artifact names are kept; the views decide who sees them

**Status:** Accepted
**Date:** 2026-09-24
**Evidence:** `2026-09-24-first-real-hook-payloads.md` (measured, Claude Code 2.1.281) for what
Claude Code sends. The choice to keep names is judgment: the operator's, on the argument below.
**Supersedes:** [ADR-0023](0023-hook-payload-allowlist.md).

## Context

[ADR-0023](0023-hook-payload-allowlist.md) put hook payloads through an allowlist at the collector.
Its decision 5 reduced the names of user-defined slash commands, subagents and MCP servers to
`custom` unless they matched `CARDO_ORG_ARTIFACTS`. The stated reason was that this "mirrors Claude
Code's OTel defaults".

The first real session showed that premise was half true. With every `OTEL_LOG_*` flag off, Claude
Code still puts the real name of a custom slash command on `skill.name`: on API request events, and
on the cost and token metrics. Its own `skill_activated` event says `custom_skill` for the same
invocation. So Cardo was already storing engineers' private command names through the OTel path,
while its documentation said it did not.

That forced the question ADR-0023 had answered by analogy: should Cardo keep these names? The
operator's answer is yes. What engineers build for themselves is the best evidence a platform team
can get of what it should ship. If twelve people have each written a `/pr-review`, that is a missing
organization artifact. That is [ADR-0001](0001-unit-of-analysis.md)'s unit of analysis, seen from
the bottom up.

The case against was also real, and the guardrails below answer it:

- **It contradicts what the bundle shows engineers.** The bundle pins `OTEL_LOG_TOOL_DETAILS=0`, and
  that flag is documented as controlling these names.
- **A name used by one person identifies them, and is sometimes content.** Examples include a
  command named for a colleague's review, or an MCP server called `personal-gmail`.
- **A per-person count of home-grown commands is a leaderboard waiting to happen** (INV-3).

## Decision

**1. ADR-0023 stands, except its decision 5 and its claim about content hashes.** These are
unchanged:

- The hook allowlist, pinned in `test/collector_test.go`.
- `cardo.received_keys`.
- The facts derived from content before it is dropped.
- The OTel path: a denylist plus pseudonymization.
- Every processor failing closed.

The content-hash claim is replaced by [ADR-0026](0026-stale-instructions-by-versioned-name.md).

**2. Artifact names are stored as sent, on both paths.** On the hook path that is `command_name`,
`agent_type` and MCP `tool_name`; on the OTel path it is `skill.name`.

**3. Guardrails.**

- **Say so.** The bundle README and `docs/design/privacy.md` state plainly that Cardo records these
  names, and that this is more than Claude Code's own telemetry sends with its flags off.
- **A name appears in a view only once enough people use it.** The threshold is a minimum number of
  distinct pseudonyms in the period shown, the same minimum group size that INV-3 cohort views use.
  Below it, the artifact is counted under "other". The number is set when the views are built;
  five is the working assumption. Convergence is the signal; a name only one person uses is noise
  for the platform team and a quasi-identifier for everyone else.
- **No view counts home-grown artifacts per person.** INV-3 already forbids it. It is restated here
  because this data invites it.
- **`CARDO_ORG_ARTIFACTS` becomes a label.** Views use it to separate what the organization shipped
  from what engineers built. In the default mode it no longer decides what is stored.
- **Strict mode stays available as configuration.** `CARDO_ARTIFACT_NAMES=org-only` restores
  ADR-0023's rule, and now applies it to OTel `skill.name` as well, which ADR-0023 missed. Any value
  other than `all` is strict, so a typo fails closed.
- **Instructions-file names are not covered by this.** A file name is part of a path (INV-5), and
  only the organization's own are kept, in both modes
  ([ADR-0026](0026-stale-instructions-by-versioned-name.md)).

**4. Generic hook field names never go on the allowlist.** This covers `reason`, `trigger`,
`source`, `prompt`, `message` and `error`. ADR-0023 excluded them as a consequence. Real payloads
turned out to use them for exactly the facts Cardo keeps, so the rule is now explicit:

- Each one is mapped onto a specific stored name, for one named event, and only when its value is
  enum-shaped. For example, `SessionEnd`'s `reason` becomes `session_end_reason`.
- `TestCollectorScopesGenericHookFields` enforces the event scoping.

## Consequences

- The artifact dashboard can show demand from what engineers build for themselves, not just usage
  of what was shipped.
- Tier 1 now holds names that engineers chose. They are kept for 90 days, linked to a pseudonym
  through the session. Retention remains the privacy control
  ([ADR-0016](0016-retention.md)).
- The view threshold lives in views that do not exist yet. Until they do, names sit only in
  bronze, readable by whoever can query ClickHouse, as everything in bronze is.
- Strict mode has two known gaps. Neither was observed carrying a private name, and neither is
  redacted in either mode:
  - OTel `plugin.name` on plugin-load events.
  - `hook_name` on hook-execution events, which is `Event:matcher`.
- An organization whose works council requires strict mode gets it as configuration, not as a fork.
  The CI `collector` job runs the real collector in both modes.

## Rejected alternatives and why

- **Keep ADR-0023's rule, and redact OTel `skill.name` to match.** Consistent and private. It
  throws away the demand signal, which is the most useful thing these names carry for this
  product's unit of analysis.
- **Keep names, with no guardrails.** Simplest. It puts names that only one person uses on
  dashboards, where they are quasi-identifiers and occasionally content.
- **A salted hash of each home-grown name**, deferred in ADR-0023. Views could show that twelve
  people use the same private command, but the platform team cannot act on "command 3f2a" without
  asking what it is. It would also put a second use of the salt into configuration.
- **Split by scope: project commands in clear, user commands redacted.** Principled on the hook
  path, where `command_source` says which is which. OTel's `skill.name` carries no scope, so the
  two paths would disagree about the same command.
- **Strict mode as the default.** Safer in the abstract. The operator decided that the default
  deployment should answer the demand question, with strictness an explicit choice. That is
  judgment, and is recorded as such.
