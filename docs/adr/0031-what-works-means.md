# ADR-0031 — What "works" means: spread, then efficiency, then delivery

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** `2026-09-25-stakeholder-field-notes.md` (auto mode; no commit identifier on any
surface); `2026-09-24-first-real-hook-payloads.md` and `2026-09-24-second-real-session.md` (what
auto mode sends). **The efficiency signals are judgment and explicitly unvalidated** — see
`risks.md` #1.

Supersedes [ADR-0008](0008-outcome-variable.md).

## Context

ADR-0008 defined a friction index with three parts: edit rejection rate, permission-wall time and
compaction rate. It also expected delivery outcomes to join later by commit SHA.

Two things have changed.

**Auto mode removes most of the friction the index measures.** Field estimates put nearly everyone
in auto mode now. In it, an allowed call raises no permission prompt and no edit is held for review,
so permission wait and edit rejections fall towards zero. Our own two sessions were already about
half in auto mode. The index's auto-mode signal, `PermissionDenied`, has never been observed.

**No surface carries a commit or PR identifier.** There are only counts per session and per day. A
join by commit SHA cannot be built.

Leadership's question, meanwhile, is which team-grown approach to make everyone use
([ADR-0030](0030-stakeholders-and-questions.md)). That needs a definition of "works" that holds
whatever mode people run in.

## Decision

An approach works by three measures, in this order.

**1. Spread.** Peers adopting it is the vote. This is built (`gold_artifact_usage`).

**2. Efficiency.** Sessions that use an approach are compared with sessions that do not, **within
the same people**, so that "champions are better engineers" does not show up as the tool's effect.
The comparison is computed per person in silver and pooled in gold; it is never shown per person.
The signals are the **efficiency family**, which replaces the friction index:

- failed tool calls, and retry loops of the same failing call;
- auto-mode denials;
- compactions;
- churn: lines removed soon after they were added;
- turns and cost per accepted change;
- permission waits, kept for the people who run in default mode.

**ADR-0008's volume rule carries over unchanged:** every rate is shown beside its volume, and the CI
check that enforces it stays.

**3. Delivery.** PR outcomes come from the git host, through a poller in `cardo`:

- **Scope:** the organization's own repositories only. Per PR: when it was opened and merged,
  whether it was reverted, and whether it carries Claude Code's co-author line (or another tool's
  mark, where the tool leaves one).
- **Never collected:** titles, code or authors.
- **Joined to everything else by repository and week** ([ADR-0035](0035-repository-identity-classified.md)),
  never per session or per person.
- **The question it answers** is within one repository. After an approach spread there, did time to
  merge and revert rate move for Claude-assisted PRs compared with the rest?
- **Source:** GitHub first. Importing from a delivery tool the organization already runs is the
  alternative.
- **Not before the adoption pilot.** A credential that reads repositories meets real resistance.
  Comparing tools raises its priority, because the git host measures every tool the same way. Where
  it lands is in [`docs/roadmap.md`](../roadmap.md).

**The efficiency family is a hypothesis.** The dogfood and the adoption pilot exist partly to test
it, and churn is the least certain signal: a refactor also removes lines.

## Consequences

- The enablement dashboard's friction panels and `gold_friction_daily` get rebuilt on the efficiency
  family, without the team breakdown ([ADR-0030](0030-stakeholders-and-questions.md) decision 4).
- The within-person comparison computes per person inside the views. INV-3 governs what a view
  shows, so the gold view that publishes the result still selects no pseudonym, and the CI check
  still applies to it.
- **Some signals need fields that are unobserved or undocumented**: retry loops need the order of
  `tool_result` events within a prompt, and churn needs `lines_of_code.count` by type within a
  session. The dogfood checks both before any view depends on them.
- Delivery outcomes add a second external credential beside the admin key, and a second poller.
- "Friction" survives as a word for one part of the family, not as the headline number.

## Rejected alternatives and why

- **Keep the friction index.** It measures waiting on a person, and auto mode removes most of the
  waiting. In an auto-mode fleet it would report near zero and look like success.
- **Drop the idea of friction altogether** and rely on unit cost and rework. That loses the
  signals of the agent struggling (failures, denials, compactions), which are what tell the
  operator which approach helps.
- **Delivery outcomes joined by commit SHA**, as ADR-0008 planned. No surface carries one.
- **Delivery outcomes per person.** Turns an enablement measure into a performance review
  (ADR-0030, decision 7).
- **Only importing outcomes from an existing delivery tool.** Most of the organizations observed
  have not started measuring impact, so there is usually nothing to import.
- **Delivery outcomes first,** before efficiency. They are what organizations care about most, and
  also the slowest, most resisted integration. Efficiency can be learned from data we already
  collect.
