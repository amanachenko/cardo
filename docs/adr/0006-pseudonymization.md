# ADR-0006 — Pseudonymize at the org-edge collector; stable salt in v0.1

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`.

## Context

`user.email` and `organization.id` are emitted on every Claude Code OTel metric and **cannot be
suppressed by configuration**. `OTEL_METRICS_INCLUDE_SESSION_ID` and
`OTEL_METRICS_INCLUDE_ACCOUNT_UUID` have toggles; email does not.

Pseudonymity is therefore impossible at the source. It has to happen in transit, and where it happens
is an architectural decision that determines what we can honestly claim.

## Decision

Hash at the **org-edge OTel Collector**, using a salted SHA256, with the salt held by the
organization's security team rather than by the analytics team:

```
set(attributes["user.pseudonym"], SHA256(Concat([attributes["user.email"], env("CARDO_SALT")], "")))
delete_key(attributes, "user.email")
```

**v0.1 uses a stable salt for all tiers.** Rotation is declared as future intent, not built.

**Store `salt_version` alongside every pseudonym from day one**, even though it will always be `1`.

## Consequences

- The privacy claim is auditable in about twenty lines of standard collector config rather than
  requiring trust in a binary we wrote.
- Break-glass re-identification requires going to whoever holds the salt. That is a real access-control
  boundary and it produces an audit trail by construction.
- **Salted hashing is not anonymization.** While the salt exists, the data is personal data under GDPR —
  DSAR-able, deletion-requestable, DPIA-relevant. With a stable salt, **retention is the primary privacy
  control** (ADR-0016), not pseudonymization. The README must say this plainly rather than leaning on
  the word "pseudonymous."
- `salt_version` costs one column now. Without it, introducing rotation later makes it impossible to
  distinguish pre- from post-rotation pseudonyms, and that migration cannot be done retroactively.
- Email exists in flight inside the organization's network. It is never persisted (INV-2).

## Rejected alternatives and why

- **Laptop-side hashing.** Strongest-sounding claim — email never leaves the machine — but requires
  installing a real agent (contradicting ADR-0011), and the data is already inside the org network
  either way. Buys little, costs a whole class of deployment failures.
- **Store raw, mask on read.** Easiest. "We have your email but we promise not to look" is not a trust
  model, and it is one misconfigured Grafana instance away from being exactly the thing we promised not
  to build.
- **Per-tier salt lifecycle** (stable for T0, rotating quarterly for T1). The stronger long-term
  design and the one to revisit in v0.2. Deferred because break-glass needs vary by organization and
  over-engineering this before a single deployment exists would be guessing.
- **No salt retention at all** (hash once, destroy the salt). Best possible privacy claim; no break-glass
  and no per-person deletion, because you cannot find the person.
