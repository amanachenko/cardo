# ADR-0032 — The engineer's view is a coach, and in pilots it is a shared link

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** `2026-09-25-grafana-access-and-shared-links.md` (measured);
`2026-09-25-stakeholder-field-notes.md` (why engineers want it). The coach's content is judgment.

Brings forward, in a smaller form, the per-engineer view that [ADR-0017](0017-v01-scope.md) held
for v0.2.

## Context

Engineers are Cardo's second user ([ADR-0030](0030-stakeholders-and-questions.md)). Their company
asks more and more of them, and a personal view of how they are doing, with points to improve,
leaves them feeling in control.

Two facts constrain how that view can be delivered.

**A pseudonymous page about one person is not anonymous.** Anthropic's console shows each person's
daily cost by name. A per-person Cardo page shows a pseudonym's daily cost too. Line the two up and
the pseudonym has a name, and everything else on the page is that person's: permission waits,
compactions, which artifacts they have not tried, the suggestions they were given. Team labels on
small teams, working hours, and being the first to use a command you wrote give a person away just
as easily. INV-3 forbids per-person pages, not per-person names.

**Grafana cannot restrict a login to its own rows.** It was measured on the reference stack:

- A Viewer can send any SQL to a data source, because `/api/ds/query` is how dashboards run.
- The stack's data source connects as the main ClickHouse user, which reads everything.
- Grafana's shared dashboards do hold: a 32-character token, the saved queries only, no login, and
  404 for a wrong token.

## Decision

**1. A coach, not a scorecard.**

- Each suggestion is tied to something the engineer can fix. For example:
  - "You waited 14 minutes this week on permission prompts for `npm test`. Here's the allow rule."
  - "Six peers use `/review`, and you haven't tried it."
  - "Your sessions compact twice as often as they did last month."
- The engineer is compared only with their own past.
- There is no percentile, no rank against peers, and no single score.

**2. In pilots, each volunteer's page is a shared link from a separate Grafana.**

- A second Grafana, "personal", has one login: the operator's. Its data source can read per-person
  rows. With nobody else logged in, nobody can send it their own queries.
- For each volunteer, the operator copies the coach dashboard, fixes that volunteer's pseudonym
  inside every query, and shares the copy by link. For 15 to 30 people this is done by hand.
- Volunteers opt in, and know that the operator can open their page.
- Every link is revoked when the pilot ends.

**3. `cardo pseudonym <email>`** prints a pseudonym, so that the operator can make the copy. It
needs the salt.

**4. The leadership Grafana reads through a ClickHouse user granted the gold views only.** This is
done before anyone except the operator has a login there.

**5. Delivery by message waits for evidence.** A weekly digest sent by `cardo`, and a web app with
company sign-in, are built only if volunteers act on the suggestions in the pilot.

## Consequences

- The pilot build is one extra container, one dashboard, one small command and one database grant.
- **The operator can read every volunteer's page** during a pilot. That is acceptable only because
  volunteers agreed, and it is the gap a delivery channel would close.
- A link works for whoever holds it. A volunteer can forward their own page, and that is their
  choice.
- A manager can ask an engineer to show them their page, and no design prevents the asking. A page
  with no score is useless as evidence and useful to the engineer. That is the defence (`risks.md`
  #17).
- The dashboard check in CI (no pseudonym, no bronze or silver in `dashboards/`) stays as it is. The
  coach dashboard lives in its own directory, under its own rule: every query is filtered to one
  fixed pseudonym.
- The coach uses tier-1 data about one person, shown only to that person. It is not the tier-2
  opt-in depth of [ADR-0002](0002-three-tier-data-model.md), which would add what the person was
  working on.

## Rejected alternatives and why

- **A scorecard**, with your metrics and where you stand against your team. A percentile is a
  ranking, and a page with one score is what a manager asks to see in a one-to-one.
- **A per-person page anyone else can open, because it only shows a pseudonym.** It is re-identified
  by lining its daily cost up with the console.
- **Per-volunteer dashboards in the one Grafana, in a folder only admins can open.** A folder hides
  dashboards, not the data source behind them. Any login can still query per-person rows.
- **A pseudonym in the URL.** A pseudonym is not a secret; every per-person query returns them.
- **Command-line reports** (`cardo report coach`) handed to each volunteer. More to build than a
  link, and the operator is still in the loop.
- **Building delivery first**, by mail or chat. It builds the channel before anyone knows whether
  the content is worth sending.
- **No personal pages in the pilot.** Leaves the coach, and with it engineers' reason to welcome the
  tool, untested.
