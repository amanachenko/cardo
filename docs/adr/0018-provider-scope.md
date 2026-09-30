# ADR-0018 — v1 targets Claude API direct with an Enterprise licence

**Status:** Superseded by [ADR-0021](0021-analytics-source-scope.md)
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`.

## Context

Claude Code's telemetry surface differs materially by deployment provider:

| Capability | Claude API direct | Bedrock | Vertex | Foundry |
|---|---|---|---|---|
| Claude Code Analytics API | yes | no | no | no |
| Usage and Cost Admin API | yes | no (CloudWatch) | no (GCP console) | no (Azure) |
| Server-managed settings (remote push) | yes | no | no | no |
| OpenTelemetry export | yes (on by default) | yes (opt-in env var) | yes (opt-in) | yes (opt-in) |

On Bedrock/Vertex/Foundry the Phase 1 wedge (ADR-0017) does not exist at all, and managed settings must
be distributed as files via MDM rather than pushed from the server.

## Decision

v1 targets organizations running **Claude API direct with an Enterprise Claude Code licence**.
Bedrock, Vertex, Microsoft Foundry and Claude Platform on AWS are **explicitly out of scope for
v1**.

The README states plainly what those deployments would and would not get: the OTel and hook path works,
the zero-install tier does not, and settings distribution is file-based.

## Consequences

- v1 stays coherent — one deployment story, one set of capabilities, one set of docs.
- Closes `risks.md` #3, which would otherwise have forced v0.1 to begin at Phase 2.
- A regulated enterprise on Bedrock cannot adopt v1 as-is. Accepted; revisit if one asks, since the
  OTel and hook path is provider-independent and most of the work would carry over.

## Rejected alternatives and why

- **Support all providers in v1.** Doubles the deployment matrix and the documentation before a
  single organization is running anything, in order to serve one that has not asked.
- **Target Bedrock/Vertex first** on the theory that regulated enterprises are the main adopters.
  Loses the zero-install wedge, which is the cheapest path to a first real deployment.
