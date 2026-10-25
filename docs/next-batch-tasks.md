# The next batch: tasks

**Status:** proposal. No task here has been started. The specification each task implements is
[`docs/next-batch-spec.md`](next-batch-spec.md); this file says what to do, in what order, and what
"done" means.

**How to read a task.** Each one names the item it closes, the files it touches, the acceptance
that proves it closed, and what must be decided before the code is written. A task that says
"needs a decision" is not ready to start — the decision comes first, and it is a record
([the ADR process](adr/0000-adr-process.md)).

**One pull request each**, the way the [roadmap](roadmap.md) asks for. Say so in an issue or a draft
pull request first, so two people do not build the same thing.

**Nothing here names an organization**, and nothing here is about anyone's plans to adopt Cardo
([ADR-0038](adr/0038-public-under-a-collective-copyright.md)).

---

## The order

| # | Task | Closes | Ready to start | Size | State |
|---|---|---|---|---|---|
| T1 | The pilot rows of the launcher | item 1 | yes | small | done 2026-10-25 |
| T2 | The engineer notice | item 2 | after T1 | small | done 2026-10-25 |
| T3 | The efficiency family, decided | item 3 (part) | needs a decision | medium |
| T4 | The efficiency family, built | item 3 | after T3 | large |
| T5 | The collector's open door | item 4 | needs a decision | small |
| T6 | Repository identity, stored and read | item 5 | yes | medium |
| T7 | The catalog and the views, joined | item 6 | needs a decision | medium |
| T8 | Policy evidence | item 7 | partly blocked | large |
| T9 | Team coverage, in the views | item 8 | yes | medium | done 2026-10-25 |
| T10 | The gaps in the guide | item 10 | yes | small | partly done 2026-10-25 |
| T11 | Aggregates that outlive their rows | item 11 | yes | large |

T9 can move up: it is independent of everything except the group floor, which is already built.

---

## T1 — The pilot rows of the launcher

**Closes:** item 1. **Done on 2026-10-25.**

**What the work turned out to be.** The task said to make `grafana_ro` call `stepGrafanaRO`. There
was no such step: the design document's row 08, "Leaders' logins", is the `gold` step, and it was
already built. Only the dispatch case was a leftover, so it was removed rather than wired up.

The second thing the task did not foresee: the pilot rows were excluded from "install all" by the
`Planned` mark, not by anything that knew about the pilot. Taking that mark off — which is what
"built" means for the board — made them part of every install. The server now skips a pilot row
unless `pilot_mode` says the organization chose the team path, and the board's own path selector
agrees because it writes that setting.

**Touches:** `internal/launcher/steps.go` (the dispatch and the two rows), a new
`internal/launcher/step_bundle.go` and `internal/launcher/step_notice.go`,
`internal/launcher/settings.go` (the bundle for a named host), `internal/launcher/launcher.go`
(the `--pilot` path), `internal/launcher/server.go` (the pilot filter), `cmd/cardo/notice.go` (the
two commands).

**Do this:**

- Make `grafana_ro` call `stepGrafanaRO` — the same code the solo `gold` row already runs, not a
  second implementation.
- Make `bundle` write `deploy/managed-settings/managed-settings.json`'s content to a path the
  operator can hand to an MDM, with the collector's address inside the organization's network, and
  print that path. The file it writes must be `env` and `hooks` and nothing else, each hook waiting
  at most a second ([ADR-0039](adr/0039-hooks-wait-at-most-one-second.md),
  [ADR-0024](adr/0024-bundle-configures-telemetry-only.md)). The existing
  `test/bundle_test.go` pins that shape — the writer must produce a file that passes it.
- Make `notice` call T2's writer.
- Delete the placeholder in the dispatch. A step that is listed and returns "not implemented" is the
  failure this repo's own guidance warns about.
- Make `cardo up --pilot` run all three rows and print each row's check.

**Acceptance:**

- `cardo up --pilot` on a clean machine: three pilot rows pass, and none says "PILOT" and "not
  implemented".
- The read-only user is refused on `raw_*` and on per-person tables, proven by a live attempt, and
  the row says so.
- The written bundle passes `test/bundle_test.go`'s shape check, and the file contains the
  collector's address and nothing else.
- The notice file exists and carries the fields T2 names.
- `cardo up` without `--pilot` is unchanged, and running it twice changes nothing.

**Watch for:** the launcher writes a file; a human hands it to the MDM. Pushing managed settings to
real developer machines stays an owner-gated action.

---

## T2 — The engineer notice

**Closes:** item 2. **Done on 2026-10-25.**

**What the work turned out to be.** Three decisions the task did not settle, taken while building:

- **The salt is named, never printed.** The page says who holds it and which version is in force.
  The salt is the key that could reveal who is behind a code name.
- **A pattern that names the organization is reported as set, without its text.**
  `CARDO_ORG_REPOS` carries the organization's own name, and the page goes to every engineer.
- **The instructions-names line reports what the collector does, not a setting.** ADR-0035 names
  `CARDO_INSTRUCTIONS_NAMES=project` and the setting does not exist yet — that is T6. A page that
  claimed a setting was in force when it was not would be worse than no page, so the line reports
  the rule in force today (ADR-0026) and changes with the setting when it lands.

**Touches:** a new `internal/notice` package, `cmd/cardo/notice.go` (the command and the launcher's
step), `cmd/cardo/main.go` (the command table).

**Do this:**

- Add `cardo notice --format md` to the command table at `cmd/cardo/main.go:109`. It writes one page
  from the current `.env`.
- The page says, in this order: **who holds the salt** — the operator, or the security team in a
  pilot ([ADR-0034](adr/0034-identity-in-pilots.md)); **which settings are on**, read from the
  settings file rather than hardcoded; **the group size**, which is the organization's own
  ([ADR-0029](adr/0029-minimum-group-size.md)); **which way the instructions-names setting is set**
  ([ADR-0035](adr/0035-repository-identity-classified.md) says the notice states it — and the
  setting does not exist yet, so this field arrives with T6); and **that the data is not for
  individual performance decisions**.
- Plain words, no legal language, the same terms as the settings file.

**Acceptance:**

- `cardo notice --format md` writes a page containing every required field.
- A test reads the rendered page and asserts each field is present. The test fails when a field is
  missing, the way `test/bundle_test.go` fails on a bundle shape change.
- The page contains no email address and no pseudonym. A test asserts that.
- The page names the salt custody as the operator by default, and as the security team when the
  pilot is set.

**Watch for:** this is a document an organization hands out. Everything committed is public, so the
page describes the settings file and never the people in it.

---

## T3 — The efficiency family, decided

**Closes:** the decision half of item 3. **No code.**

**Touches:** a new record in `docs/adr/`, and the index at `docs/adr/README.md`.

**Do this.** Write a superseding record that settles what ADR-0031 left open, and carries the
preregistration ADR-0031 requires:

- **The within-person comparison, in detail.** For each approach: which of a person's sessions count
  as "used it" and which as "did not", and what happens to a person who used it on every session they
  had. The comparison is computed per person inside the views and published without a pseudonym
  ([INV-3](design/invariants.md)). ADR-0031 already says the comparison is per person and pooled in
  gold; the record says how.
- **The baseline choice.** The same people, before and after — or the same people, with and
  without. This is the choice that can be got wrong in a way that flatters a tool, so it is named
  explicitly.
- **The fate of `gold_friction_daily`**: rebuilt on the family, or retired beside it. Not left as a
  third definition of the same thing.
- **The preregistration**: each claim to be tested, written down, with the result that would prove
  it wrong, before an organization's data is read. ADR-0031 requires this and
  `docs/roadmap.md` marks it *Open*.
- **The two signals whose data is not observed.** Retry loops need the order of `tool_result` events
  within a prompt; churn needs `lines_of_code.count` by type within a session. The record says the
  first version reads only what is collected, and that a missing signal is published as missing, not
  as zero.

**Acceptance:**

- The record exists, with Context, Decision, Consequences and **Rejected alternatives and why**.
- The index in `docs/adr/README.md` names it, and its status line says `Accepted`.
- The preregistration is in the record or in a file the record points at, and every claim has the
  result that would disprove it.
- No accepted record's body was edited. ADR-0031 keeps its body and its status line changes only.

**Watch for:** ADR-0031's own note that churn is the least certain signal — a refactor also removes
lines. The record says how a claim about churn would be told apart from a refactor.

---

## T4 — The efficiency family, built

**Closes:** item 3.

**Touches:** a new numbered migration in `sql/clickhouse/`, `internal/store/clickhouse/views_test.go`
(the gold views' tests), `test/deploy_test.go` (the dashboard queries), and `docs/design/data-model.md`.

**Do this:**

- Write the within-person comparison as a gold view, following T3's record.
- Recompute the three signals that exist — compactions, permission waits, edit rejections — in the
  new shape, beside their volumes.
- Add the signals that are observable: failed tool calls from `silver_tool_call`'s `ran` and
  `succeeded`, and the cost per accepted change joined to `gold_tool_acceptance`'s accepted edits.
- Leave out the signals whose data is not observed, and say so in the view's own output rather than
  showing them as zero.
- Every rate beside its volume ([ADR-0008](adr/0008-outcome-variable.md) carries over unchanged),
  and the CI check that enforces it stays.
- No team breakdown, and no pseudonym in a published row.

**Acceptance:**

- A gold view per signal in T3's record, each with a test that reads it against the seeded data.
- A test that reads each rate and its volume, and fails when a rate has no volume beside it.
- A test that a published row carries no pseudonym.
- `make stack-up && make clickhouse-test` passes against the real server, with `-count=1`.
- The preregistration in T3 is honoured: nothing was published before the claims were written down.

**Watch for:** a view over bronze recomputes everything on every read (risks.md #14). The
within-person comparison is more expensive than the old one, so this task is where that risk gets
measured, not where it gets discovered.

---

## T5 — The collector's open door

**Closes:** item 4. **A decision, not code.**

**Touches:** a new record in `docs/adr/`, and `risks.md` #19.

**Do this.** Decide what protects the collector from invented events, and write it down. The risk
names three real options and says a token in the settings file is not one of them (every engineer
with the file can read it, so it is not a secret —
[ADR-0024](adr/0024-bundle-configures-telemetry-only.md)):

- **Per-machine client certificates**, pushed by MDM. Claude Code supports `CLAUDE_CODE_CLIENT_CERT`
  for its exporter.
- **An address allowlist at the proxy.**
- **Accepting the risk in writing**, with a name on it.

The record says which one, what it costs the organization and the engineer, and why the other two
were turned down. If the answer is the third, the risk stays open in `risks.md` and says so.

**Acceptance:**

- The record exists, with the rejected alternatives and why.
- `risks.md` #19 names the decision, or keeps the risk open with the decision recorded inside it.
- The index in `docs/adr/README.md` names it.

**Watch for:** doing nothing is a legitimate outcome here, and it is the owner's call. Do not build
a mechanism before the decision.

---

## T6 — Repository identity, stored and read

**Closes:** item 5.

**Touches:** `deploy/collector/config.yaml`, a new numbered migration in `sql/clickhouse/`,
`internal/store/clickhouse/views_test.go`, `deploy/compose/.env.example`, the notice in T2, and
`docs/design/data-model.md`.

**Do this:**

- **Read the classification.** The collector already computes and stores it — `cardo.repo` and
  `cardo.repo.class` sit in the bronze rows' `ResourceAttributes` map
  (`deploy/collector/config.yaml:142`, `:146`, `:147`; the live stack shows them classed `external`
  with the host kept and the path dropped). Add a silver column for the repository's class and
  name: `org` with its owner and name, `external` with its host, or `none`. The full URL is never
  stored. Today no view reads it — the words `cardo.repo` appear in no file under `sql/`.
- **Add the setting ADR-0035 names.** `CARDO_INSTRUCTIONS_NAMES=project` keeps the names of
  project-level instruction files; any other value means presence-only, and a typo must fail closed.
  Today the code does something else: it keeps the names of the organization's files unconditionally
  (`config.yaml:292`) and filters them by `CARDO_ORG_ARTIFACTS` (`:294`). A name is part of a path,
  and tier 1 carries no paths ([INV-5](design/invariants.md)), so the record's default is the one
  that holds.
- **Publish the view.** A gold view answering ADR-0035's own question: which of the organization's
  repositories have instructions files, and how many — with no names by default.
- **Say it in the notice.** The notice states which way the organization set the setting.

**Acceptance:**

- A test loads a repository in each class and asserts what the view shows and does not show: the
  organization's repositories by owner and name, everything else as `external` plus its host, and no
  full URL anywhere.
- A test sets `CARDO_INSTRUCTIONS_NAMES` to a typo and asserts no file name is kept.
- A test sets it to `project` and asserts the organization's project-level names are kept, and that
  a person's own file names never are.
- `make collector-test` passes with the collector's own `CARDO_SALT` exported, and the collector
  restarted so its container and the test read the same environment.
- `make stack-up && make clickhouse-test` passes with `-count=1`.

**Watch for:** this changes what the collector stores. If a new field has to be stored, that is a
record, not a pull request ([INV-4](design/invariants.md)) — check with T3's discipline.

---

## T7 — The catalog and the views, joined

**Closes:** item 6.

**Touches:** a new numbered migration in `sql/clickhouse/`, `internal/launcher/` (the settings the
console writes), `deploy/compose/.env.example`, `docs/design/data-model.md`.

**Do this.**

- **Decide how the rollout list is kept and loaded** — the decision the roadmap marks as open. The
  candidates: the catalog's approved versions are the list; the environment pattern stays as a
  second source; or one supersedes the other. The record says which, and what happens when the two
  disagree.
- Make the views read the organization's own artifacts from the catalog's approved versions.
- Keep the group floor where it belongs: read from the settings, and never a literal
  ([ADR-0029](adr/0029-minimum-group-size.md)).

**Acceptance:**

- Approve a catalog version, and see it named as the organization's in the artifact view, under the
  group floor.
- A test approves a version and asserts the view names it; a test with one person using it asserts
  the view says `other`.
- The spread of a rolled-out artifact reads as its reach. It does not end in a computed verdict —
  that is the adoption owner's decision ([ADR-0040](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)).

**Watch for:** the catalog and the environment pattern are two sources of truth if both stay. That
is the shape that produced the retention defect in the last change: a write landing on one and a
reader elsewhere reading the other. Pick one place and make every reader read it.

---

## T8 — Policy evidence

**Closes:** item 7.

**Touches:** a new numbered migration in `sql/clickhouse/`, a new report surface in the console or
as a file, `internal/store/clickhouse/views_test.go`, `docs/design/data-model.md`.

**Do this:**

- A gold view per fact: **conformance** of the machines that report — no bypass-mode sessions, only
  approved MCP servers, Claude Code at or above the minimum version, the managed hooks present;
  **coverage** — every pseudonym active in Anthropic's usage data also reports telemetry; and
  **sessions from other organizations**, by `organization.id`.
- The report states what the evidence **cannot** see. ADR-0037 requires it, and it is the part that
  makes the rest true: "no violations seen" is only as strong as the proof that every machine
  reports.
- Every exception is listed, not counted away.

**Acceptance:**

- The report renders, and its "cannot see" section is not empty.
- A test asserts the exceptions are listed individually.
- A test asserts the coverage view names the gap: a pseudonym in the usage data that reports nothing
  is a gap, not a rounding error.
- The conformance view reads the managed settings' own rules (deny rules,
  `disableBypassPermissionsMode`, the approved MCP server list) and does not claim more than it
  checked.

**Watch for:** coverage needs the Admin API path, and the Admin API adapter has never run against a
live key (guide §13, gap 5). The conformance half is buildable now; the coverage half is blocked on
a live key and on an organization. Say which half is done rather than presenting a partial report
as the whole. The SIEM half comes later, once the report exists.

---

## T9 — Team coverage, in the views

**Closes:** item 8. **Done on 2026-10-25**, except the dates question, which is a decision and is
still open.

**What the work turned out to be.** Two things the task did not foresee.

- **The join cannot live in the gold view.** Putting a session in its team needs the pseudonym on
  both sides, and INV-3 forbids a gold view from comparing one — a per-person row is a ranking with
  one rename to go. The join therefore lives in the silver layer, `silver_team_artifact_use`, and
  the gold view counts and publishes. The view a dashboard reads still carries no pseudonym.
- **The console and the CLI were writing the mapping through two different implementations.** The
  console had its own; the command had none. Both now go through `launcher.WriteTeams`, so the two
  cannot differ in what they store.

**Touches:** a new numbered migration in `sql/clickhouse/` (`014_team_reach.sql`),
`internal/launcher/teams.go` (the shared write), `internal/launcher/server_teams.go` (now uses it),
`cmd/cardo/teams.go` (the command), `internal/store/clickhouse/team_reach_test.go` (the view against
a live server), `internal/launcher/teams_test.go`, `docs/design/data-model.md`.

**Do this:**

- A gold view for **team reach against the whole organization** — reach, never a ranking
  ([ADR-0030](adr/0030-stakeholders-and-questions.md)). Nothing divided by headcount, no league
  table.
- A `cardo teams` command: it reads the directory, hashes it, reports the coverage and the group
  floor, and writes nothing unless asked. The roadmap says this check remains.
- **Still open — a decision, not code:** whether join and leave dates are stored.
  [ADR-0034](adr/0034-identity-in-pilots.md) asks for the dates, so that last quarter's numbers
  stay on last quarter's team; [ADR-0046](adr/0046-hashed-team-directory.md) settled the file as
  `email,team` without them, and `settings_teams` has no date column today
  (`sql/clickhouse/011_teams.sql:18`). Two accepted records disagree, so this needs a record of its
  own. **The view reads the current directory**, which means a person's history is attributed to the
  team they are in now.

**Acceptance:**

- Load a directory with two teams and read each team's reach from a view, against the fleet's.
- A test with a team of one person asserts the view shows `other`, not a team of one.
- `cardo teams` prints the coverage and the group floor and writes nothing.
- Nothing in the view ranks the teams.

**Watch for:** a team smaller than the group's minimum is not rendered at all — not even as
`other`, because "1 person" is a person. The floor can be raised, never lowered.

---

## T10 — The gaps in the guide

**Closes:** item 10. **Partly done on 2026-10-25**: the two code fixes are in; gaps 4 and 8 are
still open, and each needs a decision.

**What the work turned out to be.** The `Blocked` field had no home to go to. The coach page renders
Sessions, Prompts, Cost and Peak context, and no page ever showed it — so there was nothing to
compute it for, and the field was deleted rather than invented into existence. The failure count
needed the opposite: it was already shown, and it was showing a number where there was nothing to
look at. Imported rows carry `cohort='imported'` from the moment they are written, so the window
knows whether it holds live telemetry at all.

**Touches:** `internal/coach/coach.go` (the field and the observation), `cmd/cardo/coach.go` (what
the CLI prints), `internal/coach/observed_test.go`, `docs/console-coach-history.md` §4.2 and §13.

**Do this:**

- ~~**Fix the `Blocked` field.**~~ **Done:** deleted. It was declared on the window and no query
  ever wrote it, and no page ever showed it — a number that looks like a measurement and is not one.
- ~~**Say "not observed" instead of "zero failures" for imported sessions.**~~ **Done:** the window
  carries `ToolsObserved`, and the coach prints "not observed" for a window with no live sessions.
  An imported transcript carries no tool decisions, so the figures were never looked for.
- **Gap 4 — the import sees one machine.** Export, import and removal, with their own record: the
  file its owner can read before sending, with no repository names and no organization id, and the
  import replacing by session because the raw tables have no key, so a second import does not double
  every count.
- **Gap 8 — the team path.** `cardo connect --project` per repository is the current path;
  user-level instrumentation is the alternative and needs consent, because it edits
  `~/.claude/settings.json` for every project. That is a decision.

**Acceptance:**

- A coach page for an imported session says "not observed" where it has no data, and never "0
  failures" as if it had looked.
- The `Blocked` field is either computed and shown on the page, or gone. A test covers whichever it
  is, so it cannot come back as a field that looks like a measurement and is not one.
- The guide's §13 says which half of the gap list is closed and which is not.

**Watch for:** the import reads transcripts, which is the one exception to
[INV-1](design/invariants.md) ([ADR-0050](adr/0050-local-transcript-import.md)). An export file is
the same thing travelling further, so it keeps counts and throws the text away, and it is readable
by its owner before it is sent.

---

## T11 — Aggregates that outlive their rows

**Closes:** item 11.

**Touches:** a new numbered migration in `sql/clickhouse/`, the poller in `internal/poll/`,
`internal/store/clickhouse/`, `docs/design/data-model.md`, `risks.md` #10.

**Do this:**

- Materialize the aggregates, refreshed on a schedule, carrying **no pseudonym**, retained past the
  retention window ([ADR-0016](adr/0016-retention.md)).
- Refresh with `REPLACE PARTITION`, not delete-then-insert: an empty day on a dashboard looks exactly
  like a quiet one.
- Keep the aggregates inside the tier rules: no pseudonym, no content, no paths
  ([INV-5](design/invariants.md)).
- Write down the deletion path. Because the salt exists, a deletion request can be honoured, and
  ADR-0016 says the procedure must be written down rather than improvised. **An aggregate that
  cannot be deleted is worse than no aggregate.**
- Say what happens to the aggregate's own storage: it has no pseudonym, so it does not expire with
  the rows, and that is a privacy decision with a name on it, not an accident.

**Acceptance:**

- Set the window to 30 days, wait for a day to expire, and read a trend older than 30 days that is
  still there.
- A test asserts the materialized table carries no pseudonym and no tier-1 content.
- A test proves the refresh is a `REPLACE PARTITION`: a refresh that fails mid-way leaves the
  previous day's numbers in place, not an empty day.
- The deletion procedure is written down and names what it removes.

**Watch for:** this is the item with the sharpest privacy edge. The aggregate outlives the rows by
design, so it outlives the salt rotation too — the record has to say what happens then.

---

## What is not a task here

- **Anything about an organization.** Several items wait for one, and the wait is named in the
  task, not hidden inside it.
- **The board report** (item 9) is not in this list. It waits on T4 and on data from more than one
  team, and its own spec is in the specification file.
- **Tier 2**, the private opt-in layer. Not built, and nothing of that kind is collected today
  ([ADR-0007](adr/0007-enrollment-posture.md)).
- **The delivery poller** behind the board report. It needs a credential that reads the
  organization's repositories, and ADR-0031 says it does not come before a pilot.
- **Anything that would make a manager able to rank their reports.** [INV-3](design/invariants.md)
  is not a trade-off to be made for a feature.
