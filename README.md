# Cardo

**Does your AI coding enablement actually work?**

> *cardo* (Latin) — the *cardo maximus* was the main axis a Roman town was surveyed and laid out from;
> everything else was positioned relative to it. The platform team lays down the axis. Cardo measures
> whether the town actually got built along it.

Cardo is a self-hosted observability layer for Claude Code that measures **your enablement artifacts**,
not your engineers.

> **Pre-alpha.** Cardo has run against one engineer's Claude Code sessions, never against a team or
> a live organization, and much of what is described here is decided but not built. Expect breaking
> changes, and read [Status](#status) before relying on anything.

---

## The question it answers

You shipped an org-wide skill three weeks ago. Your managed `CLAUDE.md` is 400 lines. You have an
approved MCP server list and a set of permission rules.

Do you know whether any of it is being found, loaded, or used?

Cardo answers questions of that shape:

- Is the `deploy` skill being invoked, in which repos, and do sessions that invoke it look different?
- Is our managed `CLAUDE.md` actually loading, and which version is each machine on? Who is stale?
- What have engineers built for themselves that several of them now use, and that we should ship?
- Which permission rules generate the most prompt-wall friction?
- Which MCP servers are configured but never called?

**What it deliberately does not do** is tell you which engineer isn't entering plan mode. That framing
produces metrics people game and tooling people disable. Cardo puts the platform team's own work under
scrutiny instead — see [ADR-0001](docs/adr/0001-unit-of-analysis.md). Nor does it rank teams or
people, estimate "hours saved", or compute productivity per head
([ADR-0030](docs/adr/0030-stakeholders-and-questions.md)).

### Who it is for

- **Engineering leadership**, and whoever runs adoption. Which of the approaches our teams built
  should everyone use, what does each cost, and has what we shipped reached every team?
- **Engineers**, through a private coach page: suggestions about things they can change, compared
  only with their own past.
- **Security and compliance**, through evidence that the policies administrators set actually
  hold, including for organizations that have not yet allowed Claude Code at all.

The overview for leadership, security and engineers: **[docs/overview.md](docs/overview.md)**. The order
of work: **[docs/roadmap.md](docs/roadmap.md)**.

---

## What it collects — and what it never does

**Never, under any configuration:**

- reads your session transcripts (INV-1)
- stores your email address (INV-2)
- stores prompts, code, or the paths of files you touched (INV-5)
- sends anything outside your organization's network (INV-7)

**Collected without asking** (short by design; the list is the boundary, and adding to it requires a
recorded decision): session start and end, model and effort, tokens and cost, tool *classes*, skill
names, edit accept/reject, permission outcomes and durations, compaction events, model switches and what
they cost, which kinds of instructions files loaded, and the names of the commands, subagents and
MCP servers used, including ones engineers wrote for themselves. A name only one person uses is
never shown ([ADR-0025](docs/adr/0025-artifact-names-kept-with-guardrails.md)).

**Opt-in:** anything touching what you were working on. Opting in gets you your own dashboard, visible
only to you.

Full detail, written for the engineers being measured rather than the team deploying it:
**[docs/design/privacy.md](docs/design/privacy.md)**.

### Verify it yourself

There is **no binary on your machine**. The entire client side is a readable settings file. Every
content-redaction flag Claude Code offers stays at `0` in the shipped bundle
([ADR-0005](docs/adr/0005-collection-mechanism.md)).

---

## How it works

```
Claude Code  --- native OTel ------>  OTel Collector  -->  ClickHouse  -->  Grafana
             --- HTTP hooks, 1 s -->  (hashes email
                                        before storage)
  no installed binary                org-edge, your infra          your infra
```

Plus a zero-install tier: point the `cardo` binary at an Anthropic admin key and get fleet-level
adoption, cost and accept/reject rates with nothing deployed to any machine.

Architecture: **[docs/design/architecture.md](docs/design/architecture.md)**.
Data model: **[docs/design/data-model.md](docs/design/data-model.md)**.

---

## Status

**Pre-alpha.** The Admin-API path runs end to end: poll, pseudonymize, store to ClickHouse or to
flat files, query through the SQL layers, chart in Grafana. The ClickHouse half is tested against a
real server in CI.

The collector path runs end to end too. It has three parts:

- the managed-settings bundle and its twelve-event hook pack;
- a collector that pseudonymizes OTel and reduces hook payloads to an allowlist before anything is
  written;
- views and an enablement dashboard over what it stores.

The dashboard shows whether what the organization shipped is used, who is on a stale version of
its instructions files, what engineers built that several now use, and the friction and context
costs around it. It names a home-grown artifact or a team only once five people are in it. CI runs
the real collector against a real ClickHouse on every push, and the views against seeded data.

**The collector path has seen a real Claude Code; the Admin API path has not.** One engineer ran
Claude Code 2.1.281 against the reference stack with a single `--settings` flag
([how](deploy/managed-settings/README.md#trying-it-on-your-own-machine-one-engineer-no-mdm)). The
documentation turned out to be wrong about several field names, and the collector now follows what
Claude Code actually sends ([note](docs/research/2026-09-24-first-real-hook-payloads.md)). The
Admin API returns 403 to individual accounts, so the poller still needs an organization to validate
it ([measurement](docs/research/2026-09-23-admin-api-individual-account.md)).

| Phase | Contents | Status |
|---|---|---|
| 0 | ADRs, invariants, research snapshots, risks | **done** |
| 1 | `cardo` poller, pseudonymizer, both storage targets, SQL layers, dashboard, invariant tests | **done except verification** — no live API run |
| 2 | Managed settings, hook pack, collector config, artifact dashboard | **done**: collection checked against a real Claude Code, views and dashboard against those sessions; no fleet yet |
| 3 | The coach page, the efficiency family, repository identity, teams from a directory, service-run labels, the board report, the evidence report | decided 2026-09-25 ([ADR-0030 to 0037](docs/adr/README.md)); built in the order of the [work items](docs/roadmap.md#work-items) |

### Try it without a key

The SQL layer can be exercised against a generated store, with no credential and nothing deployed:

```bash
make sql
```

Or by hand. Note the script goes in on **stdin**: `.read` is a CLI input-loop command, and
`duckdb -c` parses its argument as SQL, so the `-c` form fails on the leading dot.

```bash
CARDO_SAMPLE_DIR=.sample go test ./test/ -run TestGenerateSampleStore
printf '%s\n' \
  "SET VARIABLE cardo_root = '.sample';" \
  ".read sql/duckdb/010_bronze.sql" \
  ".read sql/duckdb/020_silver.sql" \
  ".read sql/duckdb/030_gold.sql" \
  "SELECT * FROM gold_fleet_adoption ORDER BY day;" \
| duckdb -init /dev/null
```

### With an Admin API key

```bash
export CARDO_SALT=$(openssl rand -hex 32)   # custody belongs with your security team
export CARDO_ADMIN_KEY=sk-ant-admin-...     # Console > Settings > Admin keys
go build -o bin/cardo ./cmd/cardo
./bin/cardo poll -days 30 -out ./data
```

### With the reference stack

ClickHouse and Grafana, loopback-bound, on one machine:

```bash
cd deploy/compose && cp .env.example .env    # change both passwords
docker compose up -d
cd ../..

export CARDO_CLICKHOUSE_URL=http://127.0.0.1:8124
export CARDO_CLICKHOUSE_USER=cardo CARDO_CLICKHOUSE_PASSWORD=...
./bin/cardo poll -store clickhouse -days 30
```

Grafana is on `127.0.0.1:3001` with both dashboards already provisioned: the fleet dashboard
(Admin API) and the enablement dashboard (collector). The schema is applied
on every run from migrations embedded in the binary — there is no separate migrate step to forget.
No admin key yet? `make sample-seed` writes invented data so the dashboards have something to draw.

Details, including why the stack ships a ClickHouse config that turns off most of its own
telemetry: **[deploy/compose/README.md](deploy/compose/README.md)**.

### Retention deletes data

Tier-1 rows are kept for **90 days** and then removed by a ClickHouse TTL
([ADR-0016](docs/adr/0016-retention.md)). With a stable salt, retention rather than hashing is what
actually bounds exposure, so this is the privacy control and not a storage tidy-up. Note the
current limit: the gold marts are views, so trends older than 90 days go with the rows behind them
([risks.md](risks.md) #10).

---

`cardo` refuses to start without a salt of at least 32 hex characters, and the collector refuses to
store OTel data without one. There is no unsalted mode and no default salt: over an address space
as guessable as corporate email, a weak salt produces pseudonyms that are trivially reversible, and
a store that looks pseudonymous but is not is worse than one that does not run.

## Scope and requirements

- **Claude Code on Claude API direct.** Bedrock, Vertex, Microsoft Foundry and Claude Platform on AWS
  are out of scope for v1 — they have no Analytics API and no server-managed settings. The OTel and
  hook path would still work; the zero-install tier would not
  ([ADR-0021](docs/adr/0021-analytics-source-scope.md)).
- **Two kinds of organization, two adapters.** A Claude *Console* org uses the Claude Code Analytics
  API with an Admin API key — that is the adapter that exists. A Claude *Enterprise* (claude.ai) org
  reports Claude Code activity through the Claude Enterprise Analytics API instead, with a different
  key type created by the primary owner. That adapter is in scope for v1 and **is not built yet**. If
  you are on Enterprise seats, the current poller will authenticate and return nothing; it says so
  loudly rather than reporting an empty success ([ADR-0021](docs/adr/0021-analytics-source-scope.md),
  [risks #8 and #9](risks.md)).
- **Single-tenant and self-hosted.** There is no hosted option and there will not be one
  ([ADR-0013](docs/adr/0013-deployment-topology.md)).
- **A minimum Claude Code version, never a maximum.** Cardo will never gate your ability to upgrade
  ([ADR-0014](docs/adr/0014-version-drift.md)).

## Design record

Every decision, and every rejected alternative, is written down:

- **[docs/adr/README.md](docs/adr/README.md)** — the decision index
- **[docs/design/invariants.md](docs/design/invariants.md)** — the hard rules
- **[risks.md](risks.md)** — what might be wrong, stated honestly
- **[docs/research/](docs/research/)** — dated evidence snapshots

Start with [ADR-0001](docs/adr/0001-unit-of-analysis.md) if you want to understand why this is shaped
the way it is.

## Prior art

Cardo does not rebuild what exists. [`ccusage`](https://github.com/ccusage/ccusage) solves local cost
parsing; [`claude-code-otel`](https://github.com/ColeMurray/claude-code-otel) and Grafana's published
dashboards solve the collector stack; [`mcp-scan`](https://github.com/invariantlabs-ai/mcp-scan) solves
MCP security scanning. Full survey:
[docs/research/2026-09-21-prior-art-survey.md](docs/research/2026-09-21-prior-art-survey.md).

## Contributing

Welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first: commits are signed off, and nothing from a
real organization is ever committed. Report security problems privately, as
[SECURITY.md](SECURITY.md) describes.

## Licence

Apache-2.0, copyright The Cardo Authors ([NOTICE](NOTICE)). No CLA. See
[ADR-0038](docs/adr/0038-public-under-a-collective-copyright.md).
