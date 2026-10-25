# The next batch: eleven open items, specified

**Status:** proposal. Nothing here is built. This page says what each open item is, what proves it
is open, what closing it looks like, and what has to be decided before anyone writes code.

This is the specification that follows the coverage audit of 2026-10-25. Every claim below was
checked against the code, not against a document: a step that is listed but returns "not
implemented" is open, and a document that says "not built" about something that was built later is
not evidence that it is still open.

**How to read a row.** Each item names the file that proves it open and the check that would prove
it closed. An item that needs a decision says so, and the decision is a new or superseding record
([the ADR process](adr/0000-adr-process.md)); the code waits for it.

**Two things that are not in the eleven, and are also true.** The docs say some things are open that
are built — `docs/design/architecture.md` still calls the Grafana access gap unfixed, and
`docs/design/privacy.md` still lists the coach page and the service label as "decided, not built".
Those are corrections to existing documents, not new work, and they travel with the item they
concern.

---

## The order

The items are grouped by what unblocks the most work first, not by the order they were found in.

| # | Item | Kind | Needs a decision | Depends on | State |
|---|---|---|---|---|---|
| 1 | The pilot rows of the launcher | code | no | — | built 2026-10-25 |
| 2 | The engineer notice | code | no | 1 | built 2026-10-25 |
| 3 | The signs of struggle, first version | code + a record | yes | — | open |
| 4 | The collector's open door | a record | yes | — | open |
| 5 | Repository identity, read | code | no | — | open |
| 6 | The catalog and the views, joined | code | yes | — | open |
| 7 | Policy evidence | code | no | — | open |
| 8 | Team coverage, in the views | code | no | — | open |
| 9 | The board report | code | no | 3, and an organization | open |
| 10 | The gaps in the guide | code + documents | no | — | open |
| 11 | Aggregates that outlive their rows | code | no | — | open |

**The largest is 3.** It is the only one whose output is a set of numbers people will argue about,
and every other item about "what works" waits for it.

**The cheapest and most urgent is 1.** The launcher already promises these rows: `cardo up --pilot`
accepts the flag, the board draws them, and the design document lists them with their checks. The
dispatch case returns "not implemented". A pilot cannot start until this is built.

---

## 1. The pilot rows of the launcher

**What the spec says.** `docs/design/quickstart-launcher.md` lists three pilot rows: the leaders'
logins (row 08), the file on the machines (row 09) and the notice (row 10). The design says the
pilot path takes about forty minutes against ten for a solo install, and that rows 08 to 10 are what
the extra thirty minutes buy.

**What the code said.** `internal/launcher/steps.go:220` returned a placeholder for all three:

```go
case "grafana_ro", "bundle", "notice":
    return Result{ID: id, Name: "PILOT", Value: "not implemented", ...}, nil
```

The steps were listed in the table, the board drew them, and `cmd/cardo/up.go:31` took `--pilot`.
So the surface said yes and the dispatch said no.

**Built on 2026-10-25.** The three rows are `gold`, `bundle` and `notice`, and all three run. Two
things turned out to be different from what this page first said:

- There was no `grafana_ro` step to implement. The design document's row 08, "Leaders' logins", is
  the `gold` step, and it was already built. Only the dispatch case was a leftover, so it was
  removed rather than wired up.
- The pilot rows were not excluded from "install all" — they were marked `Planned`, which the board
  shows as "Later" and skips. Taking that mark off made them part of every install, so the server
  now skips a pilot row unless the organization chose the pilot path (`pilot_mode` in the settings
  table, the same setting the board writes).

**What was already there, and was not rebuilt.** The read-only user was not missing: `gold` was
implemented (`steps.go` dispatches to `stepGrafanaRO` in `internal/launcher/grafana_ro.go:19`), it
creates `cardo_ro`, and `checkGoldAccess` (`grafana_ro.go:125`) proves the refusal live. The
bundle file was not missing either: `deploy/managed-settings/managed-settings.json` exists, and
`test/bundle_test.go` pins its shape. What was missing was that the pilot path did not write the
second one anywhere a machine could pick it up.

**What closing it looks like.**

- The `bundle` step writes the bundle to a path the operator can hand to an MDM, and says which
  path. The file it writes is the shipped one, with the collector's address inside the
  organization's network — the bundle is `env` and `hooks`, nothing else, each hook waits at most a
  second ([ADR-0039](adr/0039-hooks-wait-at-most-one-second.md),
  [ADR-0024](adr/0024-bundle-configures-telemetry-only.md)). On a laptop the collector is on
  loopback; a pilot that serves a team reaches it through the edge proxy, on the hostname the
  certificate is for.
- The `notice` step writes the notice (item 2).
- `cardo up --pilot` runs all three and each row's check passes: the read-only user is refused on
  raw and per-person tables, the bundle is written, the notice exists. A solo install runs seven
  rows and writes neither file.
- The placeholder is gone. A step that is listed and returns "not implemented" is the exact
  failure this repo's own guidance warns about.

**The check that would prove it closed:** `cardo up --pilot` on a clean machine, then read the
board: three rows pass, none says "PILOT" and "not implemented". **Proved on 2026-10-25** against
the reference stack: ten rows complete, and a solo run completes seven.

**Watch for.** Writing the bundle is a file on the operator's machine, not an installation on a
developer's. Pushing managed settings to real developer machines is an owner-gated action and stays
one: the launcher writes a file, and a human hands it over.

---

## 2. The engineer notice

**What the spec says.** `docs/roadmap.md` Step 1, marked *Open*: a notice an organization hands to
its engineers. It says who holds the salt, which settings are on, and that Cardo's data is not for
individual performance decisions.

**What the code said.** The step existed and was a placeholder (item 1). The command the step
named, `cardo notice --format md`, was not in the command table at `cmd/cardo/main.go:109`. The
design document listed the command as if it existed (`quickstart-launcher.md:115`: "`cardo notice` |
writes the engineer notice from the current `.env`").

**Built on 2026-10-25.** The command exists and writes the page, and the launcher writes it as
well. It is in `internal/notice`, so the page can be read and tested without the CLI.

**What it must say.** The three things the roadmap names, and the two that other decisions add:
which way the organization set the instructions-names setting ([ADR-0035](adr/0035-repository-identity-classified.md)
says the notice states it), and that the group size is the organization's own
([ADR-0029](adr/0029-minimum-group-size.md)).

**What closing it looks like.** A command that writes one page from the current `.env`, and the page
is checkable: a test reads the rendered page and asserts the required fields are present, the way
`test/bundle_test.go` asserts the bundle's shape.

**The check that would prove it closed:** `cardo notice --format md` writes a page containing the
salt custody, the enabled settings, the group size, and the sentence about performance decisions.

**Watch for.** It must not contain a real email address or a real pseudonym — it is a document an
organization hands out, and everything committed is public
([ADR-0038](adr/0038-public-under-a-collective-copyright.md)). It describes the settings file, not
the people in it.

**Two things the build had to decide, and one it could not.**

- The salt is named, never printed: the page says who holds it and which version is in force, and
  the salt itself is not in the page. It is the key that could reveal who is behind a code name.
- A pattern that names the organization is reported as set, without its text. `CARDO_ORG_REPOS`
  carries the organization's own name, and the page is handed to every engineer.
- **The instructions-names line could not report a setting.** ADR-0035 names
  `CARDO_INSTRUCTIONS_NAMES=project`, and the setting does not exist in the code yet — that is
  item 5. A page that claimed a setting was in force when it was not would be worse than no page,
  so the line reports the rule the collector actually applies today (ADR-0026: a name is kept when
  it matches the organization's artifact pattern). When the setting lands, the line changes with
  it.

---

## 3. The signs of struggle, first version

**What the spec says.** [ADR-0031](adr/0031-what-works-means.md) replaces the friction index with
the **efficiency family**, and names the signals: failed tool calls and retry loops; auto-mode
denials; compactions; churn; turns and cost per accepted change; permission waits for people in
default mode. It says the family's numbers are computed **within the same people** — sessions that
use an approach are compared with sessions that do not, inside the same person, so "champions are
better engineers" does not show up as the tool's effect.

ADR-0031 also requires, before any organization's data is read, that **each claim to be tested is
written down with the result that would prove it wrong**. `docs/roadmap.md` marks this *Open*.

**What the code says.** `gold_friction_daily` (`sql/clickhouse/007_gold_collector.sql:157`) is the
old friction index, and it already computes three of ADR-0031's signals, beside their volumes:
`edit_rejection_rate`, `permission_wait_s_p50` and `permission_wait_s_p90`, and
`compactions_per_session` (with `auto_compactions` split out). Those are the right measurements,
computed the old way — per day and cohort, not within people.

**What is missing is the shape of the comparison, not the arithmetic.** ADR-0031 says the
comparison is computed **within the same people**, so that "champions are better engineers" does
not show up as the tool's effect. There is no within-person comparison anywhere: of the nine gold
views, none pairs a person's sessions that used an approach with that person's sessions that did
not. Every one of them aggregates across people. So a tool's number today can be a property of the
people who chose it, and nothing in the store can tell the difference.

The signals themselves, one by one, against ADR-0031's list:

| Signal | Where it stands |
|---|---|
| failed tool calls | `silver_tool_call` carries `ran` and `succeeded`; no gold view publishes them |
| retry loops of the same failing call | not built, and the data for it is not observed (below) |
| auto-mode denials | not built |
| compactions | built, in `gold_friction_daily` |
| churn | not built, and the least certain signal (below) |
| turns and cost per accepted change | `gold_tool_acceptance` has the accepted edits; the cost per accepted change is not joined to them |
| permission waits | built, in `gold_friction_daily` |

**What has to be decided first.** ADR-0031 says the family replaces the friction panels and that
`gold_friction_daily` gets rebuilt on it, but the rebuild has two open choices that the record does
not settle: whether the old view is replaced or kept beside the new one, and what the comparison
baseline is for a person who used an approach on some days and not on others. The second one is the
one that can be got wrong in a way that flatters a tool. A superseding record settles both, and
carries the preregistration the roadmap asks for: the claims, and the result that would prove each
one wrong, written down before the data is read.

**What closing it looks like.**

- A within-person comparison: for each approach, the same people's sessions that used it and that
  did not, published without a pseudonym ([INV-3](design/invariants.md)). This is the part that is
  missing today, and it is the part that decides whether a number means anything.
- A gold view per signal, each computed within people and published without a pseudonym
  ([INV-3](design/invariants.md)). Three of the seven exist and are recomputed in the new shape.
- Every rate beside its volume — ADR-0008's rule carries over unchanged, and the CI check that
  enforces it stays.
- No team breakdown. ADR-0031 removed it deliberately.
- The preregistration written down, before an organization's data is read.
- `gold_friction_daily` either rebuilt or retired, decided by the record above — not left as a third
  definition of the same thing.

**The check that would prove it closed:** a gold view per ADR-0031's list, with a test that reads
each rate and its volume, and a CI rule that fails a view publishing a rate without one.

**Watch for.** Two of the signals are not observable in the data Cardo has. Retry loops need the
order of `tool_result` events within a prompt; churn needs `lines_of_code.count` by type within a
session. ADR-0031 says so, and calls churn the least certain signal — a refactor also removes
lines. The first version reads only what is already collected, and says in its own output which
signals are missing rather than showing them as zero.

---

## 4. The collector's open door

**What the spec says.** [risks.md](../risks.md) #19: the collector accepts hooks and OTel from any
machine that can reach ports 4318 and 8088, and nothing authenticates the sender. Anyone on the
network can post invented events — push a home-grown artifact past the group floor, or add sessions
that never happened. The risk says it must be closed before Cardo serves a second team, and
`docs/roadmap.md` Step 3 marks it *Needs a decision, recorded in an ADR*.

**Why the obvious fix is not available.** A token in the settings file would be readable by every
engineer who has the file, so it would not be a secret
([ADR-0024](adr/0024-bundle-configures-telemetry-only.md)). The risk names three real options:
per-machine client certificates pushed by MDM (Claude Code supports `CLAUDE_CODE_CLIENT_CERT` for
its exporter), an address allowlist at the proxy, and accepting the risk in writing.

**What has to be decided.** Which of the three, and it is a decision, not an implementation task:
each option changes what an organization has to run and what an engineer has to carry.

**What closing it looks like.** A record that names the option, says what it costs the organization
and the engineer, and says why the other two were turned down. If the answer is "accept the risk in
writing", the record says so and the risk stays open with a name on it.

**The check that would prove it closed:** the ADR index shows the record, and its status line names
the outcome.

**Watch for.** This is one of the few items where doing nothing is a legitimate outcome, and it is
the owner's call. Do not build a mechanism before the decision.

---

## 5. Repository identity, read

**What the spec says.** [ADR-0035](adr/0035-repository-identity-classified.md): the collector
classifies every repository — the organization's are kept by owner and name, everything else becomes
`external` plus its host, and a session outside any repository is `none`. Point 4 adds the teams'
own instructions: **by default, presence only** — for each organization repository, do sessions load
project-level instructions, and how many files, with no names. An optional setting
`CARDO_INSTRUCTIONS_NAMES=project` keeps the names. `docs/roadmap.md` Step 3 marks this *Open*, and
`docs/design/architecture.md` says "collection is built (below), and no view reads it yet".

**What the code says.** The classification happens: `deploy/collector/config.yaml:142` sets
`cardo.repo`, and `:146` and `:147` set `cardo.repo.class` to `external` or `org`, deleting any
`cardo.repo*` key that arrived from the client first (`:141`). The collector's own test
(`TestCollector_RepositoriesAreClassified`) passes, and the live stack shows the attribute in the
bronze rows — 14 of them, classed `external` with the host kept and the path dropped.

**But no view reads it.** The words `cardo.repo` and `vcs.` appear in no file under `sql/`, so the
classification is computed, stored, and never used. `bronze_otel_logs`
(`sql/clickhouse/004_bronze_collector.sql:58`) keeps `ResourceAttributes` as a map, and the silver
layer never selects a repository out of it. So the answer to ADR-0035's question — which of the
organization's repositories have instructions files — cannot be produced from what is stored.

**Two separate gaps, and the second one is a mismatch with the record.**

- **The classification is not stored.** The collector's `set(resource.attributes["cardo.repo"], …)`
  puts it in the resource attributes, and the bronze table has a column for exactly that map. The
  silver layer simply never selects it.
- **The instructions-names setting does not exist.** ADR-0035 says names are kept only when
  `CARDO_INSTRUCTIONS_NAMES=project`, and that any other value means presence-only. The code does
  something else: `config.yaml:292` sets `instructions_name` from the file path, and `:294` deletes
  it unless it matches `CARDO_ORG_ARTIFACTS`. So today the names of the organization's instruction
  files are always kept, and the setting the record names is nowhere in the repository
  (`git grep CARDO_INSTRUCTIONS_NAMES` finds only the record itself). The record's default and the
  code's behaviour disagree, and the record is the one that is right about the intent: names are
  part of a path, and tier 1 carries no paths ([INV-5](design/invariants.md)).

**What closing it looks like.**

- A silver column for the repository's class and name, `org` or `external` plus host, or `none`,
  and the full URL never stored.
- A gold view answering ADR-0035's own question: which of the organization's repositories have
  instructions files, and how many — with no names by default.
- The setting `CARDO_INSTRUCTIONS_NAMES=project`, implemented as the record describes it, failing
  closed on any other value. A typo must not start keeping names.
- The engineer notice (item 2) states which way the organization set it, because ADR-0035 says it
  does.

**The check that would prove it closed:** a test that loads a repository in each class and asserts
what the view shows and does not show; a test that sets the setting to a typo and asserts no name is
kept.

**Watch for.** This is a change to what the collector stores, so the allowlist in
`test/collector_test.go` and the mandatory event set are in scope
([INV-4](design/invariants.md)). If a new field has to be stored, that is a record, not a pull
request. The repository name is an organization's own name and stays inside
[CARDO_ORG_REPOS](../deploy/compose/.env.example); everything else is `external` plus its host.

---

## 6. The catalog and the views, joined

**What the spec says.** [ADR-0045](adr/0045-catalog-storage.md) is the enablement catalog:
versioned files on disk, metadata in ClickHouse, validate → diff → draft → approve, retire or roll
back. [ADR-0040](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md) says spread is read by how
an artifact arrived: for one the organization rolled out, it is the rollout's reach, and whether it
helps is the efficiency family's question. `docs/roadmap.md` marks the joining *Needs a decision*:
"how the rollout list is kept and loaded".

**What the code says.** The catalog is built (`sql/clickhouse/010_catalog.sql`, the console's
Catalog page). The views are not joined to it: they decide what is the organization's own from
`CARDO_ORG_ARTIFACTS`, a regular expression in the environment. ADR-0045 says this in its own
words — "the two are not yet wired together, so an approved catalog version does not by itself make
a name appear as the organization's".

**Why it matters.** An organization that approved a file has said "this is ours". The views do not
know that, so the reach of a rolled-out artifact cannot be computed from the roll-out list — which
is the whole question ADR-0040 asks.

**What has to be decided.** How the rollout list is kept and loaded: whether the catalog's approved
versions are the list, whether the environment pattern stays as a second source, and which one wins
when they disagree. The record notes the tension with the group floor
([ADR-0029](adr/0029-minimum-group-size.md)), which is also read from the environment today.

**What closing it looks like.** The views read the organization's own artifacts from the catalog's
approved versions, the environment pattern stays or goes by the decision, and the spread of a
rolled-out artifact is its reach rather than a computed verdict.

**The check that would prove it closed:** approve a catalog version and see it named as the
organization's in the artifact view, under the group floor.

**Watch for.** ADR-0040's rule is that a spread view **ends in the adoption owner's decision, never
a computed verdict**. Joining the catalog does not change that: it makes the reach real, and the
verdict stays with the owner.

---

## 7. Policy evidence

**What the spec says.** [ADR-0037](adr/0037-policy-evidence.md), marked *Accepted — not built*.
Three things: conformance of the machines that report (no bypass-mode sessions, only approved MCP
servers, Claude Code at or above the minimum version, the managed hooks present), coverage (every
pseudonym active in Anthropic's usage data also reports telemetry), and sessions from other
organizations. Delivered as a periodic evidence report and as events to the organization's SIEM.
`docs/roadmap.md` marks it *Open*, and notes that for an organization that has not allowed Claude
Code yet, this comes first.

**What the code says.** There is a `silver_policy_decision` view
(`sql/clickhouse/006_silver_collector.sql:296`) that captures permission requests with their tool
name and time. There is **no gold view and no report**. `gold_instructions_versions`
(`sql/clickhouse/007_gold_collector.sql`) covers the versions of the organization's own instruction
files, which is one part of conformance and not the whole of it.

**Why it is harder than it looks.** ADR-0037 says the honest part out loud: "no violations seen" is
only as strong as the proof that every machine reports. Coverage is the hard half, and it needs the
Admin API path — the Console adapter that exists, and the Enterprise adapter that does not
([ADR-0021](adr/0021-analytics-source-scope.md)). The Admin API adapter has never run against a
live key, so this item is blocked on the very thing the guide lists as a gap.

**What closing it looks like.**

- A gold view per fact: conformance, coverage, other organizations' sessions.
- The report states what the evidence **cannot** see, as ADR-0037 requires. A report that only
  lists what it found is a false claim.
- Every exception is listed, not counted away.

**The check that would prove it closed:** the report renders, and its "cannot see" section is not
empty.

**Watch for.** The SIEM half is later, and only once the report exists
([ADR-0037](adr/0037-policy-evidence.md), named security exceptions come under their own identity
decision). Do not build the event stream before the report.

---

## 8. Team coverage, in the views

**What the spec says.** [ADR-0034](adr/0034-identity-in-pilots.md): teams from a directory file, so
that a rollout which missed two teams shows up. `docs/roadmap.md` marks the team part *Built* —
"the console accepts `email,team`, hashes it on load (ADR-0046), and shows coverage and the group
floor" — and notes that "a `cardo teams` CLI check remains".

**What the code says.** The console loads the directory and computes coverage
(`internal/launcher/server_teams.go:96`: `v.CoveragePct = float64(v.Mapped) / float64(v.Sessions7d) * 100`),
and `sql/clickhouse/011_teams.sql` holds `settings_teams` with `team`, `pseudonym`, `salt_version`
and `batch`. There is **no gold view for teams**, and `git grep "cardo teams"` finds the phrase only
in the roadmap's sentence saying it remains. `gold_fleet_adoption` exists and is about artifacts, not
teams.

**What closing it looks like.**

- A gold view for team reach against the whole organization — reach, never a ranking
  ([ADR-0030](adr/0030-stakeholders-and-questions.md)).
- A `cardo teams` command that reads the directory, hashes it, reports the coverage and the group
  floor, and writes nothing without being asked.
- The join and leave dates ADR-0034 asks for, if they are to be stored at all: the directory format
  is a decision, and `settings_teams` has no date column today.

**The check that would prove it closed:** load a directory with two teams and read the team's reach
from a view, against the fleet's.

**Watch for.** A team smaller than the group's minimum is `other`, not a team with a small number.
And teams are compared on reach, never ranked — nothing here divides by headcount.

---

## 9. The board report

**What the spec says.** `docs/roadmap.md` Step 3, marked *Waits for* the signs of struggle and for
data from more than one team: cost per accepted change, money spent redoing work, cost per approach,
each team's reach, and the same people's efficiency before and after they took up an approach.

**What the code says.** The pieces exist separately: `gold_cost_daily` (cost), `gold_tool_acceptance`
(accepted edits), `gold_fleet_adoption` (reach), and the efficiency family once item 3 is built.
What does not exist is the report that puts them together, and the join between delivery outcomes
and sessions — which does not exist at all, because delivery outcomes are not collected.

**What closing it looks like.** A report, and a delivery poller that reads the organization's own
repositories only, joined **by repository and week**, never per person or per session
([ADR-0031](adr/0031-what-works-means.md)). Every rate beside its volume.

**The check that would prove it closed:** the report renders for an organization with more than one
team, and no number in it is per person.

**Watch for.** This is the item with the most external surface: a credential that reads
repositories meets real resistance, and ADR-0031 says it does not come before a pilot. It also
waits on item 3, because the efficiency half of the report is the efficiency family.

---

## 10. The gaps in the guide

**What the spec says.** `docs/console-coach-history.md` §13 lists the known gaps and risks. Most are
facts about the data rather than work, and the guide says so. Two of the eight are work, and two
more are facts the guide has not noticed.

**What the code says.** The list, as it stands, and what kind of thing each one is:

| Gap | Kind | Status |
|---|---|---|
| 1. Cost is list price, not the invoice | a fact | closed as a fact; the guide says so |
| 2. Imported history has no tool calls | a fact | closed as a fact, but see below |
| 3. Three of 84 imported sessions have no prompts | a fact | closed as a fact |
| 4. The import sees one machine | work | open — export, import and removal, with their record |
| 5. The Admin API has never been called with a live key | blocked | needs an organization |
| 6. `pilot_mode` changes which installer steps appear | a fact | closed as a fact |
| 7. Coach link revocation is all-or-nothing | a fact | closed as a fact; rotating the token revokes every link |
| 8. The team path needs `cardo connect --project` per repository | work | open — user-level instrumentation, which needs consent because it edits `~/.claude/settings.json` for every project |

Plus two the guide does not list but the code has:

- **`Blocked` is declared and never filled.** The coach's `Window` struct has the field
  (`internal/coach/coach.go:23`) and nothing in the repository ever writes it — the two queries read
  turns and tool calls, and `git grep Blocked` finds the field, the board's installer text
  (`internal/launcher/web/app.js:437`, which is about a blocked install step, not about this field)
  and a design mock. The coach page shows Sessions, Prompts, Cost and Peak context
  (`internal/launcher/web/app.js:1488`), so the field is invisible today. Either the signal is
  computed and shown, or the field goes.
- **`FailedTools` counts a tool that ran and failed** — `countIf(ran = 1 AND succeeded = 0)`
  (`internal/coach/coach.go:104`) — but the coach page does not render it either, and the import
  path carries no tool decisions, so an imported session's failure count would be zero. That reads
  as "no failures" rather than "not observed". The guide's gap 2 says this for tool calls; the same
  caveat belongs on the failure count, and the field needs a home on the page before it is shown.

**What closing it looks like.** Gap 4 gets its own record and its own implementation — export, import
and removal, with the file its owner can read before sending, and the import replacing by session
because the raw tables have no key. Gap 8 gets a decision, because editing every engineer's
`~/.claude/settings.json` needs consent. Gaps 1, 2, 3, 6 and 7 stay in the guide as facts, which is
where facts belong. The two unlisted ones are fixed: the "Blocked" section either shows a real
number or is removed, and the imported-path caveat covers the failure count too.

**The check that would prove it closed:** a coach page for an imported session says "not observed"
where it has no data, and never "0 failures" as if it had looked.

---

## 11. Aggregates that outlive their rows

**What the spec says.** [ADR-0016](adr/0016-retention.md): derived aggregates that carry no
pseudonym are retained indefinitely, so longitudinal trends survive the expiry of the rows behind
them. `risks.md` #10 says this is promised and not built: the gold marts are views over bronze, so
a trend older than the retention window disappears with the rows behind it.

**What the code says.** Every gold mart is a view:
`grep -ho "CREATE OR REPLACE VIEW cardo\.[a-z_]*" sql/clickhouse/*.sql` returns nine gold views
and no materialized table. Bronze rows expire at the window in force (90 days by default,
[ADR-0053](adr/0053-retention-default-and-organization-choice.md)), and the views read the rows
that are there.

**What closing it looks like.** Materialized aggregates, refreshed on a schedule, carrying no
pseudonym, retained past the window. The refresh must be a `REPLACE PARTITION` on a table, not a
`DELETE`-then-`INSERT` — an empty day on a dashboard looks exactly like a quiet one.

**The check that would prove it closed:** set the window to 30 days, wait for a day to expire, and
read a trend that is older than 30 days and still there.

**Watch for.** The aggregate must carry no pseudonym, or it is tier 1 and expires with everything
else — which is the whole point of the item. And it must be built with the deletion path ADR-0016
implies: because the salt exists, a deletion request can be honoured, and the procedure must be
written down rather than improvised. An aggregate that cannot be deleted is worse than no
aggregate.

---

## What this page does not decide

- **Whether the owner's organization runs any of this.** Nothing here names an organization, and
  several items wait for one.
- **The order inside a step.** Only the order of the eleven is fixed here.
- **Anything about tier 2.** The private opt-in layer is not built and nothing of that kind is
  collected today ([ADR-0007](adr/0007-enrollment-posture.md)).
- **The board report's delivery source.** GitHub first, per ADR-0031, but a delivery tool the
  organization already runs is the alternative, and that is an organization's choice.
