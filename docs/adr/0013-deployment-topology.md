# ADR-0013 — Strictly single-tenant, self-hosted, no phone-home

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** Judgment, no evidence.

## Context

Cardo is meant to be adopted by many organizations, each with sensitive data about how its engineers
work. That raises a question that would not arise for an internal tool: who operates the deployment,
and who holds the data?

## Decision

Cardo runs **single-tenant, inside each organization's own infrastructure**. No phone-home, no
telemetry about the telemetry, no hosted option. The project's maintainers never hold an
organization's data.

**No cross-org benchmark feed in v1.**

## Consequences

- The trust premise — nothing leaves your network (INV-7) — stays an unqualified statement with no
  asterisk.
- No DPAs to negotiate, no breach liability across multiple orgs, no standing target.
- Onboarding is slower than a hosted option would be. Mitigated by the DuckDB evaluation mode (ADR-0009)
  and the zero-install Admin API tier (ADR-0017).

## Rejected alternatives and why

- **Hosted multi-tenant.** Faster onboarding for organizations. Makes the maintainers a data
  processor for multiple organizations' engineering-activity data — DPAs, breach liability, a
  standing target — and torches the trust story that is the entire premise of the project.
- **Both, split by organization size.** Two deployment models to maintain and two different trust
  stories to tell. The weaker story becomes the one people remember.
- **Self-hosted with an opt-in anonymized cross-org benchmark feed.** Genuinely the most valuable
  thing that could be built here, and the fastest way to turn "open source, nothing leaves your
  network" into a claim with an asterisk. Revisit only once the project has standing, as a clearly
  separate opt-in product with its own consent story.
