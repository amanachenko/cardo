# Privacy — what Cardo collects and what it does not

> Edited in place; describes reality. **Status 2026-09-25:** both collection paths are built: the
> Admin API poller, and the managed-settings bundle with its collector. So are the views and
> dashboards over what they store. The collector has been run against a real Claude Code, and the
> list below is what it kept. Changes decided on 2026-09-25 and not yet built are listed
> separately, under "Decided, not built yet". This document is written to be readable by the
> engineers being measured, not only by the team deploying it.

## The short version

- Cardo **never reads your session transcripts** — not for debugging, not once (INV-1).
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
thing for themselves, that is how the platform team learns what it should have shipped. Three rules
come with it:

- A name is shown in any view only once at least five people used it in the same week. A command
  that only you use never appears by name; it is counted as "other". Your organization can raise
  the five, never lower it ([ADR-0029](../adr/0029-minimum-group-size.md)).
- No view counts anyone's own commands per person.
- Your organization can switch to the strict mode (`CARDO_ARTIFACT_NAMES=org-only`), in which every
  name that is not the organization's own is recorded as `custom`.

**About your team.** The same rule covers the team name in the bundle. A team with fewer than five
people working on a given day is shown as "other" that day, so a chart of a small team is never
two or three people's numbers. Totals for the whole organization are always shown.

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
It turns on Claude Code's own telemetry and adds thirteen hooks that fire and forget: **Cardo never
makes your session wait**. It sets nothing else. Your own hooks, plugins, permissions and version
all stay as they were ([ADR-0024](../adr/0024-bundle-configures-telemetry-only.md)).

One side effect is Claude Code's behaviour, and cannot be avoided: when an organization sets the
telemetry endpoint centrally, any telemetry endpoint you had configured yourself stops being used.

## Decided, not built yet

Recorded here so you know what is coming before it arrives. Each links to the decision behind it.

- **Which instructions files your sessions loaded in your organization's repositories:** whether
  any loaded, and how many. Your organization can choose to record the names of project-level
  files, and this document will say if it has. The names of your own personal files are never
  recorded ([ADR-0035](../adr/0035-repository-identity-classified.md)).
- **Your team, taken from your organization's directory,** with the dates you joined and left it
  ([ADR-0034](../adr/0034-identity-in-pilots.md)). The five-person rule applies unchanged.
- **Your own coach page** ([ADR-0032](../adr/0032-engineer-coach-by-shared-link.md)). It shows
  suggestions about things you can change, compared only with your own past, with no score and no
  ranking. In a pilot you volunteer for it, you get it as a private link, and you are told that the
  person running the pilot can open it too.
- **Runs in CI or in the background are labelled as a service,** not counted as a person
  ([ADR-0036](../adr/0036-service-runs-are-not-people.md)).
- **Only if your organization asks for it:** a budget owner may see the cost of people who are
  over a budget published to you. It shows only what Anthropic's console already shows them, and
  you see your own position first ([ADR-0033](../adr/0033-per-person-cost-for-budget-owners.md)).
  That would be the one exception to "nobody except you".

## What requires you to opt in (tier 2)

Anything touching *what you were working on*. Opting in gets you your own dashboard, visible only to
you.

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
([ADR-0016](../adr/0016-retention.md)):

| Tier | Retention |
|---|---|
| Security (T0) | 1 year |
| Fleet (T1) | 90 days |
| Personal (T2) | 30 days |
| Aggregates with no pseudonym | indefinite |

## What your administrator can do that Cardo cannot prevent

An organization administrator can force full collection through managed settings regardless of what
Cardo is designed to do. No tool can prevent that.

What this project offers instead: the configuration is human-readable, it lives in a repository you can
read, and this document describes what a coercive deployment would look like — so you can check what was
actually deployed on your machine ([ADR-0007](../adr/0007-enrollment-posture.md)).

## Data protection deliverables

To be shipped in this repository (v0.2, [ADR-0003](../adr/0003-efficiency-before-security.md) phase 3):
a DPIA template, a documented deletion path, and a works-council one-pager.
