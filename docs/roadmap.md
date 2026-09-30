# Roadmap — the pilots, and what gets built for each

> **A plan, edited in place.** [`docs/design/architecture.md`](design/architecture.md) describes what
> exists, and the ADRs record decisions. This file is the order of work. **Status 2026-09-28:** the
> pilots and what gets built before each are agreed; none of it is built.

## Where things stand

Phases 0 to 2 are built: the Admin API poller, the collector path, the views and two dashboards.
The collector path has been checked against two real Claude Code sessions from one engineer.
Nothing has run with a team, and the poller has never parsed a live Anthropic response
([risks.md](../risks.md) #12).

The design review of 2026-09-25 decided who Cardo is for and what each of them needs
([ADR-0030](adr/0030-stakeholders-and-questions.md) to [ADR-0037](adr/0037-policy-evidence.md)).
This file turns that into pilots.

## The pilots, in order (decided)

### 1. Dogfood

- **Who:** five or more engineers, ideally one team, for two to three weeks. Five people in one team
  is the smallest group for which the team views show anything by name
  ([ADR-0029](adr/0029-minimum-group-size.md)).
- **Where:** a small machine inside their own network. Engineers join with the one `--settings` flag
  and install nothing.
- **What it must show:**
  - that setup works on several machines;
  - views with more than one person in them, for the first time;
  - whether anyone opens their coach page;
  - first numbers for the efficiency family;
  - whether the repository attribute arrives, and in what form (it has never been observed).
- **Operator-gated:** the machine, and the engineers' settings.

### 2. Adoption pilot

- **Who:** an organization already using Claude Code, with a champion inside it. 15 to 30 engineers
  in three or more teams, for four to six weeks.
- **Operator-gated:** installing in the organization's infrastructure, pushing settings to real
  machines, and the first run of the poller against the organization's admin key.
- **Success criteria (decided):**
  - **Leadership:** the adoption owner makes at least one decision from it, such as making a
    team-grown artifact official, retiring one, or fixing a rollout. Asked at the end.
  - **Engineers:** at least half the volunteers acted on a coach suggestion. A short
    before-and-after question, "do you feel more watched?", does not get worse.
  - **Data:** at least one home-grown artifact is seen spreading past five people. The efficiency
    signals either show differences we can explain, or we learn that they do not.
  - **Interviews at the end,** because numbers alone won't say why.
  - **Stop signal:** if nobody decides or acts on anything, the product is wrong in this form, and
    we find out why before building more.

### 3. Evidence pilot

- **Who:** an organization that currently bans Claude Code. A controlled group is allowed on condition
  of the evidence report ([ADR-0037](adr/0037-policy-evidence.md)).
- **What it must show:** that a risk committee accepts the report.

## What gets built before each (agreed 2026-09-28)

### Before the dogfood

- **The collector reachable over the network, with TLS in front of it.** The reference stack
  listens only on its own machine, and the engineers' Claude Code sends email addresses unhashed to the
  collector, which hashes them on arrival.
- **Repository identity, classified** ([ADR-0035](adr/0035-repository-identity-classified.md)), so
  that the dogfood observes the attribute.
- **The efficiency family, first version** ([ADR-0031](adr/0031-what-works-means.md)). It replaces
  the friction panels and drops the team breakdown from friction.
- **The coach dashboard, shared links and `cardo pseudonym`**
  ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md)). While only the operator has a Grafana
  login, the single Grafana is enough.

### Before the adoption pilot

- **A second Grafana for personal pages,** and a gold-only database user for the leadership
  Grafana ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md)). This must be done before anyone
  except the operator gets a login (`risks.md` #15).
- **Teams from a directory file** ([ADR-0034](adr/0034-identity-in-pilots.md)).
- **The service-run label and the CI settings block**
  ([ADR-0036](adr/0036-service-runs-are-not-people.md)).
- **The board report's views:**
  - unit cost, rework and cost per approach;
  - each team's reach against the fleet;
  - the within-person efficiency comparison.

  ([ADR-0030](adr/0030-stakeholders-and-questions.md), [ADR-0031](adr/0031-what-works-means.md))
- **Instructions presence per organization repository,** and the optional names setting
  ([ADR-0035](adr/0035-repository-identity-classified.md)).
- **The engineer notice for that organization:**
  - who holds the salt;
  - which settings are on;
  - that Cardo's data is not for individual performance decisions.

### During or after the adoption pilot

- **The git-host poller** ([ADR-0031](adr/0031-what-works-means.md)). It runs in the pilot if the
  organization grants read access to its repositories, and comes afterwards otherwise. Its priority
  rises with the first organization that wants to compare tools.

### Before the evidence pilot

- **Conformance, coverage and other-organization sessions,** and the evidence report
  ([ADR-0037](adr/0037-policy-evidence.md)). The organization's approved MCP list and minimum version are
  configuration.
- **The Enterprise Analytics adapter,** if the organization is on Enterprise seats: coverage depends on
  Anthropic's per-person data ([ADR-0021](adr/0021-analytics-source-scope.md)).

### Later, when an organization asks

- the named cost view for budget owners ([ADR-0033](adr/0033-per-person-cost-for-budget-owners.md));
- handing the salt to the organization's security team ([ADR-0034](adr/0034-identity-in-pilots.md));
- named security exceptions, and the SIEM feed ([ADR-0037](adr/0037-policy-evidence.md));
- coach delivery by message, or an authenticated app, if the pilot shows people act on suggestions
  ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md));
- views for autonomous agent runs ([ADR-0036](adr/0036-service-runs-are-not-people.md));
- adapters for other tools ([ADR-0030](adr/0030-stakeholders-and-questions.md));
- rollups that outlive the 90-day retention, and materialized views for a large fleet
  ([risks.md](../risks.md) #10, #14).
