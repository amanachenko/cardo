# ADR-0022 — ClickHouse is reached over the HTTP interface, with the standard library only

**Status:** Accepted
**Date:** 2026-09-23
**Evidence:** Judgment, informed by ADR-0012's "minimal machinery" posture and by the measured write
volume of the Claude Code Analytics API (one row per actor per day).

## Context

[ADR-0009](0009-storage-agnostic-clickhouse-reference.md) makes ClickHouse the reference storage
target, and Phase 1 has to actually write to it. Go offers two routes:

1. `clickhouse-go/v2`, the official driver, speaking the native protocol on port 9000.
2. The HTTP interface on port 8123, reachable with `net/http` and nothing else.

The write volume this decision has to serve is small and bounded. The analytics APIs report one
record per actor per calendar day. A 200-engineer organization generates roughly 73,000 rows a
year — under two megabytes of JSON. The poller runs once a day, in a batch, with no latency
requirement of any kind.

The other force is ADR-0012 and ADR-0011: this project rests on the claim that there is very little
of our code between an organization's engineers and their data, and that what exists is auditable. A
dependency is not just a build artifact; it is something an organization's security team has to
review before the binary is allowed to run.

## Decision

The ClickHouse store speaks the **HTTP interface** and uses **only the Go standard library**.

`go.mod` keeps an empty `require` block. "This binary has no third-party dependencies" is a claim a
reviewer can verify in five seconds by opening one file, and it stays true.

Two details follow from the HTTP interface and are load-bearing rather than incidental:

- **Statements are split client-side**, because the interface executes one statement per request.
  The migration runner strips line comments and splits on `;`.
- **Credentials travel as `X-ClickHouse-User` / `X-ClickHouse-Key` headers**, never as query
  parameters. A password in a query string lands in ClickHouse's own `query_log` and in the access
  log of every proxy between here and there.

## Consequences

- The binary stays dependency-free and builds on a machine with no network access.
- No connection pooling beyond `net/http`'s, and no native-protocol compression. Both are
  irrelevant at a daily batch of a few thousand rows, and neither is hard to revisit: the store
  sits behind the `store.Store` interface, so swapping the transport touches one package.
- The driver's typed result scanning is unavailable, so queries ask for an explicit `FORMAT` and
  parse the response. In practice the store writes far more than it reads, and what it reads is the
  migration ledger.
- **This does not bind Phase 2.** The hook and OTel path exports to ClickHouse from the OTel
  Collector's own exporter (ADR-0012), not from this binary, so event-rate ingest is not this
  decision's problem.

## Rejected alternatives and why

- **`clickhouse-go/v2` (native protocol).** The right choice for a high-throughput ingest path, and
  the wrong one here: it buys throughput this workload does not need, in exchange for a dependency
  tree the claim above would have to be qualified around. Revisit if a future Go component in this
  repository writes at event rate — but ADR-0012 says that component should not exist.
- **`database/sql` plus a driver.** Same dependency cost, plus an abstraction layer that earns
  nothing when the only two operations are "execute DDL" and "insert newline-delimited JSON".
- **Write Parquet files and have ClickHouse ingest them.** Adds a staging directory, a second
  serialization format and a cleanup problem, to avoid an HTTP POST.
- **Reuse the JSONL store and point ClickHouse at the files.** Attractive, and it collapses under
  the first organization that wants ClickHouse on a different host from the poller.
