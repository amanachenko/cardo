# ADR-0016 — Retention is the primary privacy control

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (Claude Code's own `cleanupPeriodDays`
defaults to 30); judgment for the tier values.

## Context

ADR-0006 uses a stable salt in v0.1. A salted hash with a retained salt is **pseudonymization, not
anonymization** — the data remains personal data under GDPR for as long as the salt exists.

That makes retention, not hashing, the thing that actually bounds exposure.

## Decision

| Tier | Retention |
|---|---|
| **T0 — Security audit** | 1 year |
| **T1 — Fleet core** | 90 days |
| **T2 — Personal depth** | 30 days |
| Derived aggregates (no pseudonym) | Indefinite |

Enforced by ClickHouse TTL on the bronze and silver layers. Gold marts that carry no pseudonym are
exempt and retained indefinitely, so longitudinal trends survive the expiry of the rows behind them.

## Consequences

- **The README states plainly that retention is the privacy control**, rather than leaning on the word
  "pseudonymous." This is an honesty requirement, not a marketing choice.
- Year-over-year individual comparison is impossible at T1. That is intentional and costs almost nothing
  analytically, because the aggregates survive.
- Implies a documented deletion path: because the salt exists, a deletion request can be honoured, and
  the procedure must be written down rather than improvised.

## Rejected alternatives and why

- **Shorter everywhere** (30/30/30). Cleaner privacy posture; 30 days of T1 is too short to see whether
  an enablement artifact changed behaviour after it shipped, which is the product's core question.
- **Longer for trend analysis** (2 years at T1). Better trends; keeps personal data alive far longer
  than the analysis requires, given aggregates already cover trends.
