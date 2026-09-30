# ADR-0029 — Views name a group only once five people are in it

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** Judgment, the operator's, on the three questions below. The number five is
[ADR-0025](0025-artifact-names-kept-with-guardrails.md)'s working assumption.

## Context

[INV-3](../design/invariants.md) says cohort aggregates require a minimum cohort size.
[ADR-0025](0025-artifact-names-kept-with-guardrails.md) says a home-grown artifact name appears in
a view only once that many people use it, and left the number to "when the views are built". Five
was the working assumption. Nothing enforced any size yet. The Admin API views group the whole
fleet by day and have no smaller group to protect.

The collector-path views bring two kinds of label that can point at a person:

- **Artifact names engineers choose themselves.** A command only one person has written is a
  quasi-identifier, and sometimes content.
- **Cohort labels.** The bundle's `cardo.cohort`, a team name. A team of two on a friction chart
  is two people's friction, and its manager can tell which is which.

Three questions were put to the operator:

- What the number is, and who may change it.
- Whether the organization's own artifact names are held back too.
- Where the views learn the organization's naming pattern, `CARDO_ORG_ARTIFACTS`. Until now only
  the collector read it.

## Decision

**1. The minimum group size is five, and it is a floor.** An operator may raise it with
`CARDO_MIN_GROUP_SIZE`, for example to the ten a works council asks for, but never lower it.
This mirrors the salt: a minimum is enforced, and stricter is allowed. It is enforced twice:

- `cardo migrate` refuses a value below five.
- The `settings_effective` view applies `greatest(5, …)`, so a row written into the settings table
  by hand cannot lower it either.

**2. It applies to two kinds of label.** A label below the threshold is shown as `other`, and its
rows are counted there.

- **Home-grown artifact names:** commands, skills, subagents and MCP servers whose name does not
  match `CARDO_ORG_ARTIFACTS`.
- **Cohort labels.**

"People" means distinct pseudonyms in the view's own period. For artifact usage that is a week; for
the daily cohort views it is a day.

**3. It does not apply to three things.**

- **The organization's own names.** The organization chose them, so they identify nobody. Stale
  instructions detection ([ADR-0026](0026-stale-instructions-by-versioned-name.md)) needs them
  even when only a few people load a file, because the stragglers are the point.
- **Claude Code's built-in subagents** (`general-purpose`, `Explore`, `Plan`, `statusline-setup`,
  `claude-code-guide`), the list the collector's strict mode already exempts.
- **Fleet-wide totals.** The fleet is the cohort of last resort, as it is on the Admin API path.

**4. A row with no pseudonym counts as nobody.** Hook rows reach a person only through their
session's OTel rows. If OTel is off, a name can never cross the threshold.

**5. The views read deployment choices from `cardo.settings`.** `cardo migrate` writes it from
`CARDO_ORG_ARTIFACTS` and `CARDO_MIN_GROUP_SIZE`:

| The variable is | The setting |
|---|---|
| unset | is left as it is |
| set but empty | goes back to its default |
| set | is written |

The collector and `cardo migrate` must be given the same `CARDO_ORG_ARTIFACTS`. The reference
stack gives both the same value from one `.env`. The poller does not write settings.

## Consequences

- One engineer evaluating on a laptop sees the organization's names and every fleet total. Their
  own commands and their cohort appear as `other`. That is the guardrail working, not a fault.
- A fleet smaller than five is not protected by this. Its totals are effectively a few people's
  numbers, as they already are on the Admin API path. The fix, suppressing any view whose whole
  population is below the minimum, would blank every pilot of fewer than five people. It is left
  for the first deployment that needs it.
- **Folding small cohorts into `other` hides the label, not the arithmetic.** If only one small
  team is folded on a given day, `other` is that team. The views cannot prevent every inference
  from differences. They prevent the direct one.
- If the collector and the views are given different patterns, the labels disagree. Nothing extra
  is stored, because what is stored is decided at the collector.
- The list of built-in subagents now lives in two places: the collector config and the SQL.
- Raising the size is an environment change and a `cardo migrate`, with no new migration.

## Rejected alternatives and why

- **A constant in the SQL.** Simplest. A works council's ten would then need a new migration that
  recreates every view, and an organization is more likely to be asked for a larger number than
  to want a smaller one.
- **Freely configurable.** It would let a single evaluator set 1 and see every name. It would also
  let a production operator set 1, after which every name is a quasi-identifier and every team
  chart is a person's. This is the salt again: a weak value that makes a demo easier is exactly
  what an invariant exists to refuse.
- **Hold back the organization's names too.** Strictest and simplest. It hides a new rule until
  five people load it, which hides early rollout and exactly the stale stragglers stale detection
  exists to find.
- **Let the collector label each name as the organization's at ingest.** It would keep one reader
  of the pattern. But a label written at ingest is frozen: a pattern corrected later would not
  relabel the past. And it adds a stored attribute (INV-4) and OTTL across five fields on two
  paths.
- **A Grafana dashboard variable for the pattern.** It makes the dashboard the configuration.
  Anyone querying the views directly would bypass it.
- **ClickHouse custom settings in a user profile.** The value would be invisible, set per database
  user, and missing for any user it was not set for.
