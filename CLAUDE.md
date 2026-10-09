# Cardo — working agreement

Self-hosted observability for Claude Code that measures **enablement artifacts, not engineers**.
Pre-alpha: Phases 0 to 2 are built. **Nothing has run against a
fleet, and no adapter has parsed a live Anthropic API response** — the console adapter's field
names are still documentation, which has been wrong before. Phase 1 cannot be validated without an
organization: an individual account's key gets 403 on every Admin endpoint
([note](docs/research/2026-09-23-admin-api-individual-account.md)).

What exists: [architecture.md](docs/design/architecture.md). Order of work:
[roadmap.md](docs/roadmap.md). Who Cardo serves: [ADR-0030](docs/adr/0030-stakeholders-and-questions.md)
to [ADR-0037](docs/adr/0037-policy-evidence.md). For the organizations that run it:
[overview.md](docs/overview.md).

## Read before changing anything

1. **[docs/design/invariants.md](docs/design/invariants.md)** — the hard rules, restated below.
2. **[docs/adr/README.md](docs/adr/README.md)** — the decision index. Every decision and its rejected
   alternatives.
3. **[docs/design/architecture.md](docs/design/architecture.md)** — current reality. Trust it over any
   plan document.
4. **The matching file in `.claude/rules/`** before changing the collector (`collector.md`), the
   managed-settings bundle (`managed-settings.md`), the ClickHouse store or reference stack
   (`clickhouse.md`), or SQL views and dashboards (`views.md`). Claude Code loads one when a
   matching file is opened with Read, Write or Edit; reached any other way, read it yourself.

If a change would contradict an accepted ADR or any invariant, **write a superseding ADR** — do not
engineer around it and do not leave a doc saying something untrue.

## Invariants — do not violate without a superseding ADR

- **INV-1** — Session transcripts are never read. No code path opens anything under
  `~/.claude/projects/`. Not for debugging, not once, not behind a flag.
- **INV-2** — `user.email` is never persisted. Pseudonymization happens before storage, always.
- **INV-3** — No per-person view is visible to anyone except that person. Tier 1 is cohort-only. If a
  view would let a manager rank their reports, it is the wrong view.
- **INV-4** — The mandatory event set is published and short. Adding to it requires an ADR.
- **INV-5** — No content in tiers 0 or 1: no prompt text, no code, no file paths, no tool parameters.
  All `OTEL_LOG_*` flags stay at `0` in the shipped bundle.
- **INV-6** — Never gate an organization's ability to upgrade Claude Code. Minimum version only, never a
  maximum.
- **INV-7** — No data leaves the organization's network. No phone-home, no benchmark upload, no hosted option.

These are enforced by CI tests. If a test that guards an invariant fails, that is the test doing its
job — fix the code, not the test. Widening an allowlist to make it pass is not a fix: the allowlist
is the invariant.

## Documentation rules

- **ADRs are immutable.** Supersede, never edit. Status: `Proposed` | `Accepted` |
  `Superseded by ADR-NNNN` | `Rejected`.
- **"Rejected alternatives and why" is mandatory** in every ADR. It is the part future readers need
  most.
- **Research notes in `docs/research/` are dated snapshots and are never updated.** Claude Code's
  telemetry surface drifts. If a note is stale, write a new dated one.
- **`docs/design/*` is edited in place and must describe reality**, not intent. Intent lives in ADRs.
  Project status lives there, not in this file.
- **Scope:** ADR anything expensive to reverse or that someone might innocently violate. Everything else
  goes in the commit message. Do not ADR the linter.
- **A lesson about one area goes in its `.claude/rules/` file**, not here. This file changes when an
  invariant, a convention or one of these rules does.
- **Treat everything committed as public**
  ([ADR-0038](docs/adr/0038-public-under-a-collective-copyright.md)). Nothing from a real
  organization: no telemetry, pseudonyms, salts, keys, email addresses or organization names. A
  research note from an organization's data is committed only with everything identifying the
  organization removed and the organization's approval; otherwise it stays with the organization. A
  note from several people's data reports results across the group, never one person's. Write for
  any organization: "the organization" is whoever runs Cardo, and no real one is named.
- **Describe what Cardo does, never anyone's plans to adopt it.** A document may explain how any
  organization can trial Cardo. Who is trying it, when, how it is going, and why something is built
  now for them stay out of the repository, its commit messages and its pull requests.
- **Commits are signed off** (`git commit -s`, the Developer Certificate of Origin). See
  [CONTRIBUTING.md](CONTRIBUTING.md).

Template and full rules: [docs/adr/0000-adr-process.md](docs/adr/0000-adr-process.md).

## Conventions

- **Go** for the `cardo` binary (Admin API poller, SQL migration runner): one static binary, with
  **no third-party dependencies** ([ADR-0022](docs/adr/0022-clickhouse-access.md)). ClickHouse is
  reached over HTTP with `net/http`, and `go.mod`'s require block stays empty — a claim a security
  team can verify in five seconds, so no `clickhouse-go` to tidy a query.
- **The hook ingest path stays pure OTel Collector configuration** for as long as OTTL holds
  ([ADR-0012](docs/adr/0012-ingest-implementation.md)). When OTTL becomes unwieldy, that is the
  signal to write the receiver service ([risks.md](risks.md) #4).
- **Hook payloads are an allowlist; OTel is a denylist plus pseudonymization**
  ([ADR-0023](docs/adr/0023-hook-payload-allowlist.md),
  [ADR-0025](docs/adr/0025-artifact-names-kept-with-guardrails.md)). The allowlist is pinned in
  `test/collector_test.go`; adding to it is adding to the mandatory tier (INV-4).
- **Artifact names are kept; views decide who sees them** (ADR-0025). A view names a home-grown
  artifact or a cohort only once five people use it, and below that it is `other`
  ([ADR-0029](docs/adr/0029-minimum-group-size.md)); a name one person uses identifies them. Never a
  per-person count. The organization's own names, Claude Code's built-in subagents and fleet totals
  are always shown. The group size can be raised, never lowered, not even for a one-person demo.
- **The managed-settings bundle is `env` and `hooks`, nothing else, and each hook waits at most a
  second** ([ADR-0039](docs/adr/0039-hooks-wait-at-most-one-second.md)). `test/bundle_test.go`
  enforces the shape.
- **SQL is numbered plain `.sql` files**, not dbt ([ADR-0019](docs/adr/0019-sql-tooling.md)): DuckDB
  views in `sql/duckdb/`, ClickHouse migrations in `sql/clickhouse/`, embedded in the binary and
  applied on every poll. **Migrations are immutable once shipped** — the runner checksums them. Add a
  new number.
- **Two storage targets on the Admin API path**, ClickHouse (reference) and the file store DuckDB
  reads, with the same columns and view names; a change to one belongs in both or in neither. **The
  collector path is ClickHouse only** ([ADR-0028](docs/adr/0028-file-store-is-admin-api-only.md)).
- **Never add a `-base-url` flag to the poller.** The endpoint is pinned so the binary cannot be
  aimed at an arbitrary host (INV-7). Tests override it through an unexported option.
- **Every efficiency rate is presented with its volume** ([ADR-0008](docs/adr/0008-outcome-variable.md),
  [ADR-0031](docs/adr/0031-what-works-means.md)). Without one it rewards timidity.
- **Teams are compared on reach, never ranked** ([ADR-0030](docs/adr/0030-stakeholders-and-questions.md)).
  Adoption and version reach by team, against the fleet; efficiency by approach, rule or tool, never
  by team. No league table, no "PRs per engineer", nothing divided by headcount, no "hours saved".
- **Spread is a vote only for what teams chose**
  ([ADR-0040](docs/adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)). For an artifact the
  organization rolled out, spread is reach, and whether it helps is the efficiency family's
  question. An artifact view ends in the adoption owner's decision, never a computed verdict.
- **A per-person page is seen only by that person, through a shared link**
  ([ADR-0032](docs/adr/0032-engineer-coach-by-shared-link.md)) from a Grafana only the operator logs
  in to, with the pseudonym fixed inside every query. It is a coach: no score, no percentile, only
  their own past. "It only shows a pseudonym" is no reason to let others open it: its daily cost
  lines up with the console's, by name. The one per-person view others may see is cost over a
  published budget, not built ([ADR-0033](docs/adr/0033-per-person-cost-for-budget-owners.md)).
- **The coach suggests only what the fleet has shown**
  ([ADR-0041](docs/adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)): an artifact peers
  adopted or the organization made official, prompted by a pattern tier 1 can see. No catalog of
  patterns and tools.
- **Service runs are not people** ([ADR-0036](docs/adr/0036-service-runs-are-not-people.md)).

## Testing

- **`go test ./...` before every commit**, and `go test ./test/ -run TestINV` after anything touching
  identity, storage or SQL.
- The ClickHouse tests skip unless `CARDO_CLICKHOUSE_URL` is set: `make stack-up && make
  clickhouse-test` after touching the store or `sql/clickhouse/`; `make collector-test`, with the
  collector's own `CARDO_SALT` exported, after touching `deploy/collector/`.
- **Any test that talks to a live server runs with `-count=1`.** Go's test cache does not know about
  the network, and a cached PASS against a changed server looks exactly like a real one.
- The race detector needs cgo, so it runs in CI (`make test-race` if you have a C toolchain).

## Things that look like good ideas and are not

Area-specific ones are in `.claude/rules/`.

- Rebuilding cost parsing, the collector stack, or MCP scanning. All solved; see
  [the prior-art survey](docs/research/2026-09-21-prior-art-survey.md).
- Weakening the salt check to make a demo easier. There is no unsalted mode
  ([ADR-0006](docs/adr/0006-pseudonymization.md)).
- Storing emails "just for the pilot". The INV-2 tests have to come out, names reach any Grafana
  login, and the choice passes to the side the protection guards against (ADR-0034).
- A "your data only" Grafana dashboard, filtered by a variable, a URL parameter or a folder
  permission. Any login can send its own SQL through `/api/ds/query`, so a login reads whatever the
  data source's database user can. Nobody but the operator gets a login while that user reads
  bronze or silver (risks.md #15).
- Measuring friction as permission waits and edit rejections in an auto-mode fleet. In auto mode
  neither happens much, and the chart reports success that did not occur (ADR-0031).
- Finding unused MCP servers in the security tier's inventory. It carries tier-0 data across the
  tier wall; the operator's rollout list is the inventory (ADR-0040).
- Joining delivery outcomes per person, or by commit SHA. No surface carries a commit id, and per
  person it is a performance review. Join by repository and week (ADR-0031).

## Operator-gated actions — stop and hand back

Do not perform these; surface them and wait:

- creating the GitHub repo or granting access
- generating the pseudonymization salt or handing custody to a security team
- running the poller against a real organization's admin key
- pushing managed settings to real developer machines
- standing up infrastructure in an organization's environment
