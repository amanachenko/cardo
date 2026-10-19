# ADR-0048 — The coach is a page in the console, not a second Grafana

**Status:** Accepted
**Date:** 2026-10-18
**Narrows:** ADR-0032 (the coach), ADR-0044 (the console), roadmap item 8.

## Context

ADR-0032 chose a separate Grafana with shared links because Grafana cannot restrict a login to its
own rows: a Viewer can send any SQL to the data source, and the data source reads everything. That
was true when the only surface was Grafana.

The console now exists. It already authenticates with a per-port token, it can answer one request
without exposing a query interface, and it knows the pseudonym contract. A page served by the
console can therefore give one person their own figures and nothing else — stronger than a Grafana
shared link, which still holds a data-source connection behind it.

## Decision

1. **The coach is a console surface** at `#/coach/<pseudonym>`. The operator sees it first as a
   preview; a per-person link is issued by the operator later, one token per pseudonym, revocable.
2. **The page shows only that pseudonym's rows.** No fleet list, no other name, no ranking, no
   percentile, no single score. The comparison is always against the same person's previous window.
3. **At most three suggestions**, each naming a fix. Rules are visible in `internal/coach`; peer
   hints only appear when at least five other people used the artifact (ADR-0029).
4. **The CLI is the same data**: `cardo coach <email>` prints the figures and suggestions, so the
   page and the terminal cannot disagree.
5. **A second Grafana is no longer required for the coach.** Roadmap item 8 becomes "per-person
   links issued by the console"; the leadership Grafana keeps its gold-only user (ADR-0022).

## Consequences

- The console gains its second audience: engineers, not only the operator. Its token model must grow
  per-link tokens before a pilot hands pages out.
- Sharing a page no longer means sharing a Grafana login and a data source.
- The coach's numbers are the same views as everywhere else, so a bug shows up in both places.
