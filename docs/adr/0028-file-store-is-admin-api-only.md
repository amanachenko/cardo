# ADR-0028 — The file store serves the Admin API path only

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** Judgment, the operator's. The facts it rests on are in [risks.md](../../risks.md) #13
and in `2026-09-24-first-real-hook-payloads.md`, which measured OTel arriving on an individual
account, so the collector path can be evaluated by one engineer.
**Narrows:** [ADR-0009](0009-storage-agnostic-clickhouse-reference.md), which stays Accepted. Its
reference stack and its semantic layer are unchanged. This decides which path its evaluation mode
covers.

## Context

ADR-0009 added a DuckDB evaluation mode so that "a five-engineer pilot needs no infrastructure
approval at all". On the Admin API path that is true. `cardo poll -store file` writes NDJSON that
DuckDB reads, and nothing else runs anywhere.

The collector path has no such mode. The collector writes to ClickHouse only, and risks.md #13
asked for a decision before the Phase 2 views were written, because the answer decides whether
each of them is written once or twice.

The forces:

- **A collector pilot already needs infrastructure.** Somebody has to run an OpenTelemetry
  Collector where every laptop can reach it. The approval that the file store avoids on the Admin
  API path is already needed here.
- **The reference stack is one command.** `docker compose up` brings up the collector, ClickHouse
  and Grafana together. For one engineer evaluating on a laptop, that is the evaluation mode. It
  is how the two real sessions were recorded.
- **The alternative is expensive.** The collector's `file` exporter writes OTLP JSON: resource,
  scope and record nested three deep, with attributes as arrays of typed key-value pairs. Every
  collector-path view would need a second version in DuckDB's dialect over that shape. Each of
  those views joins across clocks, sessions and prompts, so they are the hardest SQL in the
  project. And the two versions would have to agree.
- **Nobody has asked.** Risks.md #13 named the evidence that would justify building it: a pilot
  team that can run a collector but cannot run ClickHouse. No such team exists.

## Decision

1. **The collector path writes to ClickHouse and nowhere else.** The collector has no `file`
   exporter, and no DuckDB SQL is written over its data.
2. **The views over the collector's data exist in the ClickHouse dialect only.** Their migrations
   are `sql/clickhouse/005` onwards.
3. **The rule that both stores hold the same columns and publish the same views applies to the
   Admin API path.** There it still holds, and it is still enforced by a test.
4. **Revisit when a pilot team can run a collector but cannot run ClickHouse.** That is a new ADR
   superseding this one, not a quiet addition.

## Consequences

- Each collector-path view is written once, and tested against one engine.
- Evaluating the collector path means running the reference stack. For one engineer on a laptop,
  that is `docker compose up`. For a team, somebody runs ClickHouse.
- A team that cannot run ClickHouse can still evaluate the Admin API path with no infrastructure.
  It cannot evaluate hook- and OTel-derived analytics (artifact loads, permission waits, context
  size) without ClickHouse.
- A dashboard that reads collector-path views works only on ClickHouse. The Admin API dashboard
  still works on either store.
- Risks.md #13 is closed.

## Rejected alternatives and why

- **Build the DuckDB mode for the collector path too.** It keeps ADR-0009's promise uniform, and
  it doubles the hardest SQL in the project over the least convenient format, for a team nobody
  has met. If that team appears, this is the ADR it supersedes.
- **Write the views so they run on both engines.** The collector-path views need an as-of join,
  map access and percentiles, and the two engines spell each of them differently. The Admin API
  views already needed a separate file per engine for less.
- **Embed an engine in the `cardo` binary** (chDB, or DuckDB through cgo) so the binary can answer
  queries itself. Each is a third-party dependency, and `go.mod`'s empty require block is a claim an
  organization's security team can check in five seconds ([ADR-0022](0022-clickhouse-access.md)).
- **Leave the question open.** Risks.md #13 said the decision was due before the silver views
  were written. Writing them for one engine while the question stayed open would have decided it
  anyway, without anyone saying so.
