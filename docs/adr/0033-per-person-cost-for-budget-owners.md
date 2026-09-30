# ADR-0033 — A budget owner may see cost per person, and nothing more

**Status:** Accepted — **not built**, and not part of the pilots
**Date:** 2026-09-25
**Evidence:** `2026-09-25-stakeholder-field-notes.md` section 3 (leadership already watches cost per
person); `2026-09-23-admin-analytics-apis.md` (the fields Anthropic shows per person). The shape is
judgment.

Makes the one exception to INV-3. [ADR-0001](0001-unit-of-analysis.md) stands otherwise.

## Context

INV-3 says no per-person view is visible to anyone except that person.

The field notes describe a reality that INV-3 does not change. Leadership already sees each
person's cost by email, in Anthropic's console, Datadog or their own Snowflake exports. They ask
targeted questions of people above a budget, or above the average, or below it. Engineers know.

Refusing to show cost per person in Cardo therefore buys little trust, since engineers can see it is
shown anyway. It also costs adoption, because leadership wants one place to look. What Cardo can
earn trust with is what it adds on top, and how it shows the number.

## Decision

When an organization asks for it, Cardo offers a **per-person cost view for a budget owner**, in
this shape and no other:

- **The vendor's fields only.** Cost, tokens, sessions, lines, commits and PRs: what Anthropic's
  console already shows per person. Never anything Cardo adds: artifacts, efficiency, friction,
  context or coach suggestions.
- **Only people over a published budget.** It is a list of exceptions to a stated policy, not a
  ranking of everyone.
- **The engineer sees it first.** Their coach page shows their own position against the budget with
  the breakdown, and they are told when they cross it, before anyone else is.
- **Under-use is shown only by team.** "Why aren't you using it?" is the question that feels most
  like surveillance, and its fix is almost always better enablement for the team.
- **Service runs are excluded** ([ADR-0036](0036-service-runs-are-not-people.md)).

This gives engineers a promise they can check: **Cardo shows your manager nothing about you that
Anthropic's console doesn't already, and shows it to you first.**

## Consequences

- `docs/design/invariants.md` states the exception under INV-3, and says it is not built.
- **The view needs names.** Names exist only when Cardo delivers something
  ([ADR-0034](0034-identity-in-pilots.md)), so this view needs a delivery path that the pilots do
  not build. It is not part of any pilot. During the pilots leadership keeps using the console for
  names, which is exactly what this view would show them.
- When it is built, a test pins its field list, as the collector's allowlist is pinned.
- The budget has to be published to engineers before the view runs. Otherwise "a list of exceptions
  to a stated policy" is a ranking with a cut-off nobody was told about.

## Rejected alternatives and why

- **Teams only, with INV-3 unchanged.** Engineers know the console shows cost per person anyway,
  so refusing it earns little trust. It also sends leadership elsewhere for the single place they
  want.
- **A full per-person view** with cost plus everything Cardo knows. It is the surveillance tool
  ADR-0001 refused, and it hands managers the detail engineers were promised stays theirs.
- **A ranked list of everyone.** It makes both ends of the list targets. A budget line makes it a
  policy.
- **Under-use per person.** The most surveillance-like question, and the least actionable per
  person.
- **A pseudonymous per-person cost list.** Useless for the conversation leadership wants to have,
  and re-identifiable by lining it up with the console anyway.
