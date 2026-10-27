# ADR-0057 — ClickHouse is the only store; the file store and its DuckDB views are removed

**Status:** Accepted
**Date:** 2026-10-27
**Evidence:** Judgment, the owner's, made in the review of the console's pull request on
2026-10-19. The facts it rests on are under Context, each one checkable in the repository.
**Supersedes:** [ADR-0009](0009-storage-agnostic-clickhouse-reference.md) and
[ADR-0028](0028-file-store-is-admin-api-only.md).
**Narrows:** [ADR-0043](0043-admin-poller-in-the-reference-stack.md), whose CLI no longer has
`-store file`. The poller service it decided is unchanged.

## Context

The storage-agnostic decision (ADR-0009) chose ClickHouse and Grafana as the reference stack, and
added a mode with nothing installed: `cardo poll -store file` wrote one NDJSON file per day, and
the views in `sql/duckdb/` read them. The file-store decision (ADR-0028) kept that mode to the
Admin API path. The collector path, which carries artifact use, friction, context and spend by
team, has been ClickHouse only from the start.

Since then:

- **Everything built reads ClickHouse alone.** The console, the coach, `cardo import`, the
  retention setting, the poller as a service (ADR-0043) and stored silver
  ([ADR-0052](0052-silver-is-stored-and-rebuilt-every-10-minutes.md)) have no file-store version.
  On the file store, a person sees the Admin API's daily figures, and nothing else Cardo computes.
- **Running the reference stack is one command.** `cardo up` installs it on one machine and proves
  that data arrives. ADR-0028 already said that this is what evaluating the collector path means.
- **The file store had a cost on every change.** Each Admin API view was written twice, in two
  dialects (`sql/duckdb/020` and `sql/clickhouse/002`, for example), under a rule that a change to
  one belonged in both. CI downloaded DuckDB to check them. When DuckDB 2.0 renamed its download,
  that step failed on every branch until it was pinned to 1.5.6.

## Decision

1. **ClickHouse is the only store.** `internal/store/jsonl/`, `sql/duckdb/`, the DuckDB checks and
   the CI step that ran them are removed. `cardo poll` writes to ClickHouse by default.
   `-store clickhouse` still works, so existing scripts and the stack's own command keep running.
   `-store file` is refused with a message naming this record, and `-out` is gone.
2. **The reference stack is unchanged.** It is still ClickHouse, Grafana and the collector on one
   machine, as ADR-0009 chose, and `cardo up` installs it. To try Cardo on either path, an
   organization runs it, or its own ClickHouse.
3. **The views are written for ClickHouse only.** Porting them to another engine needs a new
   record, written when an organization that cannot run ClickHouse asks for one.
4. **No check is lost with the file store.** The invariant tests that wrote through the file store
   now write through the ClickHouse store to a recording server on loopback. So every `go test`
   still checks, with no database, that no email address reaches the store and that bronze sends
   only the allowed columns. The one property only the DuckDB checks covered is now checked
   against ClickHouse (`TestAbsentCountersReadAsZero`): a tool missing from a record counts as 0,
   not as a blank that drops the whole row from a sum.
5. **The store interface stays.** A store accepts `pseudonym.Scrubbed` and nothing else, which is
   how INV-2 is enforced at compile time, and the poller's tests stand a recording store behind it.

## Consequences

- Each view is written once, in one dialect, and tested against one engine. CI downloads no
  database.
- Seeing any of Cardo's views now needs Docker or a ClickHouse. Without an Admin API key,
  `cardo mock-admin` and `make sample-seed` still give the stack something to draw.
- The storage-agnostic claim is withdrawn. The managed-settings bundle sends standard OTLP, and
  the collector is standard OpenTelemetry Collector configuration, but the views, the dashboards,
  the console and the coach run on ClickHouse alone.
- The "two storage targets" convention leaves CLAUDE.md. Migrations already shipped keep their
  comments about DuckDB, because migrations are immutable.
- The question the file-store decision left open, a file-store mode for the collector path, is
  closed with it. Risk 13 stays closed.

## Rejected alternatives and why

- **Keep the file store for the Admin API path, as ADR-0028 left it.** Every Admin API view would
  still be written twice, and CI would still download an engine that broke once, for a mode that
  shows the daily figures and none of what was built after them.
- **Keep the file writer and drop only the DuckDB views.** Nothing would read the files. A copy
  of pseudonymous telemetry on disk that nothing reads or ages out is a liability, not a feature. An
  export, if one is needed, is a decision of its own, with its own retention.
- **Write the views so they run on both engines.** ADR-0028 rejected this already: as-of joins,
  map access and percentiles are spelled differently, and stored silver depends on ClickHouse's
  refreshable materialized views, which DuckDB does not have.
- **Embed an engine in the binary** (chDB, or DuckDB through cgo). Each is a third-party
  dependency, and `go.mod`'s empty require block is a claim a security team can check in five
  seconds ([ADR-0022](0022-clickhouse-access.md)).
- **Wait until someone asks for the removal.** The cost is paid on every change to an Admin API
  view and on every CI run. The benefit goes only to someone who can run neither Docker nor
  ClickHouse, and for them the file store shows the Admin API's figures alone.
