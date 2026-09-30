# ADR-0019 — Plain SQL files with a light migration runner, not dbt

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** Judgment, no evidence.

## Context

ADR-0010 defines three layers — raw bronze tables, roughly six canonical silver views, and gold marts.
Something has to manage their creation and evolution across two storage targets (ClickHouse and DuckDB).

## Decision

Numbered plain `.sql` files under `sql/migrations/`, applied in order by a lightweight migration runner
(`golang-migrate` or equivalent) built into the existing `cardo` Go binary from ADR-0012.

Not dbt.

## Consequences

- No additional toolchain and no Python environment on the organization's infrastructure, consistent
  with the auditable-config, minimal-machinery posture that ADR-0011 and ADR-0012 establish.
- Any platform engineer can read `sql/migrations/` and know exactly what will be created.
- The migration runner is reused from a binary that already has to exist for the Admin API poller, so
  the marginal cost is close to zero.
- No lineage graph, no built-in tests over models, no generated docs. If the mart layer grows to the
  point where those matter, revisit with a superseding ADR.

## Rejected alternatives and why

- **dbt.** Real lineage, model tests, docs generation and incremental models, and well understood by
  data teams. Introduces a Python toolchain and a new concept for platform engineers to learn, for
  roughly six views plus marts. Disproportionate.
- **ClickHouse materialized views only, defined in DDL and versioned in git.** Simplest possible;
  painful to evolve, since materialized views do not backfill when their definition changes — which
  directly undercuts ADR-0010's promise that derived fields can be backfilled over history.
