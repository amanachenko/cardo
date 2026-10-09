# Roadmap — what gets built next

> **A plan, edited in place.** [`docs/design/architecture.md`](design/architecture.md) describes what
> exists, and the ADRs record decisions. This file is the order of work. **Status 2026-10-09:**
> Phases 0 to 2 are built. What comes next is the [work items](#work-items) below, in order.

## Where things stand

Phases 0 to 2 are built: the Admin API poller, the collector path, the views and two dashboards.
The collector path has been checked against one engineer's real Claude Code sessions. Nothing has
run with a team, and the poller has never parsed a live Anthropic response
([risks.md](../risks.md) #12).

[ADR-0030](adr/0030-stakeholders-and-questions.md) to [ADR-0037](adr/0037-policy-evidence.md)
decided who Cardo is for and what each of them needs. This file turns that into work.

## What Claude Code's telemetry has still to show

Some views rely on how Claude Code sends a field, and that has to be seen, not read in its
documentation. Each of these is checked before a view relies on it, and the answer goes in a dated
research note:

- **Where the repository arrives.** Seen on one machine: on each record, never on the resource
  ([note](research/2026-10-06-http-hooks-wait.md)). Still to see: the URL forms a team's remotes
  take, and whether any `external` host is not a public one, such as an internal git server the
  pattern misses. That is the evidence on which
  [ADR-0035](adr/0035-repository-identity-classified.md) would be revisited.
- **Whether `PermissionDenied` ever arrives.** It does, on an auto-mode denial
  ([note](research/2026-10-06-first-permission-denied.md)).
- **Whether `event.sequence` orders the tool results within a prompt.** A retry loop, the same
  tool failing again and again in one prompt, is counted from it.
- **Whether `claude_code.lines_of_code.count` arrives by `type` for each session, as delta.**

## How a pilot judges Cardo

Some of what Cardo claims can only be judged with people using it. A pilot at an organization
judges it by these, decided before the pilot starts:

- **Leadership:** the adoption owner makes at least one decision from it, such as making a
  team-grown artifact official, retiring one, or fixing a rollout. Asked at the end.
- **Engineers:** at least half the volunteers acted on a coach suggestion. A short
  before-and-after question, "do you feel more watched?", does not get worse.
- **Data:** at least one home-grown artifact is seen spreading past five people. The efficiency
  signals either show differences that can be explained, or it turns out that they do not.
- **Interviews at the end,** because numbers alone won't say why.
- **Stop signal:** if nobody decides or acts on anything, the product is wrong in this form, and
  the reason is found before more is built.

[`docs/overview.md`](overview.md#trying-it-in-your-organization) describes how an organization can
run one.

## Work items

The order of work, one pull request each. Each item says whether you can pick it up:

- **Taken:** someone is building it. Ask in its pull request before starting on it.
- **Open:** ready to pick up. Say so in an issue or a draft pull request first, so that two people
  do not build the same thing.
- **Needs a decision:** a short design discussion comes first, in an issue. It may end in an ADR.
- **Waits for:** another item, or data from more than one person.

1. **The dashboard guards check every panel.** The INV-3 and volume tests read only top-level
   panels. A panel inside a row, or a template variable's query, is not checked. *Taken.*
2. **A quickstart for one machine, and `cardo check`:** run the stack, send your own sessions to
   it, and check in one command that what the views rely on has arrived. *Taken.*
3. **`cardo pseudonym <email>`** ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md),
   [ADR-0034](adr/0034-identity-in-pilots.md)). *Taken.*
4. **The coach, figures only, and `cardo coach`**
   ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md),
   [ADR-0041](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)).
   - **What it shows:** permission waits, compactions, retry loops and context size, each against
     the person's own past.
   - **A CI rule:** every query is fixed to one pseudonym, or reads gold only.

   *Taken; waits for 3.*
5. **Several machines' data in one store: export, import and removal, with their ADR.** The export
   is a file its owner can read before sending, with no repository names and no organization id.
   The import replaces by session, because the bronze tables have no key, and a second import would
   double every count. *Taken.*
6. **The efficiency family, first version** ([ADR-0031](adr/0031-what-works-means.md)). It replaces
   the friction panels and drops the team breakdown from friction:
   - failures, split into shell and other tools;
   - retry loops;
   - auto-mode denials;
   - compactions;
   - turns and cost per accepted edit;
   - permission waits for people in default mode;
   - the fleet's efficiency by context size.

   *Taken.*
7. **A gold-only ClickHouse user** for the leadership Grafana, with a live test that it is refused
   bronze and silver ([risks.md](../risks.md) #15). This must be done before anyone except the
   operator gets a Grafana login. *Open.*
8. **A second Grafana for personal pages**
   ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md)). *Open; waits for 7.*
9. **Teams from a directory file** ([ADR-0034](adr/0034-identity-in-pilots.md)). *Needs a
   decision:* the file's format, and how it is loaded.
10. **The service-run label and the CI settings block**
    ([ADR-0036](adr/0036-service-runs-are-not-people.md)). *Open.*
11. **How each artifact arrived** ([ADR-0040](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)):
    - the operator's rollout list, which is also the inventory for finding what nobody uses;
    - artifact views that label a rolled-out artifact's spread as reach, with the before-and-after
      comparison around its date;
    - substitution in the efficiency views;
    - the efficiency claims to test, written down with what would refute each, before the data is
      read.

    *Needs a decision:* the mechanism.
12. **Coach suggestions drawn from the artifact views**
    ([ADR-0041](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)). *Waits for 6 and 11.*
13. **The board report's views** ([ADR-0030](adr/0030-stakeholders-and-questions.md),
    [ADR-0031](adr/0031-what-works-means.md)):
    - unit cost, rework and cost per approach;
    - each team's reach against the fleet;
    - the within-person efficiency comparison.

    *Waits for 6, and for data from more than one team.*
14. **Instructions presence per organization repository,** and the optional names setting
    ([ADR-0035](adr/0035-repository-identity-classified.md)). *Open.*
15. **An engineer notice an organization can hand out:**
    - who holds the salt;
    - which settings are on;
    - that Cardo's data is not for individual performance decisions.

    *Open.*
16. **Collector authentication** ([risks.md](../risks.md) #19). *Needs a decision,* recorded in an
    ADR.
17. **Policy evidence:** conformance, coverage, other-organization sessions, and the evidence report
    ([ADR-0037](adr/0037-policy-evidence.md)). The organization's approved MCP list and minimum
    version are configuration. *Open.*

## Later, when an organization asks

- the git-host poller ([ADR-0031](adr/0031-what-works-means.md)), which needs read access to the
  organization's repositories. Its priority rises with the first organization that wants to
  compare tools;
- the Enterprise Analytics adapter, for an organization on Enterprise seats: coverage depends on
  Anthropic's per-person data ([ADR-0021](adr/0021-analytics-source-scope.md));
- the named cost view for budget owners ([ADR-0033](adr/0033-per-person-cost-for-budget-owners.md));
- handing the salt to the organization's security team ([ADR-0034](adr/0034-identity-in-pilots.md));
- named security exceptions, and the SIEM feed ([ADR-0037](adr/0037-policy-evidence.md));
- coach delivery by message, or an authenticated app, if people act on its suggestions
  ([ADR-0032](adr/0032-engineer-coach-by-shared-link.md));
- views for autonomous agent runs ([ADR-0036](adr/0036-service-runs-are-not-people.md));
- adapters for other tools ([ADR-0030](adr/0030-stakeholders-and-questions.md));
- rollups that outlive the 90-day retention, and materialized views for a large fleet
  ([risks.md](../risks.md) #10, #14).
