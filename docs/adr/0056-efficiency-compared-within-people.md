# ADR-0056 — Efficiency is compared within one person, over one week

**Status:** Accepted
**Date:** 2026-10-25
**Evidence:** Judgment for the shape of the comparison and for the pooling rule. The field notes
[ADR-0031](0031-what-works-means.md) cites say what is observed. The preregistration at the end is
the part that is not judgment: each claim carries the result that would disprove it, written before
any organization's data is read.

Extends [ADR-0031](0031-what-works-means.md) without superseding it: spread, efficiency and
delivery stay the three measures, in that order, and everything that record decided — the seven
signals, the volume rule, the refusal of a team breakdown — stands. This record says how the
within-person comparison is computed: which sessions count as using an approach, what a person is
compared against, how per-person differences are pooled, and what happens to `gold_friction_daily`.

Narrows [ADR-0040](0040-spread-is-a-vote-only-for-what-teams-chose.md) (decision 1) on what may be
said about an artifact that is present in every session. Those keep before-and-after around the
declared rollout date, read one way; everything that still has both sides keeps the with-and-without
comparison.

## Context

[ADR-0031](0031-what-works-means.md) replaced the friction index with the efficiency family and
said the comparison is made *within the same people*, "so that 'champions are better engineers' does
not show up as the tool's effect", computed per person in silver and pooled in gold. It did not say
how.

Four questions this record settles, each a way to flatter a tool:

- **Which sessions count as "used it"** and which as "did not". A session that loaded the artifact
  is one thing; a session that merely happened after it shipped is another.
- **What a person is compared against.** Their own sessions before, or their own sessions without.
  Before-and-after is always flattering, because people learn and models improve.
- **What happens to a person who used it on every session they had.** They have no other side, so
  they cannot be compared with themselves at all.
- **How per-person numbers are pooled.** Summed across people, or averaged with one person one
  vote. A person with five hundred sessions can decide the answer either way.

Nothing in the store can tell any of this apart today. None of the gold views pairs a person's
sessions that used an approach with that person's sessions that did not; every one of them
aggregates across people. A tool's number today can be a property of the people who chose it.

Two other things turn on the same refusal of a team breakdown:

- **`gold_friction_daily`** (`sql/clickhouse/007_gold_collector.sql:157`) publishes two of the
  family's signals per day and per cohort — the permission waits (`permission_wait_s_p50`,
  `permission_wait_s_p90`) and `compactions_per_session` — plus `edit_rejection_rate`, which
  belonged to the old index and which the family dropped. It is broken down by team, which
  [ADR-0030](0030-stakeholders-and-questions.md) decision 4 forbids: "Efficiency and friction are
  shown by approach, rule or tool, not by team."
- **A trend table with those rates by team** would be the same fault in a permanent table, because
  it outlives the decision that forbids it. The trend store keeps no per-team table for that reason
  (the trends record, ADR-0055).

## Decision

**1. A person is compared with themselves, inside one week.** Their sessions in that week that used
the approach against their sessions in that week that did not. The week is the period because it is
already the grain the group floor is defined on for artifact names
([ADR-0029](0029-minimum-group-size.md)), and a shorter period would leave most people without both
sides. Decision 9 says which sessions count as using the approach.

**2. Only a person with both sides in that week contributes to the comparison.** A person who used
the approach on every session they had has nothing to be compared with, so they contribute nothing
to efficiency and everything to spread.

The consequence is stated rather than worked around: **an approach that everybody uses cannot be
evaluated by this method at all.** It has spread, which is the first measure, and no efficiency
number — except in the one case decision 10 names. Replacing the missing comparison with a
before-and-after for everyone would produce a number, and the number would flatter.

**3. The baseline is with-and-without, never before-and-after.** Before-and-after is confounded by
time, and time's direction is known in advance: people learn and models improve, so "after" is
better whatever the tool did. A bias whose direction is known is the one to refuse first. Decision
10 is the one carve-out, and it is read in the direction of the bias.

**4. Per-person differences are computed in silver and pooled in gold as a weighted mean of each
person's own difference**, beside their volumes, as [ADR-0008](0008-outcome-variable.md) requires:
people on each side, sessions on each side.

For each person `i` with both sides in the week, `d_i = rate_with_i - rate_without_i` and
`w_i = min(n_with_i, n_without_i)`; the pooled figure is `sum(w_i * d_i) / sum(w_i)`. It is the
average person's own change, weighted by how much of both sides that person has. It is never a sum
of everyone's sessions: that would put the difference between two people into the tool's column.
One person runs 90 sessions with a skill and 10 without, another the reverse; the first has 1
failed call per session either way, the second 5. The skill changes nothing for either of them, and
a ratio of sums publishes a 70% cut. Pooled as above, both differences are zero and the row says
zero. Had the skill saved each of them 0.1, the row says a saving of 0.1, where a ratio of sums
would still say about 70%.

Two numbers guard the pool:

- the row publishes the **number of people who contributed** and the sessions on each side, so a
  reader can see when the answer rests on few of them;
- **the median per-person difference must share the sign of the pooled one.** When it does not, the
  row says so and no claim is made from it. This is the guard against one prolific person deciding
  for ten quiet ones, and it needs no statistical machinery.

**5. The group floor applies to the people who take part in the comparison** — both sides together —
not merely to the approach's users. The row describes those people, so those people are the group
[ADR-0029](0029-minimum-group-size.md) sizes. Below five, it is not published, and if it is ever
materialized it is not written at all: a row about two people is two people's record, and a person's
record expires ([ADR-0016](0016-retention.md)).

**6. `gold_friction_daily` is retired, and the enablement dashboard's friction panels are rebuilt on
the family, without the team breakdown.** Nothing that measures friction is kept or shown by team
anywhere, in a view or in a permanent table, and permission prompts per team are friction in
another form. Reach by team stays — how many of a team use something, which is what reach means.
Sessions, prompts and spend per team are activity, not reach, and are never presented as reach.
Where a live view shows them, it is the sizing [ADR-0030](0030-stakeholders-and-questions.md)
decision 7 allows, never divided by headcount. The trend store keeps nothing by team (ADR-0055).

**7. A signal that is not observed is published as "not observed", never as zero.** A zero says
"measured, and there were none"; "not observed" says "nobody looked". The first version of the
family reads only what Cardo collects, so churn is absent from every chart where it is not read and
the chart says why — the same call the coach's "not observed" makes for imported sessions.

**8. A claim about churn is made only when churn moves in the same direction as at least two other
signals of the family.** A refactor also removes lines, and
[INV-5](../design/invariants.md) forbids the content that would tell a refactor from a mistake. The
two are not told apart in the data; what is refused is the claim that rests on churn alone.

**9. A session used an artifact when the artifact loads carry a row for it.** The rows in
`cardo.silver_artifact_load` are the boundary: the instructions file loaded (`InstructionsLoaded`),
the subagent started (`SubagentStart`), the skill or command run (`UserPromptExpansion`), the
connector's tool called. A session that merely came after the artifact shipped does not count as
using it.

**10. For an artifact that is present in every session, the comparison moves to before-and-after
the rollout date the organization declared.** Most rolled-out artifacts keep both sides — a skill
pushed to everyone runs in some sessions and not others, so decisions 1 to 4 apply to them. What has
no without-side is whatever is in every session: an instructions file the organization ships, a
hook, a default setting. For those, and only those, the readout is one period before the declared
date against one period after it, and it is read one way, because the bias favours "after". A result
no better than before stands and is published as "no better than before", a finding about the
artifact. A result better than before is published as "better than before, which learning and
upgrades also explain" and never as the artifact's effect. A shipped instructions file that takes
failed calls from 2.0 to 2.4 per session shows up as worse this way, where no number at all would
make it look exactly like one that helps.

## Consequences

- **An approach everyone adopted has no efficiency number** beyond decision 10's one-way readout.
  That is the honest shape of the question, and the reader is told so rather than given a flattering
  substitute.
- The comparison speaks only for **people who mixed the approach inside a week**. That group is
  published as a count and never as names; the per-person difference itself is never published
  ([INV-3](../design/invariants.md)) and the CI check that enforces it keeps applying.
- **Within one person, the two sides differ in the work they did.** People reach for a debugging
  skill when they are debugging, and debugging sessions fail more calls anyway; for an instructions
  file, the sessions without it are mostly in another repository. Nothing Cardo collects removes
  this, and nobody knows in advance which way it leans. The row is computed as written, and this
  caveat travels with it.
- One prolific person cannot decide the answer: pooling is a weighted mean of each person's own
  difference, and the sign check has to agree.
- The enablement dashboard loses its per-team friction panels. What replaces them is per approach.
- The preregistration below is written before an organization's data is read, so a claim that fails
  stays failed. Nothing here is retuned after seeing numbers.

## Preregistration: the claims, and what would disprove them

**Volume floor for every claim below** (judgment, named so it can be raised): at least **five
people** contributing on both sides — the group floor of [ADR-0029](0029-minimum-group-size.md) —
and at least **twenty sessions on each side**. A claim is not tested at all below that floor; it is
published as "not enough people to say".

Each row says what is counted and what it is divided by, per person per side, so the number is
pinned before the data arrives.

| Claim | What is counted, over what | Would be disproved by |
|---|---|---|
| An adopted approach reduces failed tool calls, within the people who mixed it | Failed calls — `silver_tool_call` rows with `ran AND NOT succeeded` — over the calls with `ran` | The pooled difference is zero or worse, or the median per-person difference points the other way |
| An adopted approach reduces compactions per session, within those people | `silver_context_event` rows with `kind = 'compaction'`, over sessions | The same |
| An adopted approach does not add waiting for people in default mode | `silver_policy_decision.wait_ms` where `permission_mode = 'default'`, summed over the count of those decisions (the mean wait) | The pooled wait rises, or the median per-person difference points that way |
| An adopted approach reduces turns and cost per accepted change | Turns (`silver_turn`) and cost (`silver_api_request.cost_usd`) over accepted changes (`silver_tool_call` with `decision = 'accept'`) | The pooled figure is flat or worse, or the median points the other way |
| An adopted approach reduces retry loops | Sessions in which the same tool failed three or more times in a row — the definition `gold_tool_loops_weekly` counts on `main` — over sessions | The pooled difference is zero or worse, or the median points the other way |
| An adopted approach reduces churn | Lines added, modified and removed (`claude_code.lines_of_code.count` by `type`) over accepted changes; read from the metric points the collector writes (`bronze_otel_metrics_sum`), because no silver table carries it yet | Any of: churn is flat or worse; or churn moves while fewer than two other signals move with it; or the median points the other way |

**Not claimed:** auto-mode denials, which nothing observes. Retry loops were unobserved when this
record was first written; `main` now counts three or more failures of the same tool in a row
(`gold_tool_loops_weekly`), so the table carries a claim for them with its falsifier. Once this
record is accepted, a new claim arrives by a new record, not by editing this table.

## Rejected alternatives and why

- **Before-and-after the same person.** It always flatters: people learn, models improve, and a
  newer Claude Code is a better Claude Code. The direction of the bias is known before the data is
  read, which makes it the first thing to refuse. Decision 10 is the one case it survives, read in
  the direction of the bias.
- **No number at all for what is in every session.** It would make a shipped instructions file that
  hurts look exactly like one that helps. The one-way before-and-after shows the harm.
- **Difference-in-differences, adopters against non-adopters.** It compares people with people —
  exactly what [ADR-0031](0031-what-works-means.md) replaced the index to avoid. The adopters'
  trend can differ from everybody else's for reasons that have nothing to do with the tool, and the
  subtraction would credit the tool with it.
- **Pooling as a ratio of sums over everyone's sessions.** It adds up every session with the
  approach and every session without across people, which puts the difference between two people
  into the tool's column — the very comparison ADR-0031 exists to remove.
- **One person, one vote when pooling.** A person with two sessions would carry the weight of a
  person with two hundred, and the answer would swing on whoever spoke least. Pooling by volume,
  with the contributing people published and the median sign checked beside it, keeps the volume
  rule and the guard in the same row.
- **Counting a team's sessions, prompts and permission prompts as its reach.** Activity is not
  reach: reach is how many of a team use something. Permission prompts per team are friction by
  team in another form, and [ADR-0030](0030-stakeholders-and-questions.md) decision 4 forbids the
  breakdown whatever the column is called.
- **Keeping `gold_friction_daily` beside the family.** Two numbers for one question, and a reader
  picks the flattering one. ADR-0031 already said "rebuilt"; keeping the old view is the third
  definition of the same thing.
- **Computing the comparison per day.** A day is too short for most people to have both sides, and
  the group floor is already defined on a week for artifact names. One period, one rule.
- **Treating an unobserved signal as zero.** A zero is a measurement; this is an absence. The build
  made the same call and the same wording.
- **A significance test.** The store has no such machinery and these fleets are small. A
  preregistered direction, a volume floor and a sign check state the standard without pretending at
  a probability the data cannot support.
- **Publishing the per-person differences.** It would show who was helped and who was not, which is
  a ranking of people ([INV-3](../design/invariants.md)). They are computed in silver and pooled in
  gold, and never leave gold.