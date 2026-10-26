# ADR-0056 — Efficiency is compared within one person, over one week

**Status:** Accepted
**Date:** 2026-10-25
**Evidence:** Judgment for the shape of the comparison and for the pooling rule. The field notes
[ADR-0031](0031-what-works-means.md) cites say what is observed. The preregistration at the end is
the part that is not judgment: each claim carries the result that would disprove it, written before
any organization's data is read.

Supersedes [ADR-0031](0031-what-works-means.md). Everything in that record stands — the order
(spread, then efficiency, then delivery), the seven signals, the volume rule, the refusal of a team
breakdown — except what is settled here: how the within-person comparison is computed, what a
person is compared against, how per-person numbers are pooled, and what happens to
`gold_friction_daily`.

## Context

[ADR-0031](0031-what-works-means.md) replaced the friction index with the efficiency family and
said the comparison is made *within the same people*, "so that 'champions are better engineers' does
not show up as the tool's effect", computed per person in silver and pooled in gold. It did not say
how.

What is open, and each is a way to flatter a tool:

- **Which sessions count as "used it"** and which as "did not". A session that loaded the artifact
  is one thing; a session that merely happened after it shipped is another.
- **What a person is compared against.** Their own sessions before, or their own sessions without.
  Before-and-after is always flattering, because people learn and models improve.
- **What happens to a person who used it on every session they had.** They have no other side, so
  they cannot be compared with themselves at all.
- **How per-person numbers are pooled.** Summed across people, or averaged with one person one
  vote. A person with five hundred sessions can decide the answer either way.

Nothing in the store can tell any of this apart today. Of the nine gold views, none pairs a person's
sessions that used an approach with that person's sessions that did not; every one of them
aggregates across people. A tool's number today can be a property of the people who chose it.

Two other things are open:

- **`gold_friction_daily`** (`sql/clickhouse/007_gold_collector.sql:157`) publishes three of the
  family's signals per day and per cohort: `edit_rejection_rate`, `permission_wait_s_p50`,
  `permission_wait_s_p90` and `compactions_per_session`. That is a third definition of the same
  question, and it is broken down by team, which [ADR-0030](0030-stakeholders-and-questions.md)
  decision 4 forbids: "Efficiency and friction are shown by approach, rule or tool, not by team."
- **`trend_day_cohort`** (added 2026-10-25,
  [ADR-0055](0055-trend-rows-outlive-the-rows.md)) kept those same rates by team in a table with no
  TTL. A forbidden breakdown in a permanent table is worse than one in a view, because it outlives
  the decision that forbids it.

## Decision

**1. A person is compared with themselves, inside one week.** Their sessions in that week that used
the approach against their sessions in that week that did not. The week is the period because it is
already the grain the group floor is defined on for artifact names
([ADR-0029](0029-minimum-group-size.md)), and a shorter period would leave most people without both
sides.

**2. Only a person with both sides in that week contributes to the comparison.** A person who used
the approach on every session they had has nothing to be compared with, so they contribute nothing
to efficiency and everything to spread.

The consequence is stated rather than worked around: **an approach that everybody uses cannot be
evaluated by this method at all.** It has spread, which is the first measure, and no efficiency
number. Replacing the missing comparison with before-and-after would produce a number, and the
number would flatter.

**3. The baseline is with-and-without, never before-and-after.** Before-and-after is confounded by
time, and time's direction is known in advance: people learn and models improve, so "after" is
better whatever the tool did. A bias whose direction is known is the one to refuse first.

**4. Per-person differences are computed in silver and pooled in gold as a ratio of sums**, beside
their volumes, as [ADR-0008](0008-outcome-variable.md) requires: people on each side, sessions on
each side. Two numbers guard the pool:

- the row publishes the **number of people who contributed**, so a reader can see when the answer
  rests on few of them;
- **the median per-person difference must share the sign of the pooled one.** When it does not, the
  row says so and no claim is made from it. This is the guard against one prolific person deciding
  for ten quiet ones, and it needs no statistical machinery.

**5. The group floor applies to the people who take part in the comparison** — both sides together —
not merely to the approach's users. The row describes those people, so those people are the group
[ADR-0029](0029-minimum-group-size.md) sizes. Below five, it is not published, and if it is ever
materialized it is not written at all: a row about two people is two people's record, and a person's
record expires ([ADR-0016](0016-retention.md)).

**6. `gold_friction_daily` is retired, and the enablement dashboard's friction panels are rebuilt on
the family, without the team breakdown.** `trend_day_cohort` keeps its volumes — how many sessions,
prompts and permission prompts a team had, which is reach and which ADR-0030 decision 4 allows —
and loses its rate columns. Friction by team goes; reach by team stays.

**7. A signal that is not observed is published as "not observed", never as zero.** A zero says
"measured, and there were none"; "not observed" says "nobody looked". The first version of the
family reads only what Cardo collects, so retry loops and churn are absent from every chart and the
chart says why — the same call T10 made for imported sessions.

**8. A claim about churn is made only when churn moves in the same direction as at least two other
signals of the family.** A refactor also removes lines, and
[INV-5](../design/invariants.md) forbids the content that would tell a refactor from a mistake. The
two are not told apart in the data; what is refused is the claim that rests on churn alone.

## Consequences

- **An approach everyone adopted has no efficiency number.** That is the honest shape of the
  question, and the reader is told so rather than given a flattering substitute.
- The comparison speaks only for **people who mixed the approach inside a week**. That group is
  published as a count and never as names; the per-person difference itself is never published
  ([INV-3](../design/invariants.md)) and the CI check that enforces it keeps applying.
- One prolific person cannot decide the answer: pooling is by volume, and the sign check has to
  agree.
- The enablement dashboard loses its per-team friction panels. What replaces them is per approach.
- Until decision 6 is carried out, `trend_day_cohort` publishes three friction rates by team. That
  is a known contradiction in the tree, named here so it cannot be mistaken for a decision, and it
  closes when the family is built.
- The preregistration below is written before an organization's data is read, so a claim that fails
  stays failed. Nothing here is retuned after seeing numbers.

## Preregistration: the claims, and what would disprove them

**Volume floor for every claim below** (judgment, named so it can be raised): at least **five
people** contributing on both sides — the group floor of [ADR-0029](0029-minimum-group-size.md) —
and at least **twenty sessions on each side**. A claim is not tested at all below that floor; it is
published as "not enough people to say".

| Claim | Would be disproved by |
|---|---|
| An adopted approach reduces failed tool calls, within the people who mixed it | The pooled difference is zero or worse, or the median per-person difference points the other way |
| An adopted approach reduces compactions per session, within those people | The same |
| An adopted approach does not add waiting for people in default mode | The pooled wait rises, or the median per-person difference points that way |
| An adopted approach reduces turns and cost per accepted change | The pooled figure is flat or worse, or the median points the other way |
| An adopted approach reduces churn | Any of: churn is flat or worse; or churn moves while fewer than two other signals move with it; or the median points the other way |

**Not claimed in the first version**, because the data for them is not observed: retry loops of the
same failing call (needs the order of `tool_result` events within a prompt), and auto-mode denials
(never observed). When a field for them appears and survives a real session, a claim may be added
with its own falsifier — by a new record, not by editing this table.

## Rejected alternatives and why

- **Before-and-after the same person.** It always flatters: people learn, models improve, and a
  newer Claude Code is a better Claude Code. The direction of the bias is known before the data is
  read, which makes it the first thing to refuse.
- **Difference-in-differences, adopters against non-adopters.** It compares people with people —
  exactly what [ADR-0031](0031-what-works-means.md) replaced the index to avoid. The adopters'
  trend can differ from everybody else's for reasons that have nothing to do with the tool, and the
  subtraction would credit the tool with it.
- **One person, one vote when pooling.** A person with two sessions would carry the weight of a
  person with two hundred, and the answer would swing on whoever spoke least. Pooling by volume,
  with the contributing people published and the median sign checked beside it, keeps the volume
  rule and the guard in the same row.
- **Keeping `gold_friction_daily` beside the family.** Two numbers for one question, and a reader
  picks the flattering one. ADR-0031 already said "rebuilt"; keeping the old view is the third
  definition of the same thing.
- **Computing the comparison per day.** A day is too short for most people to have both sides, and
  the group floor is already defined on a week for artifact names. One period, one rule.
- **Treating an unobserved signal as zero.** A zero is a measurement; this is an absence. T10 made
  the same call and the same wording.
- **A significance test.** The store has no such machinery and these fleets are small. A
  preregistered direction, a volume floor and a sign check state the standard without pretending at
  a probability the data cannot support.
- **Publishing the per-person differences.** It would show who was helped and who was not, which is
  a ranking of people ([INV-3](../design/invariants.md)). They are computed in silver and pooled in
  gold, and never leave gold.