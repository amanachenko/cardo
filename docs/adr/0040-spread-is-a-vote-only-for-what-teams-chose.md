# ADR-0040 — Spread is a vote only for what teams chose

**Status:** Accepted — **not built**
**Date:** 2026-10-08
**Edited:** 2026-10-09, in place, to word it for any organization. The decision is unchanged.
**Evidence:** Judgment, no evidence. `2026-09-25-stakeholder-field-notes.md` records adoption
turning top-down; a contributor's proposal of 2026-10-07 argued that usage is not value.

Extends [ADR-0031](0031-what-works-means.md) without superseding it: spread, efficiency and
delivery stay the three measures, in that order. This ADR says how spread is read once leadership
rolls artifacts out.

## Context

ADR-0031 makes spread the first measure of whether an approach works: peers adopting it is the
vote. That holds while adoption is bottom-up. An engineer who keeps using a team-grown skill has
chosen it over the alternatives.

Adoption is turning top-down ([ADR-0030](0030-stakeholders-and-questions.md)). Leadership wants
every team on the best option, and engineering organizations are reorganizing into smaller teams
with standard setups. An artifact pushed to everyone through managed settings or a plugin, or
mandated in a memo, spreads whether it helps or not. Its spread measures the rollout. It stops
being a vote at the moment leadership starts relying on it.

A contributor's proposal put it as "usage is not value": a skill can be invoked forty times a week
and make each of those sessions worse. Cardo would show the forty.

The same proposal had every artifact view end in a verdict computed by a gate: keep, change or
kill. The decision it points at is the right one. A pilot succeeds when the adoption owner
decides something, such as making an artifact official or retiring one
([roadmap](../roadmap.md#how-a-pilot-judges-cardo)). A computed verdict is a different thing. The
efficiency family is a hypothesis (ADR-0031, [risks.md](../../risks.md) #1), and a "kill" stamped
on a champion's skill from unvalidated signals would cost Cardo the people adoption runs on: seeing
their work spread is their reward (ADR-0030).

## Decision

**1. Spread is read by how an artifact arrived.**

- **Chosen:** grown by a team, or made available and left optional. Spread is the vote, as
  ADR-0031 says.
- **Rolled out:** pushed to everyone, set as a default, or mandated. Spread measures the rollout's
  reach. Whether the artifact helps is answered by the efficiency family alone, within the same
  people, before and after it arrived.

A view that shows a rolled-out artifact's spread labels it as reach, not adoption.

**2. The operator declares each rollout:** the artifact and the date, one entry per cohort if the
rollout was staged. Cardo does not infer it. It is configuration, as the approved MCP list is
([ADR-0037](0037-policy-evidence.md)). Carrying the organization's own name
([ADR-0029](0029-minimum-group-size.md)) does not make an artifact rolled out: an official skill
can still be optional.

**3. Substitution joins the efficiency family.** When an artifact arrives, does the mix of tool
classes in the same people's sessions shift? For example, fewer `Grep` and `Read` calls once a
code-search MCP server is in use. It is counted from tool names, which tier 1 already collects.
Like the rest of the family it is a hypothesis.

**4. The declared rollouts are also the inventory.** A rolled-out artifact that few people use is
shown as a candidate to fix or retire. "Configured but never called" is read from this list, never
from the security tier's MCP inventory.

**5. A view ends in a decision, not a verdict.** Each artifact view sets side by side what the
adoption owner needs to make it official, fix it, retire it or leave it alone: spread or reach, the
efficiency signals with their volumes, and cost. While the efficiency family is unvalidated, Cardo
computes no keep, change or kill, and no composite score for an artifact.

**6. An efficiency claim is written down before the data is read.** Before a pilot's data is used
to say one approach is more efficient than another, the claim is recorded with its comparison, its
signal and the result that would refute it. A difference found by looking is reported as something
to test next, not as a finding. The record goes with the pilot's notes, under
[ADR-0038](0038-public-under-a-collective-copyright.md)'s rules for what may be committed.

## Consequences

- Before a pilot at an organization, the operator's configuration gains a rollout list and the
  artifact views gain a column saying how each artifact arrived. The mechanism is not decided.
- A rolled-out artifact can show near-total reach and no measured effect. That is a finding, and
  the view has to make it as easy to read as a success.
- A before-and-after comparison needs the "before" still in retention. With tier 1 kept for 90
  days ([ADR-0016](0016-retention.md)), a rollout's effect has to be read within about two months of
  its date, until rollups outlive retention (risks.md #10).
- Substitution reads the mix of tool classes, which also moves when Claude Code changes its own
  tools. The comparison is read beside `app.version`, and a rollout in the same week as an upgrade
  is confounded.
- Spread was the one measure that needed no validation. For rolled-out artifacts Cardo now relies
  on the measures that do, so validating the efficiency family matters more than it did.
- Writing the claim down first costs a paragraph per pilot, and makes "we found no difference" a
  result that can be reported.

## Rejected alternatives and why

- **Spread as the vote for every artifact,** as ADR-0031 reads. Under a rollout it reports the
  rollout's success as the artifact's.
- **Inferring rollouts from the adoption curve,** such as many teams starting on the same day. A
  Claude Code release, an all-hands demo or a popular post draws the same curve, and the operator
  already knows the answer.
- **A verdict per artifact computed by a gate: keep, change or kill.** The signals behind it are
  unvalidated, a verdict hides the volume each rate needs (ADR-0031), and retiring a champion's work
  is a judgment for a person to make and explain.
- **Finding unused MCP servers in the security tier's inventory.** It carries tier-0 data into
  tier-1 views, across a wall [ADR-0002](0002-three-tier-data-model.md) makes structural.
- **Measuring a rolled-out artifact against the people who did not get it.** The teams that get a
  rollout first are rarely a random sample, which is the confound ADR-0031's within-person
  comparison exists to avoid.
