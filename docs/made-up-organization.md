# A made-up organization

*Cardo's views on an organization's worth of invented data: how to run it, what was planted in it,
and what no view shows yet. For anyone changing a view or a dashboard.*

---

## In one paragraph

Cardo's views are built for an organization. A command a few engineers wrote is named once five
people use it, and each team's spend is set beside the others'. One person's sessions cannot show
any of that working: almost everything falls under the five-person minimum and shows as "other".
The made-up organization is 60 invented engineers in six teams, with twelve weeks of sessions. They
go through Cardo's own collector into a stack of their own. Stories are planted in it, such as a
command that spreads from one team to four, and an answer key records what was planted, so each
view can be checked against it.

## Nothing seen in it is a finding

It shows that a view shows what was put in, and nothing more. For example, on the made-up
organization, sessions that use `/deploy-check` cost 30% less per accepted edit. They do because
the generator was told to make them. Whether any signal means something with real people waits for
an organization's own data.

So nothing from it goes into `docs/research/`, or into a document, commit or pull request, as
evidence about Claude Code, engineers or organizations
([the decision to build it, ADR-0051](adr/0051-a-made-up-organization.md)). Its figures, such as
how spend spreads across people, are plausible defaults, not measurements.

What it can tell is about Cardo itself:
- whether a view shows the story aimed at it;
- whether a name stays hidden while fewer than five people use it;
- how fast the views answer.

## Run it

You need Docker with compose, curl, Go 1.23 or later, and bash (Git Bash on Windows). From the
repository root:

```bash
bash scripts/made-up.sh up      # build this checkout and start the made-up stack
bash scripts/made-up.sh load    # invent the organization and send it through the made-up collector
bash scripts/made-up.sh check   # compare the views with the answer key, and time every dashboard query
bash scripts/made-up.sh down    # remove the stack, with its data and its answer key
```

- **`up`** builds Cardo from your checkout and starts the stack. It writes fresh passwords and a
  fresh salt to `deploy/compose/.env.made-up`. Grafana is at <http://127.0.0.1:3003>: sign in as
  `admin`, with `GRAFANA_PASSWORD` from that file.
- **`load`** invents the organization from a seed and sends it to the made-up collector, as many
  copies of Claude Code would. It refuses a stack that already holds a load, which would count every
  session twice. It ends by comparing the rows it sent with the rows stored. It writes the answer
  key to `deploy/compose/made-up/loaded/answer-key.json`.
  - It passes its flags on to the generator: `-preset` for the size (below), `-seed 2` for a
    different organization of the same size, `-weeks 4` for a shorter history.
  - The same seed, size and start give the same organization every time. The history ends with
    last week, so a load on another day moves the dates, not the stories.
  - Except on a Sunday, by UTC. That day, the first day of a twelve-week history is already 90
    days old, past the stack's expiry for raw rows, so it is deleted as it arrives, and `load` ends
    by saying it stored fewer rows than it sent. Load on another day, or pass `-weeks 11`.
- **`check`** checks the views and the dashboards, and prints each story, where it should show, and
  the gaps: the list to read the dashboards against. See [what check checks](#what-check-checks).
- **`probe`** sends one hook event and one OTel record dated 30 days ago, and checks that both kept
  their date. It tests the one thing this stack does differently from every other.

To read an answer key without a stack, run the generator on its own. For example,
`go run ./cmd/cardo-madeup -dry-run -preset ci -key ~/answer-key-ci.json` writes the CI size's key
and sends nothing. Keep the key outside the checkout, so it is never committed.

The stack sits on loopback ports of its own, beside the stack that collects and the preview:

| | Grafana | ClickHouse | OTLP | Hooks |
|---|---|---|---|---|
| The made-up stack | 3003 | 8126 | 4320 | 8090 |
| The stack that collects | 3001 | 8124 | 4319 | 8089 |
| The preview | 3002 | 8125 | none | none |

## The three sizes

| `-preset` | People | Teams | Weeks | For | A load takes |
|---|---|---|---|---|---|
| `review` (the default) | 60 | 6 | 12 | looking at the dashboards | about 16 minutes |
| `ci` | 40 | 5 | 8 | a check short enough for a CI job. No CI job runs it yet | about 8 minutes |
| `large` | 500 | 12 | 12 | how long a load takes, and how fast the views answer | about two hours |

- **The collector sets the pace, not the generator.** On one machine it stores about 640 hook
  events a second, and the generator retries what it turns away. The large size is 4.7 million
  hook events, about 270 times faster than its 500 people would send them while they work.
- **The CI size has every story on a shorter timeline.** For example, `/deploy-check` is named in
  week 3 rather than week 5. Each answer key gives its own size's weeks and teams.
- **The large size shares the review size's timeline,** and some stories read differently among
  500 people. For example, one person's script makes a tenth of `/review`'s sessions rather than
  most of them. Only the review and CI sizes are held to their stories' text.

## Who the people are

People are invented as ways of working, not as one average engineer with some noise. Each person's
monthly spend and session count are drawn separately, so they do not move together: someone with
20 long sessions a month can spend more than someone with a thousand short ones.

| Way of working | Of every 60 | Spend a month | Sessions a month |
|---|---|---|---|
| Steady: a few sessions a day in an editor, answering permission prompts | 22 | $150 to $900 | 40 to 150 |
| Long autonomous runs, for hours, in auto mode | 8 | $800 to $4,000 | 15 to 60 |
| Many short sessions, a new one for every small question | 6 | $100 to $1,500 | 400 to 2,000 |
| Several sessions at once, one per worktree | 5 | $1,000 to $4,000 | 150 to 500 |
| Run by a script on the engineer's laptop, one prompt each | 4 | $100 to $2,000 | 300 to 1,500 |
| Most prompts handed to subagents | 7 | $400 to $2,500 | 30 to 120 |
| Occasional: a day or two a week, mostly reading code | 8 | $100 to $250 | 4 to 20 |

- **Teams, in the review size:** payments 18, platform 14, mobile 11, data 8, web 6 and security 3,
  in five time zones.
- **People come and go.** About one in seven starts using Claude Code some weeks in. About one week
  in fifteen is a holiday. Most people run a new Claude Code release within a day, and a few take
  up to three weeks.
- **Every address is at `example.com`**, such as `engineer-007@example.com`, and the collector
  turns it into a code name as it would a real one.
- **Every figure is a placeholder,** set in `internal/madeup/calibration.go` and nowhere else. That
  includes these ranges, the model prices and how quickly people upgrade.

## The stories

Each story is a pattern planted on purpose and aimed at a view. These are the review size's weeks.
`check` prints them for the size that was loaded, from its answer key.

| Week | Planted | Where it shows |
|---|---|---|
| 2 | `/deploy-check`, a command one engineer wrote, starts in one team. It crosses five people in week 5 and reaches four teams by week 12. | Enablement, "Brought in by engineers, used by several": "other" until week 5, then named, in four teams last week, with more people than four weeks before |
| 4 | The organization ships `example-engineering-standards-v3.md`, about a fifth more context for every session that loads it. The mobile and web teams keep loading v2. | "Versions in use": v3 current, v2 older. "Sessions on an older version": none until week 3, then mobile's and web's sessions. "Each team on the current version, against the fleet": mobile at none from week 4, and web from week 5, its first week with five people working. "Context every session starts with": a step up in week 4 |
| 6 | `/review`, the organization's own command, is rolled out to every team but web and security. Sessions that use it are no different from those that do not. | "Weekly reach of the organization's artifacts": `/review` rises, and a marker on every time chart says it was first used that week |
| 6 | A Jira connector is set up for everyone, and about three people call it each week. | Its reach stays at a few people, while "Context every session starts with" steps up, because every session carries its tools |
| 6 | The `api-guidelines` skill is set up for everyone, and the model rarely uses it. | Its reach: a few people a week |
| 7 | One person in payments runs `/review` from a script, dozens of times a working day. | "Weekly reach of the organization's artifacts" counts people, not sessions, so `/review`'s reach does not jump |
| 8 | The platform team makes the most expensive model its default, and its spend for the same work rises about three times. | "Weekly spend by team": platform's spend rises. "Weekly spend by model": `claude-fable-5-1` appears the same week, which says why |
| 10 and 11 | Claude Code releases with a caching bug raise cost per request. A release early in week 12 fixes it, and people upgrade at their own pace. | "Claude Code releases: how much of the context each reuses from cache": the releases with the bug read less of their context from the cache than the rest |
| 10 | `perf-budget`, a skill of the organization's, is rolled out to payments and data in the week the bug arrives. It changes nothing. | A marker for its first week sits beside the rise in spend, and nothing says the bug arrived the same week: see the gaps |

Throughout the twelve weeks:
- **Ways of working,** as above, and **a few people spending much of the money.** No view shows one
  person's spend to anyone else, by design.
- **Compactions differ by way of working:** dozens in a long autonomous run, almost none in a quick
  question. No panel shows them now: see the gaps.
- **People in the default permission mode answer prompts; people in auto mode answer none.** No
  panel shows them now: see the gaps.
- **The mobile team's test command fails now and then,** and sessions run it three to seven times
  in a row until it passes. "Failing calls repeated in a row" shows Bash's loops every week, far
  above every other tool's.
- **Two people leave a long loop running overnight,** a few nights a week.
- **Sessions that use `/deploy-check` cost about 30% less per accepted edit** than the same people's
  other sessions. Sessions that use `/review` cost the same as those that do not.
- **What must never be named:** the three-person security team, a command four people use, and
  personal commands, subagents and connectors no more than two people use. Every view shows them as
  "other", every week ([the five-person minimum, ADR-0029](adr/0029-minimum-group-size.md)).

## What no view shows yet

A story no view can show is written down as a gap in Cardo, not left out. These are candidates for
what to build, not a plan.

- **Which repository a loop is in.** "Failing calls repeated in a row" says which tool, not which
  command or repository. Claude Code does not send the command, and the repository waits until real
  sessions have confirmed how it arrives
  ([the repository decision, ADR-0035](adr/0035-repository-identity-classified.md)). By design the
  chart never says which team.
- **How compactions spread across sessions.** The dashboard's review dropped the fleet's daily
  average, which hid that a few long sessions make most of them. `gold_friction_daily` still counts
  them.
- **Permission waits.** The review dropped the chart: in a fleet that works in auto mode almost
  nobody waits, so it reported a success that did not occur
  ([what "works" means, ADR-0031](adr/0031-what-works-means.md)). `gold_friction_daily` still
  counts them.
- **Sessions that run while nobody is there,** and what they cost.
- **Sessions that used an artifact set beside those that did not.** Neither `/deploy-check`'s
  difference nor `/review`'s absence of one can be seen.
- **A warning that two things changed in the same week.** A marker says perf-budget was first used
  in the week spend rises. Only the releases table, read separately, shows the caching bug, so a
  reader credits the skill with the bug.
- **One person's spend over a budget.** The view of cost over a published budget
  ([ADR-0033](adr/0033-per-person-cost-for-budget-owners.md)) is not built. The fleet dashboard's
  spend per engineer reads the Admin API's cost data, which this stack does not have yet.

## What check checks

- **Each gold view against the answer key, row by row.** The daily views, `gold_context_daily` and
  `gold_friction_daily`. The artifact views, `gold_artifact_usage` and `gold_instructions_versions`.
  The weekly views: spend by team and by model, context by team, instructions by team, the starting
  context, cache use by Claude Code release, and failing calls repeated in a row by tool. The check
  applies the five-person minimum to the key's own counts, so it knows which names a view should
  show as "other" and when. For example, a team is named in a week only if five of its people
  worked that week.
- **The caching bug, as the releases table should show it.** In every week, each named release
  with the bug reads less of its context from the cache than each named release without it.
- **Nothing under five people is named,** said once more in plain terms. For example, the security
  team appears in no daily or weekly view, and `db-migrate` in no artifact view.
- **Every query each dashboard sends, through the made-up Grafana,** six at a time as a browser
  sends them. It runs each over the dashboard's own last 30 days and over the whole history. A
  query that fails, or takes longer than 10 seconds, fails the check.

**Not yet run since the dashboard's review and stored silver were combined.** Each passed on its
own. Before the check reads anything, it rebuilds the stored silver tables, which ClickHouse
otherwise rebuilds every 10 minutes
([ADR-0052](adr/0052-silver-is-stored-and-rebuilt-every-10-minutes.md)). A check run straight
after the load would otherwise read tables rebuilt while the load was still sending.
- **The review's views** matched the answer key. Before silver was stored, at 60 people, the
  reviewed dashboard took about 70 seconds to draw, and 13 of its 15 queries took longer than 10
  seconds.
- **Stored silver** passed both parts on the dashboard as it was before the review: about 1.4
  seconds at 60 people, against about 95 before, when six of its 13 panels reached Grafana's
  60-second limit and drew nothing. Every silver view read the raw log table again, so one panel
  read about 100 million rows from a table of 5.5 million
  ([risk 14](../risks.md#14-the-collector-path-views-recompute-everything-on-every-read)).
- **The fleet dashboard answers at once, with no rows.** It reads the Admin API's cost data, which
  this stack does not have yet.

## How it stays apart from real data

- **A stack of its own.** Its compose project is always `cardo-madeup`, with the ports above.
  `scripts/made-up.sh` sends to no other stack. Nothing from it is loaded into a stack that holds
  real rows, and no real row is copied into it.
- **Hook events keep their invented time only here.** A hook event carries no time of its own, so a
  collector stamps it on arrival, and twelve weeks loaded in a quarter of an hour would land on one
  day. The made-up collector alone reads the time from a header the generator sends,
  `X-Cardo-Made-Up-Time`, and then drops it (`deploy/compose/made-up/collector-made-up.yml`). A
  shipped collector with that rule would let anyone on the network backdate events, so a test
  fails if the shipped settings ever mention the header (`test/madeup_test.go`).
- **It says it is not real, wherever it shows.** Its Grafana files the dashboards under a folder
  named "MADE-UP ORGANIZATION - not real data", which is in the path at the top of every dashboard,
  and opens on a page that says the same. Every answer key starts with the same warning.
- **It holds nothing worth keeping.** Every `up` makes a fresh salt, and `down` deletes the data,
  the answer key and the passwords. Raw rows are kept for 90 days, as on any stack, so a made-up
  stack left running loses its oldest days.

## Changing it

- **Better figures** go in `internal/madeup/calibration.go`, and only there.
- **Sizes and their timelines** are in `internal/madeup/org.go`. Each story's text is written from
  its size's weeks and teams, in `internal/madeup/stories.go`.
- **A new story** needs a view it shows in, or a gap. `go test ./internal/madeup/` holds the review
  and CI sizes to every story's text. It also checks that every size can tell its stories, for
  example that a team the stories name exists and that each story falls inside the history.
- **A new or changed view** gets a check against the answer key in
  `test/madeup_check_test.go`. A view is done once it has been seen showing its story.
- **When Claude Code changes what it sends,** the generator follows, as the collector does. It
  copies the event shapes observed from Claude Code (`test/fixtures/hooks/`).
