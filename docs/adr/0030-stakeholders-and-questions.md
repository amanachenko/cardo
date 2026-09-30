# ADR-0030 — Who Cardo serves, and the questions it will and will not answer

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** `2026-09-25-stakeholder-field-notes.md` (field observations, a small sample);
`2026-09-23-admin-analytics-apis.md` (what Anthropic already shows);
`2026-09-21-prior-art-survey.md` (who sells what). The choices are judgment.

Extends [ADR-0001](0001-unit-of-analysis.md) without superseding it: the enablement artifact stays
the unit of analysis. This ADR names who the analysis is for.

## Context

ADR-0001 made the platform team the first audience. By the end of Phase 2 the collection worked,
but the questions had been chosen by what the telemetry could measure. No stakeholder had said
what they would decide differently after seeing an answer.

Field notes from organizations adopting Claude Code change the picture:

- Adoption has been **bottom-up**, through champions and through teams chartered to find their own
  way. That produced a large variety of team-grown skills, tools and conventions.
- It is turning **top-down**. Boards ask whether the cost is justified, and leadership wants every
  team on the best option, soon.
- **Impact is rarely measured, and never reliably** in the organizations observed.
- **Leadership already watches cost per person**, by email, in Anthropic's console, Datadog or
  their own Snowflake.
- **Some organizations ban adoption** on policy grounds.

Anthropic already reports usage and cost per person, and documents skill and connector usage for
Enterprise. DX, Jellyfish and Faros sell cross-tool AI return-on-investment tied to delivery data.

## Decision

**1. Three users, in this order.**

- **The adoption owner** — the CTO, or whoever is tasked with adoption. They decide whether Cardo is
  adopted. Working under them are two roles:
  - the **operator**, who runs the adoption week to week and reports up;
  - the **champions**, who build the team-grown approaches. Seeing theirs spread is both their
    reward and their case for making it official.
- **Engineers**, through a personal coach ([ADR-0032](0032-engineer-coach-by-shared-link.md)).
- **Security and compliance**, through evidence that the policies administrators set actually hold
  ([ADR-0037](0037-policy-evidence.md)).

**2. Leadership's central question is convergence.** Of the approaches our teams invented, which
should everyone use, and what does each cost? Cardo leads with this, not with generic AI
return-on-investment.

**3. "Uniform" means teams converging on the approaches that win.** That is a matter of setup and
artifacts, never a score for how each person works.

- Divergence is signal. The home-grown approaches that spread fastest are the candidates to make
  official.
- New approaches still appearing is shown as well, so that converging does not freeze each team on
  whatever won first.

**4. Teams are compared on reach, never ranked.**

- Adoption and version reach are shown by team, against the fleet as a whole, and never sorted into
  a league table.
- Efficiency and friction are shown by approach, rule or tool, not by team. The cause of a team's
  friction is almost always a rule, and a rule is an artifact.

**5. Anthropic's analytics are a complement.** Cardo uses their numbers as context and as
denominators (commits and PRs by Claude Code), mapped to teams. It does not compete on counts the
vendor already gives.

**6. The board report.** Each figure is shown with its volume:

- unit cost: cost per accepted edit, and per Claude Code commit and PR;
- rework spend;
- cost per approach.

**It carries no time-saved or return-on-investment estimate.**

**7. Sizing.** Trends by team and repository (throughput, cost, adoption) are shown, and **never
divided by headcount**. Cardo computes and shows no per-head ratio. The notice to engineers says
Cardo's data is not for individual performance decisions.

**8. Other tools.** Claude Code only for now. Full adapters for other tools come as organizations
need them ([ADR-0004](0004-claude-code-first-neutral-schema.md) stands). Comparing tools fairly
needs a measure none of them supplies, and the git host provides it
([ADR-0031](0031-what-works-means.md)).

## Consequences

- `gold_friction_daily` breaks friction down by team, which decision 4 rules out. It changes when
  the efficiency family replaces the friction index ([ADR-0031](0031-what-works-means.md)).
- **Leadership as the main audience puts Cardo next to DX, Jellyfish and Faros.** The edge is narrow
  and specific:
  - which approach wins, at the level of artifacts;
  - self-hosting;
  - policy evidence.

  On generic return-on-investment Cardo is weaker: one tool, and no git or issue-tracker data yet.
- Decisions 6 and 7 are promises to engineers as much as product choices. They go into
  `docs/design/privacy.md` and the overview.
- Whether leadership decides anything with Cardo is unvalidated. The adoption pilot's success
  criteria test it ([`docs/roadmap.md`](../roadmap.md)).

## Rejected alternatives and why

- **The platform team first, as ADR-0001 read.** The person holding the budget asks the cost
  question and would buy the answer elsewhere. The platform team remains as the operator, under the
  adoption owner.
- **Leadership first, on generic AI return-on-investment.** A crowded, paid market. Cardo would be
  weaker in it: one tool, no delivery data.
- **Security on equal footing now.** Its evidence needs a working product for the other users
  first, and the organizations that ban adoption have nothing to measure until a pilot is allowed.
  It comes third in the pilots, not last in priority.
- **Engineers first.** Nobody buys it.
- **Uniform practice**, such as a plan-mode rate or a "uses the approved workflow" score per person.
  It is gameable and it is surveillance, ADR-0001's failure modes exactly.
- **Teams ranked against each other.** It becomes a scorecard for team leads, and the fix is almost
  never theirs.
- **Friction by team.** Same objection, and it points at the wrong cause.
- **Replacing the vendor's console** with team-only versions of its numbers. An admin can always
  open the console, so claiming to replace it would be untrue.
- **A time-saved or return-on-investment estimate.** Every vendor sells one and nobody can defend
  it. One indefensible number discredits the rest of the report.
- **Capacity modelling, or throughput per engineer.** "PRs per engineer" is the number that ends up
  in a performance review. The moment engineers learn the data feeds headcount, the coach becomes
  an evidence file.
- **Cost-only adapters for other tools first.** Recommended during the review and not chosen:
  organizations want tools measured against each other on more than cost.
