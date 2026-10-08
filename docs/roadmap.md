# Roadmap — the pilots, and what gets built for each

> **A plan, edited in place.** [`docs/design/architecture.md`](design/architecture.md) describes what
> exists, and the ADRs record decisions. This file is the order of work. **Status 2026-10-08:** the
> pilots and what gets built before each are agreed, and an early local trial now comes first.
> Everything the dogfood must collect from its first day is built. Its views and the coach are
> being built ahead of it, in the order of the [work items](#work-items) at the end, which also
> say what is open for contributors.

## Where things stand

Phases 0 to 2 are built: the Admin API poller, the collector path, the views and two dashboards.
The collector path has been checked against one engineer's real sessions, on Claude Code 2.1.281 to
2.1.293. Nothing has run with a team, and the poller has never parsed a live Anthropic response
([risks.md](../risks.md) #12).

The design review of 2026-09-25 decided who Cardo is for and what each of them needs
([ADR-0030](adr/0030-stakeholders-and-questions.md) to [ADR-0037](adr/0037-policy-evidence.md)).
This file turns that into pilots.

## The pilots, in order (decided)

### 1. Early local trial

- **Who:** fewer than five engineers from one organization, while the dogfood is arranged.
- **Where:** each on their own machine. They run the reference stack locally with the
  local-evaluation settings and a salt of their own, and their data stays there.
- **What it must show:**
  - that a local install works on other machines and operating systems;
  - whether anyone opens their own coach page;
  - the first-week checks below, from more than one machine and version;
  - first fleet-level numbers for the efficiency family.
- **Merging is opt-in, by file.** A participant may export their data to a file they can read
  before sending it. The export carries no repository names and no organization id. The operator
  imports it into one store, so the views can be checked on more than one person's data. An ADR
  comes with the export (work item 6).
- **What it cannot show:**
  - anything by team or by home-grown artifact name, which needs five people
    ([ADR-0029](adr/0029-minimum-group-size.md)). Merged results are for checking the views, and
    are reported across the group;
  - the network path and its round trip;
  - whether a team acts on what it sees.

### 2. Dogfood

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
  - the forms a team's repository URLs take (seen only from one machine so far).
- **Checked in the first week,** across people. Most were answered on one machine first, so the
  views that rely on them are built ahead and the dogfood confirms them:
  - whether Claude Code puts the repository on the resource or on each record. **On one machine:**
    on each record, never the resource
    ([note](research/2026-10-08-first-week-checks-on-one-machine.md));
  - whether any `external` host is not a public one, such as an internal git server the
    pattern misses. That is the evidence on which
    [ADR-0035](adr/0035-repository-identity-classified.md) would be revisited;
  - whether `PermissionDenied` ever arrives. **Answered 2026-10-06:** it does, on an auto-mode
    denial ([note](research/2026-10-06-first-permission-denied.md));
  - whether `event.sequence` orders the tool results within a prompt, which a retry loop (the same
    tool failing again and again in one prompt) is counted from. **On one machine:** it does, as a
    counter per session ([note](research/2026-10-08-first-week-checks-on-one-machine.md));
  - whether `claude_code.lines_of_code.count` arrives by `type` for each session, as delta. **On
    one machine:** it does ([note](research/2026-10-08-first-week-checks-on-one-machine.md)).
- **Operator-gated:** the machine, its DNS records and certificate, the salt, the settings file
  and the one-laptop check ([runbook](../deploy/compose/README.md#serving-a-team-over-the-network)).

### 3. Adoption pilot

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

### 4. Evidence pilot

- **Who:** an organization that currently bans Claude Code. A controlled group is allowed on condition
  of the evidence report ([ADR-0037](adr/0037-policy-evidence.md)).
- **What it must show:** that a risk committee accepts the report.

## What gets built before each (agreed 2026-09-28, the early trial 2026-10-08)

### Before the early local trial

- **A kit for one engineer on their own machine:**
  - a quickstart from clone to their own coach page;
  - `cardo check`, the first-week checks as one command that prints counts only;
  - a notice saying what is collected, how to stop, and how to delete it all.
- **The coach, figures only, made in one command** (`cardo pseudonym`, `cardo coach`).
- **Export, import and removal,** with the ADR that bounds them. These are not needed on the first
  day: a participant's data stays in their own store for 90 days
  ([ADR-0016](adr/0016-retention.md)), so it can be exported later.

### Before the dogfood

Only collection has to be right on the first day. What is not collected then is lost, and
changing it means handing every engineer a new settings file. The collector path's silver and gold
are plain views over bronze, so a view reads the dogfood's data from its start whenever it is
built. The views are built ahead, on one engineer's data and then the early trial's, and the
dogfood checks them across a team.

- **The collector reachable over the network, with TLS in front of it.** The reference stack
  listens only on its own machine, and the engineers' Claude Code sends email addresses unhashed
  to the collector, which hashes them on arrival. **Built:** an opt-in overlay of the reference
  stack, and the operator's [runbook](../deploy/compose/README.md#serving-a-team-over-the-network).
- **Repository identity, classified** ([ADR-0035](adr/0035-repository-identity-classified.md)), so
  that the dogfood observes the attribute. **Collection built;** no view reads it yet.
- **The efficiency family, first version** ([ADR-0031](adr/0031-what-works-means.md)). It replaces
  the friction panels and drops the team breakdown from friction. **Collection built:** it reads
  only what was already collected, and the bundle now pins metrics to delta. Its views are built
  ahead (work item 7), without churn: a session's counts of lines added and removed cannot say
  which lines were removed ([note](research/2026-10-08-first-week-checks-on-one-machine.md)).
- **The coach dashboard, shared links and `cardo pseudonym`**
  ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md)). While only the operator has a Grafana
  login, the single Grafana is enough. Built ahead for the early trial, showing figures only.
  Suggestions come with the artifact views they are drawn from
  ([ADR-0041](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)), and start no earlier than
  a volunteer's second week.

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
- **How each artifact arrived** ([ADR-0040](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)):
  - the operator's rollout list, which is also the inventory for finding what nobody uses;
  - artifact views that label a rolled-out artifact's spread as reach, with the before-and-after
    comparison around its date;
  - substitution in the efficiency views;
  - the efficiency claims the pilot will test, written down with what would refute each, before
    its data is read.
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

## Work items

The order of work, one pull request each. Each item says whether you can pick it up:

- **Taken:** someone is building it. Ask in its pull request before starting on it.
- **Open:** ready to pick up. Say so in an issue or a draft pull request first, so that two people
  do not build the same thing.
- **Needs a decision:** a short design discussion comes first, in an issue. It may end in an ADR.
- **Waits for:** another item, or data from a trial.

Items further out stay in the sections above until they move into this list.

1. **This list, and three first-week checks answered on one machine**
   ([note](research/2026-10-08-first-week-checks-on-one-machine.md)). *Taken.*
2. **The dashboard guards check every panel.** The INV-3 and volume tests read only top-level
   panels. A panel inside a row, or a template variable's query, is not checked. *Taken.*
3. **The early trial's kit:** the quickstart, `cardo check` and the participant notice. *Taken.*
4. **`cardo pseudonym <email>`** ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md),
   [ADR-0034](adr/0034-identity-in-pilots.md)). *Taken.*
5. **The coach, figures only, and `cardo coach`**
   ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md),
   [ADR-0041](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)).
   - **What it shows:** permission waits, compactions, retry loops and context size, each against
     the person's own past.
   - **A CI rule:** every query is fixed to one pseudonym, or reads gold only.

   *Taken; waits for 4.*
6. **Export, import and removal, with their ADR.** The export is a file its owner can read before
   sending, with no repository names and no organization id. The import replaces by session,
   because the bronze tables have no key, and a second import would double every count. *Taken.*
7. **The efficiency family, first version** ([ADR-0031](adr/0031-what-works-means.md)):
   - failures, split into shell and other tools;
   - retry loops;
   - auto-mode denials;
   - compactions;
   - turns and cost per accepted edit;
   - permission waits for people in default mode;
   - the fleet's efficiency by context size.

   *Taken.*
8. **A gold-only ClickHouse user** for the leadership Grafana, with a live test that it is refused
   bronze and silver ([risks.md](../risks.md) #15). *Open.*
9. **A second Grafana for personal pages**
   ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md)). *Open; waits for 8.*
10. **Teams from a directory file** ([ADR-0034](adr/0034-identity-in-pilots.md)). *Needs a
    decision:* the file's format, and how it is loaded.
11. **The service-run label and the CI settings block**
    ([ADR-0036](adr/0036-service-runs-are-not-people.md)). *Open.*
12. **How each artifact arrived** ([ADR-0040](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)):
    the rollout list, spread labelled as reach, and substitution. *Needs a decision:* the
    mechanism.
13. **Coach suggestions drawn from the artifact views**
    ([ADR-0041](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)). *Waits for 7 and 12.*
14. **The board report's views** ([ADR-0030](adr/0030-stakeholders-and-questions.md),
    [ADR-0031](adr/0031-what-works-means.md)). *Waits for 7, and for data from a trial.*
15. **Instructions presence per organization repository,** and the names setting
    ([ADR-0035](adr/0035-repository-identity-classified.md)). *Open.*
16. **An engineer notice for an adopting organization:**
    - who holds the salt;
    - which settings are on;
    - that the data is not for performance decisions.

    *Open; builds on 3's participant notice.*
17. **Collector authentication** ([risks.md](../risks.md) #19). *Needs a decision,* recorded in an
    ADR.
