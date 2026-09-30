# Cardo — working agreement

Self-hosted observability for Claude Code that measures **enablement artifacts, not engineers**.
Pre-alpha. Phase 0 (the design record) is complete and Phase 1 is built: the Admin-API poller,
pseudonymizer, both storage targets, both SQL dialects and a Grafana dashboard run end to end.
**Phase 2 is built** too. Its collection half is the managed-settings bundle, the 13-event hook
pack and the org-edge collector, verified against a running collector and ClickHouse **and against
two real Claude Code 2.1.281 sessions** ([first](docs/research/2026-09-24-first-real-hook-payloads.md),
[second](docs/research/2026-09-24-second-real-session.md)): 11 of 13 hook events arrive, OTel
arrives on an individual account, and the hook fixtures carry the observed field names. Its
analysis half is seven silver views, four gold marts (`sql/clickhouse/005` to `007`) and the
enablement dashboard, checked against those sessions. Nothing has run against a fleet.
**No adapter has yet parsed a live Anthropic API response** — the console adapter's field names are
still documentation, and documentation was wrong about four of the hook events' fields and about
two fields the design leaned on. The API itself has been reached: a personal key on an individual
account returns 403 on every Admin endpoint except `/v1/organizations/me`, which is an account-type
rule and not a key-type one. See
[the measurement](docs/research/2026-09-23-admin-api-individual-account.md); the practical effect is
that Phase 1 cannot be validated without an organization.

**What comes next was decided on 2026-09-25 and is not built.** A design review settled who Cardo
serves and what each of them needs:

- engineering leadership, deciding which team-grown approaches everyone should use and what they
  cost;
- engineers, through a private coach;
- security, through evidence that policy holds.

The decisions are [ADR-0030](docs/adr/0030-stakeholders-and-questions.md) to
[ADR-0037](docs/adr/0037-policy-evidence.md). Their order is **[docs/roadmap.md](docs/roadmap.md)**:
an internal trial, then an adoption pilot, then an evidence pilot. The version written for the
organizations that run it is [docs/overview.md](docs/overview.md).

## Read before changing anything

1. **[docs/design/invariants.md](docs/design/invariants.md)** — the hard rules, restated below.
2. **[docs/adr/README.md](docs/adr/README.md)** — the decision index. Every decision and its rejected
   alternatives.
3. **[docs/design/architecture.md](docs/design/architecture.md)** — current reality. Trust it over any
   plan document.

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
job — fix the code, not the test.

## Documentation rules

- **ADRs are immutable.** Supersede, never edit. Status: `Proposed` | `Accepted` |
  `Superseded by ADR-NNNN` | `Rejected`.
- **"Rejected alternatives and why" is mandatory** in every ADR. It is the part future readers need
  most.
- **Research notes in `docs/research/` are dated snapshots and are never updated.** Claude Code's
  telemetry surface drifts. If a note is stale, write a new dated one.
- **`docs/design/*` is edited in place and must describe reality**, not intent. Intent lives in ADRs.
- **Scope:** ADR anything expensive to reverse or that someone might innocently violate. Everything else
  goes in the commit message. Do not ADR the linter.
- **Treat everything committed as public**
  ([ADR-0038](docs/adr/0038-public-before-the-adoption-pilot.md)). Nothing from a real organization:
  no telemetry, pseudonyms, salts, keys, email addresses or organization names. A pilot's research
  note is committed only with everything identifying the organization removed and the organization's
  approval; otherwise it goes in a private repository. A note from the internal trial reports
  results across the group, never one person's. Write for any organization: "the organization" is
  whoever runs Cardo, and no real one is named.
- **Commits are signed off** (`git commit -s`, the Developer Certificate of Origin). See
  [CONTRIBUTING.md](CONTRIBUTING.md).

Template and full rules: [docs/adr/0000-adr-process.md](docs/adr/0000-adr-process.md).

## Conventions

- **Go** for the `cardo` binary (Admin API poller, SQL migration runner). A single static binary is
  much easier for a platform team to accept than a Python environment.
- **The hook ingest path stays pure OTel Collector configuration** for as long as OTTL holds
  ([ADR-0012](docs/adr/0012-ingest-implementation.md)). "No code we wrote touches your events" is worth
  preserving. When OTTL becomes unwieldy across the 13 payload shapes, that is the signal to write the
  receiver service — see [risks.md](risks.md) #4.
- **Hook payloads are an allowlist; OTel is a denylist plus pseudonymization**
  ([ADR-0023](docs/adr/0023-hook-payload-allowlist.md), upheld by
  [ADR-0025](docs/adr/0025-artifact-names-kept-with-guardrails.md)). Hooks carry content by design
  and nothing is redacted at source; OTel is redacted at source and flag-gated. The hook allowlist
  is pinned in `test/collector_test.go`, and adding to it is adding to the mandatory tier (INV-4).
- **Artifact names are kept; views decide who sees them**
  ([ADR-0025](docs/adr/0025-artifact-names-kept-with-guardrails.md)). Command, subagent and MCP
  names engineers define themselves are stored. A view names one only once the minimum group size
  of people use it, and never counts them per person. `CARDO_ORG_ARTIFACTS` labels what the
  organization shipped; `CARDO_ARTIFACT_NAMES=org-only` is the strict mode, and anything but `all`
  is strict. Instructions-file names are kept only when they are the organization's
  ([ADR-0026](docs/adr/0026-stale-instructions-by-versioned-name.md)).
- **The managed-settings bundle is `env` and `hooks`, nothing else**
  ([ADR-0024](docs/adr/0024-bundle-configures-telemetry-only.md)). Installing Cardo must not change
  how Claude Code behaves for an engineer beyond sending telemetry. `test/bundle_test.go` enforces
  the shape.
- **SQL is numbered plain `.sql` files** applied in order, not dbt
  ([ADR-0019](docs/adr/0019-sql-tooling.md)). DuckDB views in `sql/duckdb/`, ClickHouse migrations in
  `sql/clickhouse/`, embedded in the binary and applied on every poll. **Migrations are immutable
  once shipped** — the runner checksums them and refuses a file that changed. Add a new number.
- **Two storage targets, both real, on the Admin API path:** ClickHouse (reference) and the file
  store that DuckDB reads (evaluation mode). They hold the same columns and publish the same view and
  column names, so a dashboard cannot tell which one it is reading. A change to one belongs in both
  or in neither. **The collector path writes to ClickHouse only, and its views exist in the
  ClickHouse dialect only** ([ADR-0028](docs/adr/0028-file-store-is-admin-api-only.md)). Adding a
  file-store mode for it is a superseding ADR, prompted by a pilot team that can run a collector
  but not ClickHouse.
- **A view names a home-grown artifact or a cohort only once five people are in it**
  ([ADR-0029](docs/adr/0029-minimum-group-size.md)). Below that, it is `other`. The organization's
  own names, Claude Code's built-in subagents and fleet totals are always shown. The views read the
  pattern and the size from `cardo.settings`, which `cardo migrate` writes from `CARDO_ORG_ARTIFACTS`
  and `CARDO_MIN_GROUP_SIZE`. The size can be raised, never lowered.
- **`cardo` has no third-party dependencies and that is a feature** ([ADR-0022](docs/adr/0022-clickhouse-access.md)).
  ClickHouse is reached over its HTTP interface with `net/http`. Keep `go.mod`'s require block empty.
- **`go test ./...` before every commit**, and `go test ./test/ -run TestINV` after anything touching
  identity, storage or SQL. The ClickHouse tests skip unless `CARDO_CLICKHOUSE_URL` is set: run
  `make stack-up && make clickhouse-test` after touching the store or `sql/clickhouse/`, and
  `make collector-test` (with the collector's own `CARDO_SALT` exported) after touching
  `deploy/collector/`. **Any test that talks to a live server runs with `-count=1`**: Go's test
  cache does not know about the network, and a cached PASS against a changed server looks exactly
  like a real one. The race detector needs cgo, so it runs in CI rather than locally
  (`make test-race` if you have a C toolchain).
- **Never add a `-base-url` flag to the poller.** The endpoint is pinned so the binary cannot be
  aimed at an arbitrary host (INV-7). Tests override it through an unexported option.
- **Every efficiency rate is presented with its volume.** Without one it rewards timidity
  ([ADR-0008](docs/adr/0008-outcome-variable.md), carried into
  [ADR-0031](docs/adr/0031-what-works-means.md), whose efficiency family replaces the friction
  index). This is a product constraint, not styling.
- **Teams are compared on reach, never ranked** ([ADR-0030](docs/adr/0030-stakeholders-and-questions.md)).
  Adoption and version reach are shown by team, against the fleet as a whole. Efficiency is shown by
  approach, rule or tool, not by team. Nothing is divided by headcount, and there is no "hours saved".
- **A per-person page is seen only by that person, and in Grafana that means a shared link**
  ([ADR-0032](docs/adr/0032-engineer-coach-by-shared-link.md)). The link comes from a Grafana only
  the operator logs in to, with the pseudonym fixed inside every query. The engineer's page is a
  coach: no score, no percentile, only their own past. The one per-person view anyone else may see
  is cost over a published budget, the vendor's fields only, not built
  ([ADR-0033](docs/adr/0033-per-person-cost-for-budget-owners.md)).
- **Service runs are not people** ([ADR-0036](docs/adr/0036-service-runs-are-not-people.md)).
  `actor_kind=service` rows stay out of people counts, the five-person rule, budget lists and the
  coach.

## Things that look like good ideas and are not

- Reading a transcript "just to debug this one thing" — INV-1.
- A leaderboard, a per-engineer ranking, or any view a manager could use to compare reports — INV-3.
- Pinning `requiredMaximumVersion` to guarantee correctness — INV-6.
- Adding a field to the mandatory tier because it would be useful — INV-4, write the ADR first.
- Rebuilding cost parsing, the collector stack, or MCP scanning. All solved; see
  [the prior-art survey](docs/research/2026-09-21-prior-art-survey.md).
- Reading `estimated_cost.amount` as dollars. **It is cents**, on both analytics APIs. Silver divides
  once and names the column `cost_usd`; nothing downstream should divide again.
- Weakening the salt check to make a demo easier. There is no unsalted mode
  ([ADR-0006](docs/adr/0006-pseudonymization.md)).
- "Fixing" a failing invariant test by widening its allowlist. The allowlist is the invariant.
- Adding `clickhouse-go` to make a query tidier. The empty require block is a claim an organization's
  security team can verify in five seconds ([ADR-0022](docs/adr/0022-clickhouse-access.md)).
- `sum(x) AS x` in a ClickHouse view, or in a dashboard panel. Legal in DuckDB, an error here as
  soon as `x` is used again in the same query: a panel computing
  `sum(r) / nullIf(sum(sessions), 0)` beside `sum(sessions) AS sessions` fails with "aggregate
  function found inside another". Aggregate in a subquery with a `_total` suffix and rename
  outside, so the published names still match. Run a new panel's query through Grafana, not only
  through ClickHouse directly: the macros are Grafana's.
- Replacing a day with DELETE-then-INSERT instead of `REPLACE PARTITION`. A process that dies in
  between leaves the day empty, and an empty day on a dashboard looks exactly like a quiet one.
- Leaving ClickHouse's system logs at their defaults. A stock single-node server spends a continuous
  fraction of a core merging its own diagnostics, forever, with no TTL — measured at 894 MiB of
  `system.*` against 1.65 MiB of real data. See `deploy/compose/clickhouse/config.d/`.
- Trusting a ClickHouse server-side default that affects correctness. `async_insert` flipped to **on**
  in 26.x, which put cardo's staging INSERT behind a queue that `REPLACE PARTITION` does not wait for.
  `Insert` sends `async_insert=0` and `input_format_skip_unknown_fields=0` on every call; the
  destination is the operator's ClickHouse (INV-7) and they may never have seen our overlay.
- Adding a file to `deploy/compose/clickhouse/config.d/` and stopping there. It is mounted a file at a
  time, so an overlay without a `volumes:` line is silently ignored: clean start, no warning, stock
  defaults. `TestDeployOverlaysAreAllMounted` is the only thing that notices.
- Tuning ClickHouse from a list of plausible settings. The timer and pool defaults all *looked* like
  the idle-CPU culprit and together changed it by nothing; one stack trace found the real cause in a
  minute. `system.stack_trace` with `allow_introspection_functions=1`, then read the frames.
- A `--` inside an XML comment. The spec forbids it, ClickHouse crash-loops, and the only symptom is
  a restarting container. `TestDeployXMLIsWellFormed` catches it.
- Adding a hook field to the collector's `keep_keys` "because it would be useful". That is the
  mandatory tier growing (INV-4). The pinned list in `test/collector_test.go` is the invariant.
- Putting a generic hook field (`reason`, `trigger`, `source`, `prompt`, `message`, `error`) on the
  allowlist because the event you are looking at uses it harmlessly. The list is a union across
  events. Map it onto a specific name for that one event, guarded to an enum's shape, as
  `SessionEnd`'s `reason` becomes `session_end_reason`. `TestCollectorScopesGenericHookFields`
  checks the scoping.
- Trusting documentation, and above all a *summary* of documentation, for a hook field name. The
  2026-09-23 note was built from WebFetch summaries and was wrong about four events plus
  `content_hash` and `tool_use_id`. `cardo.received_keys` from a real session is the authority.
- Leaving `webhook_event`'s `max_request_body_size` at its 100 KiB default. Larger bodies are
  refused with a 400 and no collector log line; the only trace is a hook error count in Claude
  Code's own OTel. Real `SubagentStop` payloads were lost this way.
- Counting subagents from `SubagentStop`. Claude Code's own helpers, prompt suggestion and
  compaction, fire it with no `SubagentStart`. Pair the two on `agent_id`.
- Subtracting a hook row's time from an OTel row's. A hook row carries the collector's receive
  time; OTel carries the laptop's. On the reference stack they were 1 to 3 s apart, and not steady,
  which added about 1.3 s to each real permission wait. Both ends of a duration come from one
  clock: a permission wait starts at OTel's `hook_execution_start` for `PermissionRequest:<tool>`.
- Reporting `PreModelSwitch`'s `estimated_cache_write_usd` as what a switch cost. It assumes the
  whole context is rewritten. In the switch observed, most of it was still cached, and the estimate
  was 3.2 times the next request's entire `cost_usd`. The next main-thread `api_request` is the cost.
- Showing an artifact name that only one or two people use, or any per-person count of
  home-grown commands. Names are stored so that *convergence* is visible (ADR-0025); a name only
  one person uses is a quasi-identifier.
- Lowering `CARDO_MIN_GROUP_SIZE` below five to make a one-person evaluation show more. It is the
  salt again: a weak value that makes a demo easier is what an invariant refuses (ADR-0029). A
  single evaluator sees the organization's names and every total; their own commands are `other`.
- Adding up `people` or `active_people` across weeks or days. They are distinct counts; the same
  engineer on five days is one person, not five. A panel over a range shows the busiest week or
  day. Sessions, uses and prompts add up.
- Ordering rows by a server timestamp to decide which write is newest. The reference stack's
  Docker VM clock steps backwards, and a settings write 5 ms after another lost to it one run in
  eight. `cardo.settings` orders by a version the server computes as one above the highest.
- Checking that a panel charts a rate's volume by looking for the volume's name in the SQL. Each
  rate is computed from its volume, so the name is always there. The test requires the volume as
  an output column.
- Storing emails "just for the pilot" to keep things simple. It is more work: the tripwires and
  INV-2 tests have to come out. It exposes names to any Grafana login. It makes the security review
  harder, and it hands the choice to the side the protection guards against. Considered and
  refused in ADR-0034.
- A per-person page that others can open "because it only shows a pseudonym". Its daily cost lines
  up with the cost Anthropic's console shows by name, and then everything else on it has a name
  too.
- A "your data only" Grafana dashboard, filtered by a variable, a URL parameter or a folder
  permission. Any login can send its own SQL to the data source through `/api/ds/query`; measured
  on 2026-09-25. What a login can read is exactly what the data source's database user can read.
- Giving anyone but the operator a Grafana login while its data source reads bronze or silver
  (risks.md #15).
- Measuring friction as permission waits and edit rejections in an auto-mode fleet. In auto mode
  neither happens much, and the chart reports success that did not occur (ADR-0031).
- A team league table, friction by team, "PRs per engineer", or an "hours saved" figure. Each is
  the surveillance or indefensible-number failure ADR-0030 exists to refuse.
- Keeping an external repository's URL, or a salted hash of it, in tier 1. The collector keeps the
  organization's repositories by name and reduces everything else to `external` plus the host.
  A blank `CARDO_ORG_REPOS` means everything is external, never everything is the organization's
  (ADR-0035).
- Joining delivery outcomes per person, or by commit SHA. No surface carries a commit id, and per
  person it is a performance review. Join by repository and week (ADR-0031).
- Hashing a migration's raw bytes. On Windows, git rewrites line endings when it touches a file,
  and one checkout held `003_gold.sql` as CRLF beside LF siblings. The runner hashes with line
  endings normalized, and still accepts checksums recorded the old way.
- Capturing raw hook payloads to see their real shape. `cardo.received_keys` already records every
  field name that arrived; nothing needs the values of the fields that were dropped.
- `allowManagedHooksOnly`, `allowedHttpHookUrls`, or any other policy in the bundle. Each silently
  changes Claude Code for every engineer, and `allowedHttpHookUrls` in particular breaks every
  existing HTTP hook not on the list (ADR-0024).
- `error_mode: ignore` on any collector processor. A statement that errors is skipped and the record
  exported anyway — for the identity transform, that is an unhashed email.
- Treating a blank regex as "no pattern" in OTTL. An empty regex matches everything; a blank
  `CARDO_ORG_ARTIFACTS` kept every private name until each statement special-cased `""`.
- Relying on the collector's `${env:...}` to fail on a missing variable. It expands to empty with a
  startup warning, which is why a missing salt is caught by a statement that refuses the batch.
- Upgrading the collector image as a tag bump. OTTL syntax, component names and the exporter's
  columns move between releases; see `deploy/collector/README.md`.

## Operator-gated actions — stop and hand back

Do not perform these; surface them and wait:

- creating the GitHub repo or granting access
- generating the pseudonymization salt or handing custody to a security team
- running the poller against a real organization's admin key
- pushing managed settings to real developer machines
- standing up infrastructure in an organization's environment
- publishing the repository publicly ([ADR-0038](docs/adr/0038-public-before-the-adoption-pilot.md) says when)
