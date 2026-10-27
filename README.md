# Cardo

**Does your AI coding enablement actually work?**

> *cardo* (Latin) — the *cardo maximus* was the main axis a Roman town was surveyed and laid out from;
> everything else was positioned relative to it. An organization lays down the axis: the ways of
> working it wants everyone on. Cardo shows whether the town actually got built along it.

Cardo is a self-hosted observability layer for Claude Code that measures **your enablement artifacts**,
not your engineers.

> **Pre-alpha.** Cardo has run against one engineer's Claude Code sessions, never against a team or
> a live organization, and much of what is described here is decided but not built. Expect breaking
> changes, and read [Status](#status) before relying on anything.

---

## Install the launcher

The launcher is one binary. It installs the stack, verifies it, and serves the installation board on
loopback. It runs from a clone of this repository, because the stack is built from the repository's
files: the compose file, the collector's configuration, the dashboards and the image's Dockerfile.
You need Git, Go 1.23 or later, and Docker Desktop 24 or later, running.

macOS and Linux:

```sh
git clone https://github.com/amanachenko/cardo.git && cd cardo
go build -o bin/cardo ./cmd/cardo
./bin/cardo ui     # opens the installation board
```

Windows (PowerShell):

```powershell
git clone https://github.com/amanachenko/cardo.git; cd cardo
go build -o bin\cardo.exe .\cmd\cardo
.\bin\cardo.exe ui
```

Run every `cardo` command from the root of the clone. To update, `git pull`, build again and run
`cardo up`: it rebuilds the stack's image from the checkout and applies any new migration, keeping
the salt and the data.

`install.sh` and `install.ps1` download a published release's binary and check its SHA-256. No
release has been published yet, and that binary would still have to run from a clone.

CI runs are not people: `deploy/ci/github-actions.card.yaml` turns on telemetry for GitHub Actions with
`cardo.actor=service` and `cardo.workflow=<workflow>`; those runs count as work and cost, never as a
person, a team member or a coach subject (ADR-0036).

The board is a console, not only an installer: **Overview** (is everything fresh, and what to do next),
**Catalog** (versioned enablement artifacts with a diff before approval), **Teams** (a hashed directory,
coverage, floors) and **Settings** (path, poller, floor, org artifacts). The installer itself lives in
Settings → Installation → *Open install and repair*, and on the Overview next-action when a check fails.
How each surface works, and how to run the coach and the one-time transcript importer:
**[docs/console-coach-history.md](docs/console-coach-history.md)**.

`cardo up` runs the same installation without the board, and on an installed stack applies an
update. `cardo check` re-runs the checks read-only, and `cardo connect --project <dir>` installs the
Claude Code settings file.

The Admin API path is configured on the same board: paste an organization admin key once and the
`poller` service starts inside the stack (Compose profile `admin`) on the interval you choose. The key
is written to `deploy/compose/.env` with mode 0600, never shown again, and the poller runs no cron on
any machine. Without a key the service is never started.

The first start downloads the Docker images and Grafana's ClickHouse plugin; the running stack sends
nothing out. `cardo probe`
proves the local pipeline with one synthetic event and deletes its rows immediately, so the session
model never counts it; `cardo check --proof` passes only when one of your own sessions has the fields
the views read.

## The question it answers

You shipped an org-wide skill three weeks ago. Your managed `CLAUDE.md` is 400 lines. You have an
approved MCP server list and a set of permission rules.

Do you know whether any of it is being found, loaded, or used?

Cardo answers questions of that shape:

- We rolled the `deploy` skill out to everyone. Has it reached every team, and do sessions look
  different since it arrived?
- Is our managed `CLAUDE.md` actually loading, and which machines still load an old version?
- What have engineers built for themselves that several of them now use, and that we should make
  official?
- Which connectors (MCP servers) did we roll out to everyone that almost nobody calls?

**What it deliberately does not do** is tell you which engineer isn't entering plan mode. That framing
produces metrics people game and tooling people disable. Cardo puts the organization's own tools
under scrutiny instead ([why it measures tools, not people](docs/adr/0001-unit-of-analysis.md)).
Nor does it rank teams or people, estimate "hours saved", or compute productivity per head. The full
list of what it will never do is in [where Cardo is going](docs/vision.md#never).

### Who it is for

- **Engineering leadership**, and whoever runs adoption. Which of the approaches our teams built
  should everyone use, what does each cost, and has what we shipped reached every team?
- **Engineers**, through a private coach page: suggestions about things they can change, compared
  only with their own past.
- **Security and compliance**, through evidence that the policies administrators set actually
  hold, including for organizations that have not yet allowed Claude Code at all.

The overview for leadership, security and engineers: **[docs/overview.md](docs/overview.md)**. What
Cardo is for, where it is heading, and how we choose what to build next:
**[docs/vision.md](docs/vision.md)**. The order of work: **[docs/roadmap.md](docs/roadmap.md)**.

---

## What it collects — and what it never does

**Never, under any configuration:**

- reads your session transcripts on the collection path (INV-1). `cardo import` reads them only when
  you run it yourself, keeps lengths and token counts, and never stores the text (ADR-0050)
- stores your email address (INV-2)
- stores prompts, code, or the paths of files you touched (INV-5)
- sends anything about your organization outside its network (INV-7). The binary contacts no host at
  all: prices come from a table inside it

**Collected without asking** (short by design; the list is the boundary, and adding to it requires a
recorded decision): session start and end, model and effort, tokens and cost, tool *classes*, skill
names, edit accept/reject, permission outcomes and durations, compaction events, model switches and what
they cost, which kinds of instructions files loaded, and the names of the commands, subagents and
MCP servers used, including ones engineers wrote for themselves. A name only one person uses is
never shown ([ADR-0025](docs/adr/0025-artifact-names-kept-with-guardrails.md)).

**Opt-in, not built yet:** anything touching what you were working on, for a dashboard of your own
that only you would see. Nothing of that kind is collected today, and it is built only when an
organization asks for it.

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

The dashboard shows, week by week, whether what the organization shipped is used, how many people
are still on an older version of its instructions files, what engineers brought in that several now
use, which tool keeps failing several times in a row, and where the money goes: by model, by team
and by Claude Code release. It names a home-grown artifact or a team only in a week with enough
people in it (five by default; the organization may raise that).
[What each chart is for](docs/design/dashboards.md) lists every chart, who it is for, and what it
cannot show. CI runs the real collector against a real ClickHouse on every push, and the views
against seeded data.

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
| 3 | The coach page, the efficiency family, repository identity, service-run labels, the board report, the evidence report — **teams from a directory is built** in the console (hashed loader, coverage, floors) | decided 2026-09-25 ([ADR-0030 to 0037](docs/adr/README.md)); built in the order of the [work items](docs/roadmap.md#work-items) |

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

### On a made-up organization

One person's sessions show almost nothing under the five-person minimum: a command is named once
five people use it, so with one person it shows as "other". A stack of its own holds 60 invented
engineers in six teams instead, sent through the collector, with stories planted in their sessions
and an answer key that every view is checked against:

```bash
bash scripts/made-up.sh up && bash scripts/made-up.sh load && bash scripts/made-up.sh check
```

The load takes about 16 minutes. Its Grafana is on `127.0.0.1:3003`. Nothing seen there is a finding
about real engineers ([ADR-0051](docs/adr/0051-a-made-up-organization.md)). How to run it, what was
planted, and what no view shows yet: **[docs/made-up-organization.md](docs/made-up-organization.md)**.

### Retention deletes data

Tier-1 rows are kept for **90 days by default** and then removed by a ClickHouse TTL
([ADR-0053](docs/adr/0053-retention-default-and-organization-choice.md)). With a stable salt,
retention rather than hashing is what actually bounds exposure, so this is the privacy control and
not a storage tidy-up. Note the current limit: the gold marts are views, so trends older than the
window go with the rows behind them ([risks.md](risks.md) #10).

Ninety days is long enough to compare a few weeks before and after a tool is rolled out, and short
enough that rows tied to a code name do not linger. The grouped totals, which carry no code name,
are kept anyway, so long-term trends do not need longer raw data.

**Your organization may change the window.** Settings → Retention window, or
`CARDO_RETENTION_DAYS`. A shorter window deletes sooner, which is what an organization with its own
retention rule needs; a longer one keeps rows tied to a code name alive for longer, and the
console says so, because your engineers have to be told.

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
  key type created by the primary owner. That adapter **is not built yet**, and it will be when an
  organization on Enterprise seats asks for it. If you are on Enterprise seats, the current poller will authenticate and return nothing; it says so
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

## Retention

Raw rows are deleted on a window set once. **90 days is the default**, and your organization may
change it; the console shows which one is in force. `0` keeps everything, and takes the tables' TTL
off so nothing expires on its own either; anything else is applied by the poller after every run,
and `cardo prune` does the same by hand:

```sh
cardo prune -days 30 --dry-run   # count, delete nothing
cardo prune -days 30             # delete raw rows older than 30 days
```

The window also changes the ClickHouse TTL on the bronze tables, so it works in both directions
(the tables ship with 90 days). Silver and gold are worked out from the raw rows, so they shorten
with them. The collector path's stored silver tables follow at their next rebuild, within 10
minutes, and their TTL moves with the raw tables'.

Ninety days is long enough to compare a few weeks before and after a tool is rolled out, and short
enough that rows tied to a code name do not linger. The grouped totals carry no code name and are
kept anyway, so long-term trends do not need longer raw data. A window above the default keeps rows
tied to a code name alive for longer, and the console says so — your engineers have to be told
([ADR-0053](docs/adr/0053-retention-default-and-organization-choice.md)).

## Verify the Admin API path without a key

The poller's endpoint is pinned; a local stand-in is the one exception, and it must be on loopback.

```sh
./bin/cardo mock-admin -port 8791 -days 2 -users 2 -api-keys 1
```

In another shell, point the poller at it with the same salt and ClickHouse the stack uses:

```sh
export CARDO_ADMIN_KEY=sk-ant-mock CARDO_SALT=... \
       CARDO_CLICKHOUSE_URL=http://127.0.0.1:8124 \
       CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=...
./bin/cardo poll -base-url http://127.0.0.1:8791 -days 2 -store clickhouse
```

Expect `user_actor` and `api_actor` rows, no email addresses in `payload`, and `cardo check`
reporting `ACTOR TYPES  api_actor N · user_actor M  OK`. Adding `-unknown 1` to the stand-in makes
that row fail with the undocumented type's name. Delete the synthetic rows with
`ALTER TABLE cardo.bronze_actor_day DELETE WHERE org_id = '00000000-0000-4000-8000-00000000cafe'`.

A base URL that is not loopback is refused: the key cannot be pointed at a host someone else runs.

## Contributing

Welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first: commits are signed off, and nothing from a
real organization is ever committed. Report security problems privately, as
[SECURITY.md](SECURITY.md) describes.

## Licence

Apache-2.0, copyright The Cardo Authors ([NOTICE](NOTICE)). No CLA. See
[ADR-0038](docs/adr/0038-public-under-a-collective-copyright.md).

## Coach

`cardo coach you@example.com` prints your own figures for the last 28 days against the 28 before
them: sessions, prompts, cost, peak context, failed tool calls, and the artifacts you loaded. It
ends with at most one suggestion, drawn only from what colleagues have shown, and it names the
number of people behind it. There is no rank, no percentile and no
single score, and no other person's rows are read. The console serves the same page at `#/coach`
(operator preview) and issues a per-person link you can hand out; the link carries one pseudonym and
nothing else, and it opens only on the machine that runs the console. `-days` changes the window,
`-json` prints the
report for a page or a test. Importing your existing transcripts with a one-time command, and the
link's security model, are covered in
[docs/console-coach-history.md](docs/console-coach-history.md).

