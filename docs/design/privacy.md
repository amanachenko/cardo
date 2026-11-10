# Privacy — what Cardo collects and what it does not

> Edited in place; describes reality. **Status 2026-10-26:** both collection paths are built: the
> Admin API poller, and the managed-settings bundle with its collector. So are the views and
> dashboards over what they store, the operator's console with its coach page, and the import
> command. The collector has been run against a real Claude Code, and the list below is what it
> kept. Changes decided and not yet built are listed separately, under "Decided, not built
> yet". This document is written to be readable by the engineers being measured, not only by the
> team deploying it.

## The short version

- Cardo **never reads your session transcripts on its own** — not for debugging, not once (INV-1).
  The one exception is the import command you run yourself, which keeps counts and throws the text
  away ([ADR-0050](../adr/0050-local-transcript-import.md)).
- Cardo **never stores your email address**. It is hashed before anything is written down (INV-2).
- Cardo **never stores your prompts, your code, or the paths of files you touched** (INV-5).
- **Nobody except you can see your individual activity.** Everyone else sees team-level aggregates
  (INV-3).
- **Cardo's data is not for individual performance decisions.** It ranks nobody, and it computes
  no per-head figure such as "PRs per engineer" ([ADR-0030](../adr/0030-stakeholders-and-questions.md)).
- **No data leaves your organization's network** (INV-7).

## What is collected without asking (tier 1)

Short by design, and this list is the boundary — adding to it requires a recorded decision (INV-4).

- session started and ended, and how long it ran
- which model and effort level, and whether you were in plan mode
- tokens and cost
- tool *classes* invoked (e.g. "a file was edited"), never the arguments
- whether an edit suggestion was accepted or rejected
- permission prompt outcomes and how long they took, and when you switched permission mode (plan
  mode included)
- context compaction events
- when you switched model, and Claude Code's own estimate of what the switch cost
- the **length** of each prompt, never its text
- which *kind* of instructions file loaded (`CLAUDE.md`, a rule file, ...), whether it was your
  organization's managed one, and, for your organization's own files only, the file's name (such
  as `acme-standards-v7.md`). **Never where it is on your disk**, and never the name of a file of
  yours
- the skills, slash commands, subagents and MCP servers you used, **by name, including ones you
  wrote yourself** (below)
- your team, as set in the bundle your organization deployed
- your team from your organization's directory, if it uploads one in the console: which team
  your code name belongs to. The directory's email addresses are hashed as it is read and never
  stored ([ADR-0046](../adr/0046-hashed-team-directory.md))
- whether a session was a service run, such as one in CI, as labelled by the run's own settings.
  A service run is never counted as a person
  ([ADR-0036](../adr/0036-service-runs-are-not-people.md),
  [ADR-0054](../adr/0054-both-actor-types-are-people.md))
- **which repository you worked in**, if it is one of your organization's, by name (such as
  `github.com/acme/widgets`). Any other repository is recorded only as "external" plus its host,
  such as `github.com`, and **never by its full address**
  ([ADR-0035](../adr/0035-repository-identity-classified.md)). Your organization lists its own
  repositories in `CARDO_ORG_REPOS`; until it does, every repository is "external". Nothing is
  recorded when you work outside a repository. No view shows repositories yet, and a real Claude
  Code has not yet been seen sending one

**About names.** Cardo records the names of commands, subagents and MCP servers that you defined
yourself. **That is more than Claude Code's own telemetry sends by default**, and it is deliberate
([ADR-0025](../adr/0025-artifact-names-kept-with-guardrails.md)). When several people build the same
thing for themselves, that is how the organization learns what it should make official for
everyone. Three rules come with it:

- A name is shown in any view only once enough people used it in the same week (five by default; the
  organization may raise that). A command
  that only you use never appears by name; it is counted as "other". Your organization can raise
  the five, never lower it ([ADR-0029](../adr/0029-minimum-group-size.md)).
- No view counts anyone's own commands per person.
- Your organization can switch to the strict mode (`CARDO_ARTIFACT_NAMES=org-only`), in which every
  name that is not the organization's own is recorded as `custom`.

**About your team.** The same rule covers the team name in the bundle. A team with fewer than five
people working in a chart's period, a day on a daily chart and a week on a weekly one, is shown as
"other" for that day or week, so a chart of a small team is never two or three people's numbers.
Totals for the whole organization are always shown. A team from the directory with fewer than
five people is not named either.

The exact list is the allowlist in
[`deploy/collector/config.yaml`](../../deploy/collector/config.yaml), and a CI test fails if it
changes without a recorded decision (INV-4).

## What your Claude Code sends that Cardo throws away

Claude Code's hooks hand their full input to whatever receives them, and that input includes your
prompt text, the commands and file contents of tool calls, subagent output, and your working
directory. It crosses your organization's network to the collector. **The collector discards all of
it before anything is written**, and keeps only the list above
([ADR-0023](../adr/0023-hook-payload-allowlist.md)). It also drops your Anthropic account
identifiers, your machine's hostname, and the full address of the repository you work in. It keeps
your organization's Anthropic ID, which is the same for everyone in the organization.

For any field it did not keep, the collector records the field's *name*, never its value. That is
how a new field in a future Claude Code release gets noticed without its contents being stored.

## What installing Cardo changes on your machine

Your organization deploys one JSON file ([`deploy/managed-settings/`](../../deploy/managed-settings/)).
It turns on Claude Code's own telemetry and adds twelve hooks. It sets nothing else. Your own hooks,
plugins, permissions and version all stay as they were
([ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md)).

**Claude Code waits for each hook's answer.** From a working collector that takes a few
milliseconds. Each hook gives up after one second, so a collector that is down or out of reach
costs each event at most that, and shows a hook error or a timeout notice under your message. Your
session carries on either way, and that event is not recorded. No hook can block a prompt, a
permission or a model switch.

One side effect is Claude Code's behaviour, and cannot be avoided: when an organization sets the
telemetry endpoint centrally, any telemetry endpoint you had configured yourself stops being used.

## Your own coach page

The console has a coach page for each person ([ADR-0048](../adr/0048-coach-in-the-console.md),
[ADR-0032](../adr/0032-engineer-coach-by-shared-link.md)). It compares your figures for a period
with the period before: sessions and prompts, cost at list price, the largest context, tool calls
and failures, and what you loaded. It may add one suggestion, and only of something enough people
here use (five by default) that you have not tried. It has no score and no ranking, and it shows
nobody else's figures. On a stack with fewer people than that, such as your own install, it
suggests nothing.

**The person who runs the stack can open any person's page** from the console, and can give you
yours as a link that opens on that machine. You should be told who that person is.

## Decided, not built yet

Recorded here so you know what is coming before it arrives. Each links to the decision behind it.

- **Which instructions files your sessions loaded in your organization's repositories:** whether
  any loaded, and how many. Your organization can choose to record the names of project-level
  files, and this document will say if it has. The names of your own personal files are never
  recorded ([ADR-0035](../adr/0035-repository-identity-classified.md)).
- **Only if your organization asks for it:** a budget owner may see the cost of people who are
  over a budget published to you. It shows only what Anthropic's console already shows them, and
  you see your own position first ([ADR-0033](../adr/0033-per-person-cost-for-budget-owners.md)).
  That would be the one exception to "nobody except you".

## What requires you to opt in (tier 2)

**Not built, and nothing of this kind is collected today.** It would cover anything touching *what
you were working on*, and opting in would get you a dashboard of your own, visible only to you. It
is built only when an organization asks for it
([where Cardo is going](../vision.md#later-when-an-organization-asks)), and this document will
describe it before anything is collected.

## What the security tier collects (tier 0)

**Not installed by default.** It is a separate module, and its arrival is a deliberate, announced act
([ADR-0003](../adr/0003-efficiency-before-security.md)). When present it collects policy decisions, MCP
server inventory, the full address of repositories outside your organization, denied tool calls,
login and org identity, and bypass-mode usage — for the security team only, under the same
governance as endpoint detection tooling. **No prompts and no code** ([ADR-0002](../adr/0002-three-tier-data-model.md)).

## Redaction settings, in full

Claude Code redacts content by default. Cardo keeps every one of those defaults. The shipped
managed-settings bundle sets all of these to `0`:

| Flag | What it would have enabled had we set it |
|---|---|
| `OTEL_LOG_USER_PROMPTS` | the full text of everything you type |
| `OTEL_LOG_ASSISTANT_RESPONSES` | the full text of every model response |
| `OTEL_LOG_TOOL_DETAILS` | tool commands, parameters and file paths |
| `OTEL_LOG_RAW_API_BODIES` | complete API request and response JSON |
| `OTEL_LOG_TOOL_CONTENT` | Read/Write/Edit file contents in trace spans |
| `OTEL_LOG_MANAGED_SETTINGS` | your organization's resolved managed settings |

One qualification. `OTEL_LOG_TOOL_DETAILS` is also documented as controlling the names of your own
commands and MCP servers. Claude Code sends the name of a custom slash command on its cost metrics
regardless, and Cardo records these names from the hooks by design (see *About names* above).

You can verify this yourself: the bundle is a readable JSON file, and there is no binary on your machine
to inspect ([ADR-0011](../adr/0011-hook-transport-http.md)).

## Pseudonymization, stated honestly

Your email is hashed with a salt before storage. **This is pseudonymization, not anonymization.** While
the salt exists, the data is still personal data under GDPR — it can be the subject of an access or
deletion request.

**Who holds the salt.** It depends on the stage:

- **In a deployment,** Cardo's design has your security team hold it, not the team running the
  analytics. Re-identification then requires going to them, which is a real access boundary that
  produces an audit trail.
- **In a pilot,** the person running the pilot holds it, so that a pilot does not wait on a
  handover ([ADR-0034](../adr/0034-identity-in-pilots.md)). They could re-identify anyone taking
  part, and you should be told who they are. Your organization can hand the salt to its security
  team afterwards.

**Because the salt is stable in v0.1, retention is the actual privacy control** — not the hashing
([ADR-0016](../adr/0016-retention.md), and the window itself
[ADR-0053](../adr/0053-retention-default-and-organization-choice.md)):

| Tier | Retention |
|---|---|
| Security (T0) | 1 year |
| Fleet (T1) | 90 days by default; your organization may change it |
| Personal (T2) | 30 days |
| Aggregates with no pseudonym, each describing at least five people | indefinite |

## The deletion path, written down

[ADR-0016](../adr/0016-retention.md) says a deletion request can be honoured because the salt
exists, and that the procedure must be written down rather than improvised. This is that
procedure, and each step names what it removes.

**1. The person's sessions, and the rows credited to them, are listed first.** Once their OTel rows
are gone, their hook rows can no longer be tied to them: a hook row carries no identity at all,
only the session id. A session can also carry two accounts, when someone switches Claude accounts in
the middle of it ([a session can carry more than one account, ADR-0059](../adr/0059-a-session-can-carry-more-than-one-account.md)).
A row with no code name of its own is theirs only if the pages credit it to them, because it came
while their account was active. So the rows credited to them are written down before anything is
deleted, in a table that step 2 drops.

```sql
SELECT DISTINCT session_id FROM cardo.silver_session_live
    WHERE pseudonym = '<pseudonym>' FORMAT TSV;

-- the rows without a code name that the pages credit to them: hook rows, and OTel events that
-- came without an email
CREATE TABLE cardo.deletion_credited ENGINE = MergeTree ORDER BY tuple() AS
SELECT 'hook' AS kind, r.LogAttributes['session_id'] AS session_id, r.Timestamp AS at,
       cityHash64(r.LogAttributes) AS attributes
FROM cardo.bronze_hook_events AS r
ASOF JOIN cardo.silver_session_account_live AS a
    ON a.session_id = r.LogAttributes['session_id'] AND r.Timestamp >= a.active_from
WHERE r.LogAttributes['session_id'] IN (<sessions>) AND a.pseudonym = '<pseudonym>'
UNION ALL
SELECT 'otel', r.LogAttributes['session.id'], r.Timestamp, cityHash64(r.LogAttributes)
FROM cardo.bronze_otel_logs AS r
ASOF JOIN cardo.silver_session_account_live AS a
    ON a.session_id = r.LogAttributes['session.id'] AND r.Timestamp >= a.active_from
WHERE r.LogAttributes['session.id'] IN (<sessions>)
  AND r.LogAttributes['user.pseudonym'] = '' AND r.ResourceAttributes['user.pseudonym'] = ''
  AND a.pseudonym = '<pseudonym>';
```

For example, a session that started on someone's work account and moved to their personal account
at 14:20: deleting the personal account deletes its events and the hook rows from 14:20 on, and the
work account's part of the session stays, because it is the work account's history.

**2. Everything that carries the pseudonym, and everything credited to it, deleted by the field each
table actually carries.** The collector puts the pseudonym wherever the email arrived: on the
resource, on the event record or on the metric point. `bronze_hook_events` carries no identity at
all, and `bronze_actor_day` has a `pseudonym` column and no resource. The team directory, if your
organization uploaded one, keeps one line per code name in `settings_teams`. Deleting by one field
alone leaves the rest in the store.

```sql
-- bronze_otel_logs: the pseudonym on the record or on the resource, and the events without one
-- that step 1 credited to them
ALTER TABLE cardo.bronze_otel_logs DELETE WHERE
    LogAttributes['user.pseudonym'] = '<pseudonym>'
    OR ResourceAttributes['user.pseudonym'] = '<pseudonym>'
    OR (LogAttributes['session.id'], Timestamp, cityHash64(LogAttributes)) IN
       (SELECT session_id, at, attributes FROM cardo.deletion_credited WHERE kind = 'otel')
    SETTINGS mutations_sync = 1;

-- bronze_otel_metrics_sum: the pseudonym on the data point or on the resource
ALTER TABLE cardo.bronze_otel_metrics_sum DELETE WHERE
    Attributes['user.pseudonym'] = '<pseudonym>'
    OR ResourceAttributes['user.pseudonym'] = '<pseudonym>'
    SETTINGS mutations_sync = 1;

-- bronze_hook_events: no identity, so the rows step 1 credited to them
ALTER TABLE cardo.bronze_hook_events DELETE WHERE
    (LogAttributes['session_id'], Timestamp, cityHash64(LogAttributes)) IN
    (SELECT session_id, at, attributes FROM cardo.deletion_credited WHERE kind = 'hook')
    SETTINGS mutations_sync = 1;

-- bronze_actor_day: its own pseudonym column
ALTER TABLE cardo.bronze_actor_day DELETE WHERE
    pseudonym = '<pseudonym>' SETTINGS mutations_sync = 1;

-- settings_teams: the directory's line for them, in every upload kept
ALTER TABLE cardo.settings_teams DELETE WHERE
    pseudonym = '<pseudonym>' SETTINGS mutations_sync = 1;

-- the list from step 1, which names their sessions
DROP TABLE cardo.deletion_credited;
```

Every figure Cardo shows is computed from those rows, so your absence changes the numbers rather
than hiding inside them. The directory line comes back with the next upload that lists you, so your
organization removes you from its directory file too.

**3. Stored silver is rebuilt, on its own within ten minutes.** The silver tables hold what the
last rebuild saw, so the person's rows are rebuilt away — and a trend recomputed after that cannot
put them back, because `cardo trend` rebuilds silver before it reads.

**4. The trend rows are not part of the request.** The rows that outlive the window
([ADR-0055](../adr/0055-trend-rows-outlive-the-rows.md)) carry no pseudonym, and each one describes
at least five people. A line saying "five people, twelve sessions on 3 March" cannot be reduced to
you, so it is not your record and it is left alone.

**5. An organization that wants those periods gone anyway deletes the period, and the period is the
unit.** One person cannot be subtracted from an aggregate: the rows that said which of the five was
you expired with the window. So the deletion is the whole period, said out loud:

```sql
ALTER TABLE cardo.trend_day           DROP PARTITION tuple('2026-03-03');
ALTER TABLE cardo.trend_week_artifact DROP PARTITION tuple('2026-03-01'); -- the week's Monday
```

That removes the day for the whole fleet and the week for every artifact they used — not only their
share of it. `cardo trend --from 2026-03-03 --to 2026-03-03` then recomputes the day from whatever
rows are still inside the window.

**6. For a period whose rows have already expired, the person's share cannot even be located.**
Nothing in a trend row says who was there. Your organization names the periods it wants removed, and
they are removed whole, as in step 5.

Deleting a person's rows does not shrink the trend rows for the days they worked: those counts were
computed while their rows were there, and every one of them describes a group of at least five. That
is the honest limit of an aggregate, and it is why the floor is applied before anything is written.

## What your administrator can do that Cardo cannot prevent

An organization administrator can force full collection through managed settings regardless of what
Cardo is designed to do. No tool can prevent that.

What this project offers instead: the configuration is human-readable, it lives in a repository you can
read, and this document describes what a coercive deployment would look like — so you can check what was
actually deployed on your machine ([ADR-0007](../adr/0007-enrollment-posture.md)).

## Data protection deliverables

To be shipped in this repository (v0.2, [ADR-0003](../adr/0003-efficiency-before-security.md) phase 3):
a DPIA template and a works-council one-pager. The deletion path is written down above.
