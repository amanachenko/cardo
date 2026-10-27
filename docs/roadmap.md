# Roadmap — what gets built next

> **A plan, edited in place.** [`docs/design/architecture.md`](design/architecture.md) describes what
> exists, and the ADRs record decisions. This file is the order of work. **Status 2026-10-15:**
> Phases 0 to 2 are built, and nothing is in progress. What comes next is the
> [work items](#work-items) below, in three steps. The rule that sets the order, build what gets an
> organization to its first real decision soonest, is explained with an example in
> [where Cardo is going](vision.md#how-we-choose-what-to-build-next).

## Where things stand

Phases 0 to 2 are built: the Admin API poller, the collector path, the views and two dashboards.
The collector path has been checked against one engineer's real Claude Code sessions. Nothing has
run with a team, and the poller has never parsed a live Anthropic response
([risks.md](../risks.md) #12).

The Admin API poller is now part of the stack (ADR-0043): a `poller` service behind the `admin`
Compose profile, configured once from the board, so no operator machine needs a cron entry.

The quickstart is built: one binary, `cardo ui` for the board, `cardo up` for the same steps without
it, `cardo check` for a read-only pass, and `cardo check --proof` to require one real session with
the fields the views read. The diagnostic probe (`cardo probe`) proves the pipeline and deletes its
own rows, so no synthetic session ever reaches `silver_session`.

A made-up organization is built, so a view can be seen before any organization's data exists
([ADR-0051](adr/0051-a-made-up-organization.md)). It is 60 invented engineers in six teams, sent
through the collector on a stack of its own, with stories planted in their sessions. For example,
one engineer's `/deploy-check` command reaches a fifth person in week 5, and `check` confirms that
the enablement views show it as "other" until then and by name after. Nothing seen there is a
finding about real engineers, and what no view shows yet is listed in [the made-up
organization](made-up-organization.md#what-no-view-shows-yet).

[ADR-0030](adr/0030-stakeholders-and-questions.md) to [ADR-0037](adr/0037-policy-evidence.md)
decided who Cardo is for and what each of them needs, and
[the North Star decision (ADR-0042)](adr/0042-the-north-star.md) says what all of it is for. This
file turns that into work.

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
- **Whether `event.sequence` orders the tool results within a prompt.** A retry loop is the same
  tool failing again and again. The first count of them orders a session's calls by when Claude
  Code decided to run each one, and the sequence would check that order.
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

The order of work, one pull request each, in three steps. Each item says whether you can pick it
up:

- **Taken:** someone is building it. Ask in its pull request before starting on it.
- **Open:** ready to pick up. Say so in an issue or a draft pull request first, so that two people
  do not build the same thing.
- **Needs a decision:** a short design discussion comes first, in an issue. It may end in an ADR.
- **Waits for:** another item, or data from more than one person.

Nothing is taken today.

### Step 1: run it safely

Two decisions are already possible with what is built: chasing the machines still on an old version
of the organization's instructions, and spotting a command a team built that is spreading. What is
missing is an organization running Cardo safely, so this comes first.

- **A quickstart for one machine, and `cardo check`.** Run the stack, send your own sessions to it,
  and check in one command that everything the views rely on has arrived. **Built:** `cardo ui` for
  the board, `cardo up` for the same steps without it, `cardo check` for a read-only pass, and
  `cardo check --proof` to require one real session with the fields the views read. The proof is a
  real session, not a fixture.
- **Dashboard logins that cannot read anyone's personal rows.** The leaders' Grafana gets its own
  database user, which can read only the grouped views, with a live test that it is refused the
  raw and per-person tables ([risks.md](../risks.md) #15). This must be done before anyone except
  the operator gets a Grafana login. **Built:** `cardo gold` creates `cardo_ro`, the `leadership`
  database of SQL SECURITY DEFINER views, grants `leadership.*` only, and proves the refusal live;
  the `Cardo gold (read-only)` datasource uses it, and `cardo check` re-proves the boundary without
  writing anything. **Still missing:** that datasource sits in the same Grafana organization as the
  full-access one, and a login can query any data source in its organization. Leaders need a
  Grafana organization of their own that holds only the gold datasource, with a live test that a
  leader's login is refused the full-access one.
- **A notice an organization can hand to its engineers.** It says:
  - who holds the salt, the key that could reveal who is behind a code name;
  - which settings are on;
  - that Cardo's data is not for individual performance decisions.

  *Open.*

### Step 2: two tracks, side by side

#### The leaders' side: what should become standard?

*Lead:* the project's owner.

- **How each artifact arrived: rolled out to everyone, or picked up by choice**
  ([spread is a vote only for what teams chose](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md)).
  Example: "the review skill pushed to everyone on 3 November reaches 80% of people, but sessions
  that use it look no different from before". It needs:
  - the operator's rollout list, which is also the inventory for finding what nobody uses;
  - artifact views that show a rolled-out artifact's spread as reach, with a before-and-after
    comparison around the day it arrived;
  - the efficiency views noticing when one tool replaces another.

  *Needs a decision:* how the rollout list is kept and loaded. **Built in part:** the enablement
  catalog — versioned files on disk with their metadata in ClickHouse, validate → diff → draft →
  approve, retire or roll back ([ADR-0045](adr/0045-catalog-storage.md)). The views still decide
  which artifacts are the organization's own from the operator's list, so an approved version is not
  yet what the views call an organization's artifact.
- **Teams from a directory file** ([identity in pilots](adr/0034-identity-in-pilots.md)). Who is
  in which team, from an export the organization already has, so that a rollout that missed two
  teams shows up. *Needs a decision:* the file's format, and how it is loaded. **Built:** the console
  accepts `email,team`, hashes it on load (ADR-0046), and shows coverage and the group floor; a
  `cardo teams` CLI check remains.

#### The engineer's side: what does struggle look like, and how does a person find out?

*Lead:* a contributor who knows from experience what an agent getting stuck looks like.

- **The signs of struggle, first version** ([what "works" means](adr/0031-what-works-means.md)).
  Older documents call this the efficiency family. It replaces the friction panels, which the
  enablement dashboard's review took off on 2026-10-23, and shows none of it by team:
  - failed tool calls, split into shell commands and other tools;
  - retry loops: the same tool failing again and again. A first version is built: three or more
    failed calls of one tool in a row in a session, by tool
    ([failing calls repeated in a row](design/dashboards.md#failing-calls-repeated-in-a-row));
  - denials in auto mode;
  - conversations squeezed because they got too long (compactions);
  - turns and cost per accepted edit;
  - permission waits, for people in default mode;
  - how sessions across the organization go as the conversation grows.

  Before an organization's data is read, each claim to test is written down with the result that
  would prove it wrong
  ([spread is a vote only for what teams chose](adr/0040-spread-is-a-vote-only-for-what-teams-chose.md),
  sixth point). *Open.*
- **The code-name command, `cardo pseudonym <email>`**
  ([the coach by shared link](adr/0032-engineer-coach-by-shared-link.md),
  [identity in pilots](adr/0034-identity-in-pilots.md)). It prints one engineer's code name, so
  that the operator can make that engineer's private page. **Built:** prints the value and its salt
  version, read from the same salt the collector uses; the address is never stored.
- **The private page, figures only, and `cardo coach`**
  ([the coach by shared link](adr/0032-engineer-coach-by-shared-link.md)). It shows a person their
  own permission waits, compactions, retry loops and context size, each against their own past.
  A CI rule checks that every query is fixed to one code name, or reads only the grouped views.
  *Waits for* the code-name command. **Built:** `cardo coach <email>` prints the figures, and the
  console serves the same data as a page at `#/coach` (operator preview) and
  `/coach/<pseudonym>?k=…` (per-person link), with an eight-week trend
  ([ADR-0048](adr/0048-coach-in-the-console.md)). The per-person link is an **own-laptop setup
  only**: it opens on the machine that runs the console, and how engineers on a team reach their
  page is still open.
- **Its own dashboard server** ([the coach by shared link](adr/0032-engineer-coach-by-shared-link.md)).
  A second Grafana for the private pages, apart from the leaders' one. *Waits for* dashboard logins
  that cannot read anyone's personal rows. **Built differently:** the coach is a page in the
  console, not a second Grafana ([ADR-0048](adr/0048-coach-in-the-console.md)).
- **Suggestions on the private page**
  ([the coach suggests what the fleet has shown](adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)).
  Example: "11 people here use `/review`, and you haven't tried it yet". *Waits for* the signs of
  struggle, and how each artifact arrived. **Built:** one suggestion at a time, drawn only from what
  the fleet has shown, with the count of the people who use it.

The signs of struggle feed both tracks: leaders compare tools with them, and each engineer sees
their own.

### Step 3: later

- **The board report** ([who Cardo serves](adr/0030-stakeholders-and-questions.md),
  [what "works" means](adr/0031-what-works-means.md)):
  - cost per accepted change, money spent redoing work, and cost per approach;
  - each team's reach against the whole organization;
  - the same people's efficiency before and after they took up an approach.

  *Waits for* the signs of struggle, and for data from more than one team.
- **Proof that policy holds** ([policy evidence](adr/0037-policy-evidence.md)): whether machines
  conform, whether everyone using Claude Code is reporting, sessions signed in to other
  organizations, and the evidence report. The organization's approved connector list and minimum
  version are configuration. *Open.* For an organization that hasn't allowed Claude Code yet, this
  comes first ([the exception](vision.md#next)).
- **Protecting the collector from made-up data** ([risks.md](../risks.md) #19). Today anyone who
  can reach the collector can write to it. Done before Cardo serves a second team. *Needs a
  decision,* recorded in an ADR.
- **The automated-run label, and the CI settings block**
  ([service runs are not people](adr/0036-service-runs-are-not-people.md)). Runs in CI or in the
  background are labelled as a service, and never counted as a person. **Built:** actor kind and
  workflow from resource attributes, service runs kept out of the people views, a GitHub Actions
  block, and a check that fails on undocumented actor types.
- **Which of the organization's repositories have instructions files,** and the optional setting
  that records their names ([repository identity](adr/0035-repository-identity-classified.md)).
  *Open.*
- **Several laptops' data in one store: export, import and removal, with their ADR.** Only if an
  organization runs Cardo on laptops rather than on one shared server. The export is a file its
  owner can read before sending, with no repository names and no organization id. The import
  replaces by session, because the raw tables have no key, and a second import would double every
  count. *Waits for* an organization that runs it that way.

## Later, when an organization asks

The directions [where Cardo is going](vision.md#later-when-an-organization-asks) puts under
"later" come first, then smaller pieces. Each links the decision behind it:

- **Pull-request outcomes from the git host,** such as time to merge and reverts, per repository
  and never per person ([what "works" means](adr/0031-what-works-means.md)). It needs read access
  to the organization's repositories, and its priority rises with the first organization that
  wants to compare tools.
- **Other AI coding tools side by side** ([who Cardo serves](adr/0030-stakeholders-and-questions.md)).
- **Views for agents that run with nobody at the keyboard,** in CI or in the background
  ([service runs are not people](adr/0036-service-runs-are-not-people.md)).
- **The private, opt-in personal layer:** richer detail about what an engineer was working on,
  seen only by them ([thin mandatory core plus opt-in depth](adr/0007-enrollment-posture.md)). Not
  built, and nothing of that kind is collected today.
- **The adapter for organizations on Claude Enterprise seats,** whose usage data comes from a
  different Anthropic API. How much it covers depends on Anthropic's per-person data
  ([both analytics sources](adr/0021-analytics-source-scope.md)).
- **Sending security events to the organization's SIEM,** and naming security exceptions, once the
  policy report exists ([policy evidence](adr/0037-policy-evidence.md)).
- **The named cost view for budget owners**
  ([per-person cost for budget owners](adr/0033-per-person-cost-for-budget-owners.md)).
- **Handing the salt to the organization's security team**
  ([identity in pilots](adr/0034-identity-in-pilots.md)).
- **Delivering the private page by message, or through an app engineers sign in to,** if people
  act on its suggestions ([the coach by shared link](adr/0032-engineer-coach-by-shared-link.md)).
- **Summaries that outlive the 90-day retention, and faster views for a large organization**
  ([risks.md](../risks.md) #10 and #14).
