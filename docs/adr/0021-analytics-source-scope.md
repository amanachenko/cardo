# ADR-0021 — Support both analytics sources; build the Console adapter first

**Status:** Accepted
**Date:** 2026-09-23
**Evidence:** `2026-09-23-admin-analytics-apis.md`.
**Supersedes:** [ADR-0018](0018-provider-scope.md).

## Context

[ADR-0018](0018-provider-scope.md) scoped v1 to *"Claude API direct with an Enterprise Claude Code
licence."* That phrase reads as one configuration. It names two, and they are mutually exclusive.

The [2026-09-23 snapshot](../research/2026-09-23-admin-analytics-apis.md) establishes that Anthropic
ships **two** analytics APIs:

| | Claude Code Analytics API | Claude Enterprise Analytics API |
|---|---|---|
| Applies to | Claude Console / Platform orgs | Claude Enterprise (claude.ai) orgs |
| Key | Admin API key, org **admin** | Analytics API key, **primary owner** |
| Endpoint | `/v1/organizations/usage_report/claude_code` | `/v1/organizations/analytics/...` |

> "The key types are not interchangeable: an Admin API key cannot call the Claude Enterprise
> Analytics API, and an Analytics API key cannot call the Admin API."

Enterprise organizations calling the Admin API proper reach **only the members and invites
endpoints** — `usage_report` is not among them.

ADR-0018 was right about the **provider** axis (Claude API direct vs Bedrock / Vertex / Foundry) and
wrong about the **product** axis, which it did not know existed. The practical consequence: a pilot
organization whose engineers hold claude.ai Enterprise seats returns **nothing** from the endpoint
Phase 1 was designed against.

This matters more than a normal scoping error because the stated target market — medium-to-large
engineering organizations — is disproportionately likely to be exactly that configuration.

## Decision

**1. The provider exclusion from ADR-0018 stands unchanged.** Bedrock, Vertex, Microsoft Foundry and
Claude Platform on AWS remain explicitly out of scope for v1. Neither analytics API covers Bedrock
usage, so this is unaffected by the correction above.

**2. v1 supports both analytics sources** — Claude Console organizations and Claude Enterprise
organizations — as two adapters behind one internal interface.

**3. Phase 1 implements the Console / Admin API adapter first.** This is a **testability** decision,
not a market judgment. The Console adapter can be exercised end to end today against an organization
we create ourselves, without depending on anyone else and with no operator-gated credential. The
Enterprise adapter cannot: every test of it requires an organization's primary owner to mint a key.

**4. Bronze carries a `source` discriminator and stores each response payload verbatim**, so adding
the second adapter is additive rather than a schema migration. This extends
[ADR-0010](0010-layered-schema.md)'s raw-first rule rather than modifying it.

Differences between the two sources are **adapter concerns**, absorbed before the canonical layer:
history floor of 2026-01-01, ~1-day lag, decimal-string cents, 60 rpm org-wide limits, and cursors
bound to their issuing query all belong to the Enterprise adapter and must not leak into silver.

## Consequences

- **Phase 1 becomes testable immediately.** An individual can create a Console organization and
  provision an admin key without involving anyone else, which removes the last blocking dependency
  from the wedge.
- **Two response shapes to parse, one canonical model.** The silver views absorb the difference; this
  is precisely the job ADR-0010 gave them, now with a real second source rather than a hypothetical.
- **The Enterprise adapter is specified but not evidenced.** Its response shape was never retrieved —
  only a prose description of its contents. Its field names are very likely *not* symmetric with
  `core_metrics` / `tool_actions`. See `risks.md` #8.
- **"Console-first" is a sequencing choice and must not be read as a market bet.** A future session
  finding only a Console adapter should read this paragraph before concluding Enterprise was
  dismissed.
- Cost arithmetic now has two unit conventions to get wrong — integer cents on one source, decimal
  strings in cents on the other. Both are cents; neither is dollars.

## Rejected alternatives and why

- **Console only in v1**, as ADR-0018 effectively implied. Simplest, one adapter, and excludes a
  large share of the stated target market — possibly the majority of it. The failure mode is silent:
  the poller runs, authenticates, returns zero rows, and looks like a working install with no usage.
- **Enterprise only, or Enterprise first**, on the grounds that it is where the largest
  organizations are. Correct about the market and wrong about sequencing: it makes every single test
  depend on an organization's primary owner minting a `read:analytics` key, which is the slowest
  possible feedback loop for the phase whose entire purpose is to validate the analysis layer
  cheaply.
- **One client that branches internally** on which key it was handed. Hides two genuinely different
  APIs behind false symmetry. The differences — a hard history floor, a different freshness model, a
  different cost encoding, query-bound cursors — are not cosmetic, and a single class would grow
  conditionals at every one of them.
- **Defer the decision until a pilot organization's configuration is known.** Blocks all Phase 1
  work on a fact that may take weeks to obtain, when the recommended sequencing is correct under
  every possible answer.
