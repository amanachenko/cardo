# ADR-0036 — Service runs are labelled, and are never counted as people

**Status:** Accepted — **not built**
**Date:** 2026-09-25
**Evidence:** `2026-09-23-admin-analytics-apis.md` (`api_actor` versus `user_actor`);
`2026-09-23-hooks-otel-collector-surfaces.md` (hooks under `claude -p`; which events fire
headlessly is **not documented**). Otherwise judgment.

## Context

Agents are starting to run with nobody at the keyboard: `claude -p` in CI, background agents, and
applications built on the Agent SDK. Every Cardo row belongs to a pseudonym, which means to a
person. Take a nightly job that runs `claude -p "fix the flaky tests"`. It goes wrong one of two
ways:

- **Run with an API key:** there is no email, so there is no pseudonym, and the views count it as
  nobody. Its cost is in the fleet total but in no team, and its skill uses count as uses but add no
  people.
- **Run with someone's login:** every run lands on that person. They appear as one engineer with
  400 sessions a day, straight onto the over-budget list
  ([ADR-0033](0033-per-person-cost-for-budget-owners.md)).

Anthropic's usage data already tells the two apart: `api_actor`, named by its key, versus
`user_actor`. CI runners are not managed by MDM, so their settings come from the workflow's own
configuration.

## Decision

1. **Two columns:**
   - `actor_kind`, either `person` or `service`;
   - `workflow`, a name such as `nightly-flaky-tests`.

   They are filled from resource attributes, `cardo.actor=service` and `cardo.workflow=<name>`, the
   same way the team label arrives today.
2. **A ready-made CI settings block,** starting with GitHub Actions, that turns on telemetry and sets
   those attributes. **The guide that ships with it says to run with an API key, not a person's
   login.**
3. **The views keep service runs out of anything about people:**
   - out of people counts and the minimum group size (the five-person rule is about people);
   - out of the budget list;
   - out of the coach.

   They appear as their own totals.
4. **Views for agent runs** (cost per workflow and per repository, success meaning the run's PR
   merged) wait until a pilot actually runs agents.

## Consequences

- On a managed laptop the bundle's `OTEL_RESOURCE_ATTRIBUTES` wins over a person's own, so a person
  cannot label themselves a service to leave the budget list.
- **A run made with a person's login still appears under their name in Anthropic's own data,**
  which the label cannot change. Only the setup guide prevents it.
- Which hook events fire headlessly is unobserved. The first CI run through the collector is also
  the observation.
- The cost of this is small now and large after the first team automates: two reports we agreed
  on would break silently.

## Rejected alternatives and why

- **Nothing until agents show up.** The first team to automate breaks the people counts and the
  budget list without anyone noticing.
- **Columns only, with no CI settings block.** Nothing would set the label, so it is the same as
  nothing.
- **Guessing service runs from behaviour,** such as sessions per day. Guesswork that misfires on the
  busiest engineer.
- **A separate pipeline for agents.** Premature. The same telemetry, carrying a label, is enough
  until agent-run views are wanted.
