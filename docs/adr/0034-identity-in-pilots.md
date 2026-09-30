# ADR-0034 — In the pilots, pseudonyms stay, the operator holds the salt, and teams come from a directory

**Status:** Accepted
**Date:** 2026-09-25
**Evidence:** `2026-09-25-grafana-access-and-shared-links.md` (what any Grafana login can read);
`2026-09-23-admin-analytics-apis.md` (no team field). Otherwise judgment.

Narrows [ADR-0006](0006-pseudonymization.md) on who holds the salt during pilots. INV-2 is
unchanged.

## Context

Several decisions now need to know who a person is:

- a coach page is made for one volunteer ([ADR-0032](0032-engineer-coach-by-shared-link.md));
- a budget view would name people ([ADR-0033](0033-per-person-cost-for-budget-owners.md));
- security wants named exceptions ([ADR-0037](0037-policy-evidence.md)).

A simpler option was proposed for the pilots: **store emails beside pseudonyms.** The views would
not show them, identity protection could wait for the next stage, and organizations could be asked
later whether they wanted names hidden from them. The worry behind it is real: complexity that adds
friction to adopting the tool.

Two further facts:

- ADR-0006 has the organization's security team generate and hold the salt. Before a pilot can
  start, that means a meeting, an owner and a delay.
- A team is a label in the settings bundle (`cardo.cohort`), so it takes one bundle per MDM group.
  Anthropic's usage data has no team field at all.

## Decision

**1. INV-2 holds in the pilots.** No email is stored, anywhere.

**2. During the pilots, the operator generates and holds the salt.** Handing it to the
organization's security team becomes something Cardo offers when an organization asks. It is no
longer a precondition for starting.

**3. Teams come from a directory file**, falling back to the bundle label.

- The file is exported from HR or the identity provider and maps each email to a team, with the
  dates of each assignment.
- It is converted to pseudonym-to-team at ingest.
- The dates keep last quarter's numbers on last quarter's team.
- It also gives Anthropic's per-person data a team, which it has no other way to get.
- The five-person rule ([ADR-0029](0029-minimum-group-size.md)) applies to these teams as it does
  to bundle labels.

**4. Names exist only when Cardo hands something to someone.** In the pilots that is `cardo
pseudonym`, used to make a volunteer's coach page. Nothing named is stored in ClickHouse or shown
on a dashboard.

**5. Offered to an organization that asks:**

- handing the salt to their security team;
- the named cost view ([ADR-0033](0033-per-person-cost-for-budget-owners.md));
- named exceptions for security ([ADR-0037](0037-policy-evidence.md)).

With pseudonyms as the default, an organization that wants names can have them. The reverse would
make protection something an organization opts into.

## Consequences

- **During a pilot the operator can re-identify anyone,** because the salt and the directory file
  are both in their hands. That was already true of whoever runs the collector, which needs the
  salt. The engineer notice says who holds it.
- The directory file is identified data. It is read at ingest and kept outside ClickHouse; only
  the pseudonym-to-team table is stored.
- Cardo keeps its strongest checkable claim in front of an organization's security team,
  data-protection officer or works council: no email reaches the database, and here is the test that
  proves it.
- The stated worry, complexity, is met by building almost nothing else for identity until an
  organization asks for it.

## Rejected alternatives and why

- **Store emails beside pseudonyms for the pilots.** Rejected for three reasons:
  - **It is more work, not less.** Pseudonymization is built. Storing emails means removing what
    was built to refuse them: the collector's and the poller's tripwires, and the CI tests that
    guard INV-2. The complexity it meant to avoid came from per-person pages, and storing emails
    does not remove it.
  - **"Not visible through the views" does not hold in Grafana.** Any login can query whatever the
    data source can read, so the first leadership login would reach every person's detail, by
    name.
  - **It makes the security review harder.** "Emails are stored but not shown" starts a review of
    access controls that have not been built. GDPR names pseudonymisation explicitly as a safeguard.
    In Germany and countries with similar rules, a works council must approve any system able to
    monitor employees, and those agreements usually ask for exactly this. It would also leave the
    choice to leadership, the side the protection guards against, with engineers never asked.
- **The organization's security team holds the salt from day one**, as ADR-0006 designed. That is
  right for a deployment and a delay for a pilot. It is now the organization's choice.
- **Teams only from the bundle label.** MDM groups are rarely the org chart, and Anthropic's
  per-person data would get no team.
- **A directory without dates.** Every reorganization would rewrite history.
