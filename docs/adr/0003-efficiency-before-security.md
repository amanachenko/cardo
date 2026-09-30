# ADR-0003 — Efficiency first; security is a separate, later, enforcement-first module

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (native prevention controls);
`2026-09-21-prior-art-survey.md` (usage and security are disconnected pipelines across the market).

## Context

Two distinct needs motivated the project: AI-assisted coding efficiency, and security threat detection
(out-of-org repo work, tool calls outside normal range, unapproved third-party integrations). The
security half was explicitly the less developed of the two.

Two facts reshape it:

1. **Detection needs a baseline of normal**, and the only way to get one is to run the efficiency
   corpus for weeks first.
2. **Most of the security wish-list is better prevented than detected.** Claude Code already supports
   `forceLoginOrgUUID` (blocks out-of-org sign-in), `allowManagedMcpServersOnly` and `deniedMcpServers`
   (blocks unapproved integrations), `permissions.deny` plus blocking `PreToolUse` hooks (blocks
   out-of-range tool calls), and `disableBypassPermissionsMode`. Detecting any of these in a dashboard
   hours later is strictly worse than refusing them at the edge.

## Decision

Ship the efficiency product first, with T1 and T2 only. T0 arrives later as a **separately installed
module**, and its v1 is **enforcement evidence, not behavioural detection**:

- managed-settings fleet conformance — who is on the current policy version, who has drifted
- MCP server inventory versus the approved list
- blocked-call audit — what was refused, by which rule, and why

Behavioural detection is added only once months of baseline exist.

The v0.1 schema still emits the events T0 would need (MCP server identity, repo/remote identity, tool
call classes) so that adding the module later is additive rather than a rewrite.

## Consequences

- The security offer changes from "detection platform" to "policy-as-code plus continuous evidence
  that the policy is in force." That is CISO-legible, far easier to build correctly, and requires no
  session content at all.
- This is the decision most likely to be renegotiated by an organization that wants the security
  story first. The counter-argument is that enforcement evidence *is* the security story, delivered
  sooner.
- Prior art confirms the gap: nobody currently feeds coding-agent session telemetry into a threat
  detection layer. Usage observability (Datadog, Grafana, Honeycomb) and agent security (Zenity, Wiz,
  Prisma AIRS) are two disconnected pipelines over the same activity.

## Rejected alternatives and why

- **Security first, efficiency as byproduct.** Different audience, mandatory-consent posture from
  day one, and a SIEM-integration surface. Would poison the efficiency product before it had users.
- **Both as co-equal pillars in v1.** Different audiences, retention, and output surfaces — high
  risk of doing neither credibly.
- **Drop security entirely.** Cleanest trust story; gives up a real need that organizations
  articulated and a genuine market gap.
