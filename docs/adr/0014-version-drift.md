# ADR-0014 — Minimum version only, tolerant parsing, CI fixture corpus

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (attributes already gated at `2.1.214+`
and `2.1.269+`; `requiredMinimumVersion` / `requiredMaximumVersion` exist in managed settings).

## Context

Hook payloads, OTel attribute names and the `claude_code.*` namespace all move between Claude Code
releases. Managed settings can pin a version range, which would guarantee our correctness.

## Decision

Declare a **minimum** supported Claude Code version. **Never a ceiling.** Combine with:

- **tolerant parsing** — raw-first storage (ADR-0010) means unknown fields are never lost and derived
  views can be backfilled over history once fixed
- **a CI fixture corpus** — recorded payload samples per Claude Code version, replayed through the
  collector config, asserting the canonical views still populate
- **a published compatibility matrix** in the README: "tested through 2.1.x; newer versions ingest raw,
  but new attributes need a view update"

## Consequences

- An organization can upgrade Claude Code whenever it likes. This is INV-6 and it is non-negotiable.
- Breakage surfaces as a failing CI test rather than as an organization's empty dashboard.
- Adding a fixture on every Claude Code version bump is ongoing maintenance. Accepted.

## Rejected alternatives and why

- **Pin a tested version range** via `requiredMinimumVersion`/`requiredMaximumVersion`. Guarantees
  correctness and gates an organization's Claude Code upgrades on our release cadence. Platform
  teams will remove the tool rather than accept that, and it is a security-negative posture besides
  — it would hold a fleet back from a release containing a security fix.
- **Tolerant parsing only.** Lowest effort; you find out from a user rather than from CI.
- **CI fixtures only.** Early warning without the documentation burden, but leaves users guessing about
  support.
