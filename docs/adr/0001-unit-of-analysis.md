# ADR-0001 — The unit of analysis is the enablement artifact, not the engineer

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** Judgment, no evidence. This is the founding reframe of the project and is deliberately
unvalidated — see `risks.md` #1.

## Context

The project was conceived to solve a stated problem: large orgs "find it hard to make sure all engineers
are following best practices, using recommended org-wide workflows and skills, entering plan mode,
maintaining session and context window hygiene, optimal model and effort settings."

Read literally, that makes **the engineer** the unit of analysis. That framing has three failure modes:

1. **It is surveillance.** Engineers will read it as such, correctly, and respond by disabling telemetry,
   escalating to a works council, or quietly routing around it.
2. **The metrics are gameable and therefore worthless.** "Entered plan mode" measures whether someone
   performed a ritual, not whether the work got better. Make it a KPI and you will get 100% plan-mode
   entry and zero improvement.
3. **It misdiagnoses the problem.** An org where engineers will not follow best practices usually has a
   discoverability and ergonomics problem, not a compliance problem. Ranking people does not surface that.

## Decision

Move the unit of analysis to **the platform team's own artifacts**: skills, managed `CLAUDE.md` and
`.claude/rules/*`, MCP server inventory, permission rules, model policy.

The questions Cardo answers are of this shape:

- We shipped a `deploy` skill three weeks ago — is it being invoked, in which repos, and do sessions that
  invoke it look different?
- Our managed `CLAUDE.md` is 400 lines — is it actually loading, and which content hash is each machine
  on? Who is stale?
- Which permission rules generate the most prompt-wall friction?
- Which MCP servers are configured but never called?

Per-engineer detail exists but is **self-service only** (ADR-0002, INV-3). Leadership gets cohort
aggregates derived from the same data, never a per-person view.

## Consequences

- The metrics stop being gameable. "Was the skill invoked" is a fact about the skill's discoverability,
  not a score anyone can farm.
- The party under scrutiny becomes the platform team, which is the party that can actually act.
- Every dashboard design decision inherits this: if a view would let a manager rank their reports, it is
  the wrong view regardless of how useful it looks.
- **This is the decision most likely to be challenged by a stakeholder** ("but I want to see who is
  not using plan mode"). The answer is cohort aggregates, and holding that line is the whole
  project.

## Rejected alternatives and why

- **Individual coaching only** (per-engineer feedback, never reported upward). Trustworthy, but answers
  nothing at fleet level — a platform team learns nothing about its own artifacts. Retained as a
  *component* (tier 2) rather than the whole product.
- **Compliance / adoption reporting** (per-team and per-person conformance dashboards for leadership).
  The original framing. Highest gaming risk and highest rejection risk; see failure modes above.
- **Cost and capacity only.** Least controversial and narrowest. Closest to already-solved by
  `ccusage` and every vendor connector. Retained as a near-free byproduct, not the point.
