# ADR-0002 — Three-tier data model with structural boundaries

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (for what each tier can contain);
judgment for the tier boundaries themselves.

## Context

Efficiency analytics and security monitoring want the same raw activity but have irreconcilable
requirements: different consumers, different consent posture (opt-out-able vs. not), different retention
(90 days vs. years), different outputs (dashboards vs. alerts). Most vendors solve this by building one
identified stream and applying access control on top.

That approach fails here for a specific reason: the moment engineers learn that the agent feeding their
team's efficiency dashboard *also* feeds insider-risk detection, they stop trusting the efficiency
product — and that trust is not recoverable once lost.

## Decision

Three tiers, with the boundary enforced by infrastructure rather than by policy or org chart.

| Tier | Mandatory | Identity | Owner | Contents | Retention |
|---|---|---|---|---|---|
| **T0 — Security audit** | Yes, non-negotiable | Identified | SecOps only | Policy decisions, MCP inventory, repo/remote identity, denied tool calls, login/org identity, bypass-mode usage. No prompts, no code. | 1 year |
| **T1 — Fleet core** | Yes | Pseudonymous | Platform team | Session/model/effort/cost, tool classes, skill names | 90 days |
| **T2 — Personal depth** | Opt-in | Self-visible only | The engineer | Rich session detail, their own dashboard | 30 days |

Separate storage, separate credentials, separate deployment (ADR-0003). T0 ships as a separately
installed module so that turning on security monitoring is a deliberate, announced act rather than a
silent capability that was always present.

## Consequences

- We cannot honestly say "you can turn this off." The honest replacement is narrower and stronger:
  *here is the exact, short list of what the un-turn-off-able tier collects; it contains no prompts and
  no code; it goes to security, not to your manager; every query against it is audited.*
- Governance becomes a real deliverable, not a footnote: a documented access policy, audit logging on
  T0, and in the EU almost certainly a DPIA and a works-council conversation.
- If the platform team operates the whole stack including T0, engineers will assume the wall is soft and
  they will be right. The separation must be operational, not aspirational.

## Rejected alternatives and why

- **Single mandatory identified stream serving both use cases.** What most vendors do. Simplest to
  build and the fastest way to lose the engineering population permanently.
- **Named individual-level throughout.** Maximum analytical power; maximum rejection risk and GDPR /
  works-council exposure.
- **Aggregate-only, always** (k-anonymity on every query, no per-person rows). Safest possible posture
  and it eliminates the security use case entirely — you cannot investigate someone cloning out-of-org
  repos without knowing who.
