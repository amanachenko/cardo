# ADR-0054 — Both actor types are people, so a service run is labelled by the run itself

**Status:** Accepted
**Date:** 2026-10-25
**Evidence:** `2026-09-23-admin-analytics-apis.md` (`api_actor` versus `user_actor`);
[ADR-0036](0036-service-runs-are-not-people.md).
**Refines:** [ADR-0036](0036-service-runs-are-not-people.md), which stays as it was written

## Context

[ADR-0036](0036-service-runs-are-not-people.md) decided that service runs are labelled and kept out
of people counts, and it gave a reason: *Anthropic's usage data already tells the two apart:
`api_actor`, named by its key, versus `user_actor`.*

That reason does not hold. The Analytics API documents exactly two actor types, `user_actor` (an
email address) and `api_actor` (a key name), and **both are people**. A key belongs to whoever
holds it, and a key used by a CI runner was created by a person who then forgot it existed. So an
`api_actor` row is not a machine; it is a person reached through a different door.

This is the second time this project has been caught by a documentation summary. The first was a
hook field name read from a summary of Claude Code's documentation, which was wrong about four
events. The lesson is the same both times: what a summary says is not what the thing does, and the
thing itself has to be looked at.

ADR-0036's **decision** still stands, and this record does not change it: service runs are labelled,
and the label comes from the run. What changes is where the label comes from, and why the actor
type cannot be it.

## Decision

1. **The service label comes from the run, not from the actor type.** A run declares itself with
   `cardo.actor=service` and `cardo.workflow=<name>`, the same way the team label arrives today.
2. **An actor type is never enough to tell a service from a person.** Both documented types are
   people, so neither may be read as "not a person". A run that wants to be counted as a service
   says so; a run that says nothing is a person, which is the right default for the run nobody
   labelled.
3. **CI runners are labelled by their own configuration,** because they are not managed by MDM:
   their settings come from the workflow's own file, and that file is where the label goes.
4. **ADR-0036's text is left as it was written.** It records what we believed at the time. This
   record says which part of the reasoning was wrong, so a reader does not have to guess.

## Consequences

- The collector's `refuse_weak_salt` and the two actor types do not decide anything about people.
  Only the label does.
- The ready-made CI block has to set the label, or the run is counted as a person. That block ships
  with `deploy/ci/github-actions.card.yaml`.
- The people counts in the gold views filter on `actor_kind != 'service'`, and so does the coach
  ([ADR-0048](0048-coach-in-the-console.md)): a service run is never counted as an engineer's
  sessions.
- A run with an API key that nobody labelled is counted as a person, and it looks like one engineer
  with an unusual number of sessions. That is the safe direction to be wrong in: the alternative
  would hide a real person from the counts.

## Rejected alternatives and why

- **Keep ADR-0036's reasoning and treat `api_actor` as a machine.** It would let the views decide
  from the actor type alone, which is simpler. It is also wrong: the key belongs to a person, and
  dropping that person's rows from the counts is the opposite of what the views are for.
- **Edit ADR-0036 in place to correct the sentence.** Accepted records are immutable; the archive is
  worth something because it records what was believed at the time. A short record that corrects
  one reasoning keeps both.
- **Treat any run with an API key as a service.** Same error in the other direction: the key
  belongs to a person, and a person's real sessions would disappear from the counts.
