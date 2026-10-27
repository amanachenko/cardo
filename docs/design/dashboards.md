# Dashboards — what each chart is for

> Edited in place; describes reality. **Status 2026-10-24:** two dashboards. The enablement
> dashboard was reviewed on the made-up organization on 2026-10-23 and rebuilt as the review left
> it. The fleet dashboard has not been reviewed: it waits for the cost data path, Anthropic's usage
> and cost data, which no organization's key has fetched yet and the made-up organization does not
> have.

Every chart on Cardo's two dashboards, with the question it answers, who it is for and what they
decide with it, what it reads, what it cannot show, and how far it has been checked. It is written
so that any chart can be challenged: a chart whose decision nobody makes should go, and a decision
with no chart is a gap. When a panel is added, renamed or removed, its entry here changes with it,
and `test/dashboards_doc_test.go` fails until it does.

## Who the charts are for

The people [who Cardo serves (ADR-0030)](../adr/0030-stakeholders-and-questions.md) names:

- **The adoption owner**, whoever is tasked with adopting Claude Code. They decide what becomes
  standard, and whether the cost is justified.
- **The operator**, who runs the adoption week to week, ships the organization's artifacts and
  reports up.
- **The champions**, who build the team-grown approaches, and whose case for making one official
  is that others chose it.

Engineers are not among them. A person's own figures belong on their own private page, which is not
built ([the coach by shared link, ADR-0032](../adr/0032-engineer-coach-by-shared-link.md)). Today
only the operator has a Grafana login, because any login can read every row the data source can
([risks.md](../../risks.md) #15), so "for the adoption owner" means what the operator shows them.

## How far each chart has been checked

Each entry ends with a status:

- **Built:** in the dashboard file. CI checks every query: it reads only gold views, names no
  pseudonym, and charts every rate beside its volume.
- **Reviewed on the made-up organization:** walked through chart by chart on
  [the made-up organization](../made-up-organization.md)'s twelve weeks, where every planted story
  is known, and kept, changed or dropped. It shows that a chart can show what was planted, not that
  it means anything with real people. The examples below come from there, and none is a finding.
- **Seen on an organization's data:** no chart yet.

## Rules every chart follows

**Weekly, Monday to Sunday, and a week shows once it is over.** Each query keeps the weeks from the
start of the range to the last complete week it reaches, and "last week" in a table means that
week. For a range ending on Saturday 24 October, last week is the week of 12 October. The
enablement dashboard opens on 90 days, so twelve weeks fit. The fleet dashboard is still daily.

**A team or a name is shown only when five people are in it, counted over the chart's own
period** ([the five-person minimum, ADR-0029](../adr/0029-minimum-group-size.md)). On a weekly
chart a team is named in the weeks at least five of its people worked, and otherwise counted as
"other". The same holds for a command, skill, subagent or connector the organization did not roll
out, and for a model, a Claude Code release or a connector's tool ("other models", "other
releases", "other tools"). The organization's own names, Claude Code's own tools and subagents,
and totals for the whole fleet are always shown. The five can be raised, never lowered.

Counting per day broke every team's line. On the made-up organization's twelve weeks, the data team,
eight people, was named on 39 of 84 days, and missing from every weekend. Counted per week, the
data team is named every week, web from week 5, its first week with five people working, and the
three-person security team never.

**People are never added up across weeks.** The same engineer in five weeks is one person, not
five. A table over a range shows last week's people, or the busiest week's. Sessions, requests and
cost add up.

**Every rate is charted beside its volume**
([what "works" means, ADR-0031](../adr/0031-what-works-means.md), carrying on
[ADR-0008](../adr/0008-outcome-variable.md)). Without one it rewards timidity. The pairs are:

- an artifact's share of people, with everyone active that week;
- a share of sessions (current, older, or of a file's sessions), with the sessions;
- the share of context read from the cache, and any figure per request, with the requests;
- on the fleet dashboard, the edit acceptance rate with the edits proposed.

`TestADR0008_RatesAreChartedWithTheirDenominator` fails a panel that charts one without the other.

**Teams are listed, never ranked, and nothing is divided by headcount**
([who Cardo serves, ADR-0030](../adr/0030-stakeholders-and-questions.md), decisions 4 and 7).
Teams appear alphabetically and are compared on reach, against the whole fleet. Anything about
where work gets stuck is shown by tool, rule or approach, never by team.

**Nobody's own row.** Every panel reads a gold view. None reads bronze or silver, the layers that
hold a pseudonym, and none names one (the no-per-person-view invariant, INV-3). CI fails a panel
that does.

**The markers are leads, not causes.** Every time chart on the enablement dashboard carries a
marker, "Organization's artifacts first used", at the first week each of the organization's
artifacts was used. A marker beside a change says what arrived that week, not what caused the
change. On the made-up organization, `perf-budget`'s marker sits in the week a Claude Code release
with a caching bug arrived, and the bug is what raised the spend. An artifact already in use when
the data begins has no marker. The marker is the first use, not the rollout date, which Cardo does
not know until the operator's rollout list exists.

## How an instructions file's version is read

Cardo never reads what a file says, so it reads a version from the file's name
([stale instructions by versioned name, ADR-0026](../adr/0026-stale-instructions-by-versioned-name.md)).
`example-engineering-standards-v3.md` is version 3 of `example-engineering-standards`. The
organization adopts this convention when it ships a file, for example a rule named
`.claude/rules/acme-security-v3.md`.

- **Current** is the highest version anyone had loaded by the end of that week. Before the review
  it was the highest ever loaded, so on the made-up organization the three weeks before v3 shipped
  read as entirely on an older version.
- **Older** is a session that loaded an earlier version: a machine that has not picked up the new
  file.
- A version shipped and not yet loaded by anybody cannot be seen.
- A file with no version in its name is listed without one, and is never counted as older.
- Only the organization's own files are named, those that match `CARDO_ORG_ARTIFACTS`. Other
  instructions files' names are dropped before they are stored.

The dashboard says the same in its "How versions are read" panel.

## Cardo — enablement

`dashboards/cardo-enablement.json`, over the collector's data: the hook pack and Claude Code's own
OTel. **Status:** reviewed on the made-up organization, 2026-10-23, and rebuilt as the review left
it, on the weekly views of `sql/clickhouse/015_gold_enablement_review.sql`. At the made-up
organization's 60 people it took about 70 seconds to draw while silver was plain views, which
recompute everything on every read ([risks.md](../../risks.md) #14). It has not been timed on
stored silver yet.

#### Data as of

- **Asks:** how old is what the other panels show?
- **For:** the operator, before reading anything else here.
- **Reads:** `gold_data_as_of`: the oldest of the seven stored silver tables' last good rebuilds,
  for example "2026-10-26 10:20 UTC", and `failing`, how many rebuilds failed on their last try.
  ClickHouse rebuilds the tables every 10 minutes
  ([ADR-0052](../adr/0052-silver-is-stored-and-rebuilt-every-10-minutes.md)), so a time older
  than that means a rebuild keeps failing, and every other panel shows the data as it was then.
- **Cannot show:** why a rebuild failed. ClickHouse's `system.view_refreshes` says, as
  [the stack's README](../../deploy/compose/README.md#the-dashboards-run-up-to-10-minutes-behind)
  shows.
- **Status:** added with stored silver, after the review.

### Artifacts: what the organization rolled out, and what engineers brought in

#### The organization's artifacts in use

- **Asks:** of everyone who used Claude Code last week, how many used each command, skill,
  subagent, connector and instructions file the organization rolled out?
- **For:** the operator, deciding what to announce again, train people on, or retire.
- **Reads:** `gold_artifact_usage`, the names that match `CARDO_ORG_ARTIFACTS`: people last week,
  everyone active that week, the share, and the first week used.
- **Cannot show:** who an artifact was rolled out to, so a skill installed for one team reads as a
  small share of the whole fleet. That comes with the operator's rollout list
  ([spread is a vote only for what teams chose, ADR-0040](../adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)).
  Nor whether the artifact helps.
- **On the made-up organization:** `perf-budget` went to the payments and data teams, 26 people.
  Last week 23 people used it, which reads as 43% of the 53 active, when nearly everyone it went to
  uses it.
- **Status:** reviewed on the made-up organization, 2026-10-23. Its three "peak" columns, which
  could come from three different weeks, became last week's figures, and its sessions and uses
  columns went, because no decision used them.

#### Weekly reach of the organization's artifacts

- **Asks:** did a rollout take, hold or fade?
- **For:** the operator and the adoption owner.
- **Reads:** `gold_artifact_usage`: the people who used each of the organization's artifacts each
  week, against everyone active that week, drawn as bars.
- **Cannot show:** whether people use it because it helps or because it is there. For an artifact
  the organization rolled out, spread is the rollout's reach, not a vote (ADR-0040).
- **On the made-up organization:** `/review` is first used in week 6, under a marker, and rises. In
  week 7 one person starts running it from a script dozens of times a day: its sessions go from 185
  to 523, and its people stay at 37, so its line does not jump.
- **Status:** reviewed on the made-up organization, 2026-10-23.

#### Brought in by engineers, used by several

- **Asks:** which commands, skills, subagents and connectors that the organization did not roll
  out are several people choosing, and across how many teams?
- **For:** the adoption owner and the champions, deciding what to make official. Several people
  choosing the same thing is the case for it, and spread across teams is the stronger vote
  (ADR-0040).
- **Reads:** `gold_artifact_usage`, the names that are neither the organization's nor Claude
  Code's own: how many teams and people used each last week, its people four weeks before, and the
  first week used. A blank means fewer than five people used it that week, if anyone did. The
  "other" rows stay: they show how much has not converged yet.
- **Cannot show:** which teams, only how many. Nor whether a name helps: that column comes with
  the signs of struggle (see [not built yet](#not-built-yet)).
- **On the made-up organization:** `/deploy-check`, which one engineer wrote in week 2, is "other"
  until week 5. Last week 19 people used it, in four teams.
- **Status:** reviewed on the made-up organization, 2026-10-23. It was called "built by
  engineers": the GitHub and Sentry connectors are brought in, not built.

### Shared instructions: which version people load

#### How versions are read

- **Asks:** nothing. A text panel that explains the versioned-name convention to whoever reads the
  row, as in [how an instructions file's version is read](#how-an-instructions-files-version-is-read).
- **For:** anyone reading the charts below it, who would otherwise not know why a file has a
  version, or why a file without one is never older.
- **Reads:** nothing.
- **Status:** added in the review on the made-up organization, 2026-10-23.

#### Versions in use

- **Asks:** has the new version reached people, and can the old one be removed?
- **For:** the operator.
- **Reads:** `gold_instructions_versions`: for each version of each of the organization's files,
  whether it was current last week, the people and sessions that loaded it that week, its share of
  the sessions that loaded the file, and the first week anyone loaded it.
- **Cannot show:** who is on an older version, only how many. A version nobody has loaded yet.
- **On the made-up organization:** last week v3 is current, loaded by 38 people, and v2 is older,
  loaded by 15: the mobile and web teams, who never picked v3 up.
- **Status:** reviewed on the made-up organization, 2026-10-23. "Current" is now judged per week.

#### Sessions on an older version

- **Asks:** after a new version ships, is the share of sessions still loading an older one
  falling towards zero?
- **For:** the operator, chasing the machines that never picked up the change.
- **Reads:** `gold_instructions_by_team_weekly`, added up over teams: the share of sessions that
  loaded an older version, with the sessions that loaded the file at all, drawn as bars.
- **Cannot show:** which machines, or why.
- **On the made-up organization:** none in weeks 1 to 3. v3 ships in week 4 and the share goes to
  28%, then stays between 29% and 43%, because two teams keep loading v2.
- **Status:** reviewed on the made-up organization, 2026-10-23. It was a count of sessions; it is
  now the share, with the count as its volume.

#### Each team on the current version, against the fleet

- **Asks:** where has the new version not landed?
- **For:** the operator and the adoption owner.
- **Reads:** `gold_instructions_by_team_weekly`: for each team, the share of its sessions that
  loaded the current version, beside the whole fleet's share, with each line's sessions.
- **Cannot show:** why a team has not picked it up. It ranks nobody: teams are listed
  alphabetically.
- **On the made-up organization:** mobile is at none from week 4, and web from week 5, its first
  week with five people working; before that its sessions count under "other".
- **Status:** added in the review on the made-up organization, 2026-10-23.

### Where work gets stuck

#### Failing calls repeated in a row

- **Asks:** which tool keeps failing, three or more times in a row, while the model runs it again
  and reads the failure each time?
- **For:** the operator and the champions, deciding what to fix once for everyone, such as a flaky
  test or a broken command.
- **Reads:** `gold_tool_loops_weekly`: each week and tool, the runs of three or more failed calls
  of one tool, one after another in a session, with no other call that ran in between. The tool's
  calls and the sessions it happened in are its volume, in the tooltip. Calls are ordered by when
  Claude Code decided to run them. A connector's tool fewer than five people called that week is
  "other tools".
- **Cannot show:** which test or command, because Claude Code does not send it and Cardo keeps no
  tool parameters (the no-content invariant, INV-5). Which repository comes later, once real
  sessions have shown how it arrives
  ([repository identity, ADR-0035](../adr/0035-repository-identity-classified.md)). Never which team
  or person.
- **On the made-up organization:** the mobile team's flaky test command. Bash has 48 to 119 such
  runs a week, 1,044 in twelve weeks, and every other tool 3 or fewer a week. Of the 1,317 planted,
  the other 273 failed twice and then passed: two in a row can be bad luck, so they are not counted.
- **Status:** added in the review on the made-up organization, 2026-10-23, by tool only for now.

### Spend and setup: where the money goes, and what moved it

#### Weekly spend by model

- **Asks:** what did Claude Code cost each week, and which model did the money go to?
- **For:** the adoption owner, telling a change of default from more work.
- **Reads:** `gold_model_spend_weekly`: the cost per week and model. A model fewer than five people
  used that week is "other models", because what one person spent on a model is that person's
  spend. The cost is Claude Code's own estimate for each request, not the invoice.
- **Cannot show:** who changed the default: read it beside spend by team. Nor whether the dearer
  model did better work.
- **On the made-up organization:** `claude-fable-5-1` appears in week 8, and weekly spend goes from
  about $10,000 to about $18,000.
- **Status:** reviewed on the made-up organization, 2026-10-23.

#### Weekly spend by team

- **Asks:** whose spend moved?
- **For:** the adoption owner, beside spend by model: a team whose spend steps up in the week a new
  model appears is the team that changed its default.
- **Reads:** `gold_spend_weekly`: the cost per week and team, stacked, teams listed alphabetically.
- **Cannot show:** whether a team spends well. Spend is never divided by headcount, so a large team
  spends more, and that is all it says.
- **On the made-up organization:** the platform team's spend rises about three times in week 8,
  the week `claude-fable-5-1` appears in spend by model.
- **Status:** reviewed on the made-up organization, 2026-10-23. It replaced daily spend by team.

#### Claude Code releases: how much of the context each reuses from cache

- **Asks:** does a Claude Code release reuse the cache less than the ones around it?
- **For:** the operator, deciding whether to raise the minimum version. Never a maximum: Cardo
  never stops an organization upgrading (INV-6).
- **Reads:** `gold_release_cache_weekly`, over the range, newest release first: the share of the
  context its requests read from the cache, the cache written per request, the cost per request,
  the requests they are measured over, the people in its busiest week, and the first and last week
  it was seen. A release fewer than five people used that week is "other releases".
- **Cannot show:** why a release reads less. Cost per request also moves with the model, so the
  cache columns come first.
- **On the made-up organization:** releases 9.0.33 to 9.0.39 read 0.87 of their context from the
  cache and the rest about 0.95, and they write about 12,000 tokens to the cache per request
  against about 4,800.
- **Status:** added in the review on the made-up organization, 2026-10-23.

#### Context every session starts with

- **Asks:** did a rollout add to what every session carries?
- **For:** the operator.
- **Reads:** `gold_setup_context_weekly`: the context of each session's first request, the median
  and 90th percentile across the fleet each week, with the sessions as bars. That is Claude Code's
  system prompt, the connectors' tools and the instructions files, plus the first prompt, and every
  request of the session carries it again.
- **Cannot show:** which artifact added it; the markers are leads. Nor dollars: what a token costs
  depends on the model and on the cache.
- **On the made-up organization:** the median is about 31,600 tokens in weeks 1 to 3, 35,600 in
  week 4, when v3 of the instructions file ships, and 37,300 in week 6, when a Jira connector is
  set up for everyone.
- **Status:** reviewed on the made-up organization, 2026-10-23. It was a line per team; the step is
  the fleet's.

#### Context per prompt

- **Asks:** does what the organization rolls out change how large conversations grow?
- **For:** the operator, while it is explored.
- **Reads:** `gold_context_weekly`: for each prompt, the largest context any of its main-thread
  requests sent; the weekly median by team, with the 90th percentile and the prompts it is measured
  over in the tooltip.
- **Cannot show:** a share of the context window, because Claude Code does not report the window's
  size. Nor whether a large context was a problem.
- **Status:** reviewed on the made-up organization, 2026-10-23, and kept to explore: the review
  proposed dropping it, and it stays to see whether adoption shapes it. No decision reads it yet.

#### Model switches

- **Asks:** what do switches of model in the middle of a session cost?
- **For:** the operator, while it is explored.
- **Reads:** `gold_context_weekly`, by team over the range: the switches, their measured cost (the
  next main-thread request on the new model: the cache it wrote and its whole cost), and Claude
  Code's own estimate beside it. The estimate assumes the whole context is written again; in the
  one switch observed in a real session it was 3.2 times the measured cost, so it is shown only
  beside the measurement.
- **Cannot show:** whether a switch was worth it.
- **On the made-up organization:** 1,702 switches cost $824 in twelve weeks, 0.5% of the spend.
- **Status:** reviewed on the made-up organization, 2026-10-23, and kept to explore, as context per
  prompt is. No decision reads it yet.

## Cardo — fleet adoption

`dashboards/cardo-fleet.json`, over the Admin API's data. **Status:** not reviewed; waits for the
cost data path. Its figures are daily, and it predates the rules above. Its descriptions say what
each chart shows, not who reads it, so its entries have no "For" line. Its review starts from
[the questions after them](#questions-for-the-fleet-dashboards-review).

### Fleet adoption

#### Engineers using Claude Code

- **Asks:** how many people used Claude Code each day?
- **Reads:** `gold_fleet_adoption`: distinct code names active that day, by source.
- **Cannot show:** who.
- **Status:** not reviewed; waits for the cost data path.

#### Sessions per active engineer

- **Asks:** are the people who use Claude Code leaning on it more, or are more people trying it
  once?
- **Reads:** `gold_fleet_adoption`: sessions divided by the people active that day.
- **Cannot show:** how the sessions spread. An average per head hides that people work in very
  different ways: on the made-up organization, someone with a thousand short sessions a month sits
  beside someone with twenty long ones.
- **Status:** not reviewed; waits for the cost data path.

#### Code movement

- **Asks:** how much code, how many commits and pull requests, went through Claude Code each day?
- **Reads:** `gold_fleet_adoption`: lines added and removed, commits and pull requests, as the API
  reports them, deliberately not folded into a score.
- **Cannot show:** whether any of it was useful. Lines of code are an activity measure.
- **Status:** not reviewed; waits for the cost data path.

### Cost

#### Daily spend

- **Asks:** what does Claude Code cost each day?
- **Reads:** `gold_cost_daily`: the cost in dollars, by source. The API reports cents; silver
  divides once.
- **Cannot show:** why it moved.
- **Status:** not reviewed; waits for the cost data path.

#### Spend per active engineer

- **Asks:** what does Claude Code cost per active person each day? Its description calls it the
  number to take into a capacity conversation.
- **Reads:** `gold_cost_daily`: the cost divided by the people active that day.
- **Cannot show:** where the money goes. An average per head hides that a few people spend much
  of it, as on the made-up organization, where it was planted.
- **Status:** not reviewed; waits for the cost data path.

#### Model mix by spend

- **Asks:** where does the money go, by model, over the range? For example, whether a model policy
  is in force.
- **Reads:** `gold_model_mix`: the cost per model, as a pie.
- **Cannot show:** when the mix changed.
- **Status:** not reviewed; waits for the cost data path.

#### Token mix by model

- **Asks:** how many billable tokens each model used each day, cache reads left out?
- **Reads:** `gold_model_mix`: input and output tokens per model and day.
- **Cannot show:** cost; the cache.
- **Status:** not reviewed; waits for the cost data path.

### Tool acceptance

#### Edit acceptance rate, with the volume it is measured over

- **Asks:** how many of the edits Claude Code proposed did people accept?
- **Reads:** `gold_tool_acceptance`: the acceptance rate, with the edits proposed as bars.
- **Cannot show:** much in a fleet that works in auto mode, where no person is asked about an edit,
  so nearly every edit counts as accepted. This is why the enablement dashboard dropped its edit
  rejection rate.
- **Status:** not reviewed; waits for the cost data path.

### Pipeline health

#### Fields the adapter does not model

- **Asks:** has the API started sending a field this build does not read? Empty is the expected
  state.
- **Reads:** `gold_schema_drift`: each field nobody models, with the rows it was in and when it was
  first and last seen. The raw payload kept it, so nothing is lost.
- **Status:** not reviewed; waits for the cost data path.

## Questions for the fleet dashboard's review

- **Two panels divide by headcount.** "Sessions per active engineer" and "Spend per active
  engineer" are figures per head, which
  [who Cardo serves (ADR-0030)](../adr/0030-stakeholders-and-questions.md) rules out in its seventh
  decision: "Cardo computes and shows no per-head ratio". [The privacy page](privacy.md) makes the
  same promise to engineers. Both panels predate that decision.
- **Who each chart is for, and what they decide with it.** No description says.
- **Daily or weekly.** The enablement dashboard went weekly because daily team figures broke at
  weekends. The fleet dashboard draws no team today.
- **Whether the board report's figures belong here:** cost per accepted change and per approach,
  each with its volume ([roadmap](../roadmap.md)), rather than spend over time alone.

## Views no panel reads

Both are kept, and the made-up organization's check still compares them with its answer key.

- **`gold_friction_daily`:** edit rejections, permission waits and compactions, per day and team.
- **`gold_context_daily`:** the context each session starts with, the context each prompt grows to,
  spend, and model switches, per day and team. The weekly views replace it on the dashboard.

## Dropped in the review, and why

- **Edit rejection rate.** Flat near 0.7% on the made-up organization. In auto mode no person is
  asked about an edit, so nothing is rejected, and a low rate reports a success that did not occur
  ([what "works" means, ADR-0031](../adr/0031-what-works-means.md)).
- **Permission waits.** The fleet's average hides the split between people in the default mode,
  who wait, and people in auto mode, who never do.
- **Compactions per session.** The average hides that a few long autonomous sessions make most of
  them.
- **Friction by team.** It compared teams on friction, which "teams are compared on reach, never
  ranked" rules out (ADR-0030, fourth decision), and read as a league table.
- **Daily spend by team.** Replaced by weekly spend by model and by team, which together say why
  spend moved.

## Not built yet

Each was asked for in the review and waits for something else:

- **Does it help:** beside each artifact, its sessions against the same people's other sessions:
  compactions, failed calls, cost per accepted edit. It comes with the signs of struggle
  ([roadmap](../roadmap.md)).
- **Out of those it went to,** and reach by team. They need the operator's rollout list (ADR-0040).
- **A warning that two things changed in the same week,** so that a reader does not credit a skill
  with a caching bug.
- **Loops left running overnight,** and what they cost.
- **Dollars for the context every session starts with:** an estimate that belongs with the board
  report's cost work.
- **Which repository a loop is in,** once real sessions have shown how the repository arrives.
