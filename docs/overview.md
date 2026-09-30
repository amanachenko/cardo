# Cardo — is your AI-assisted engineering working?

*An overview for engineering leadership, security teams and engineers. Draft, September 2026.*

---

## In one paragraph

Cardo is open-source software you run in your own network. It shows how Claude Code is used across
your engineering teams:

- which of the approaches your teams have built are spreading, and which work best;
- what adoption costs, and where the money goes on rework;
- whether the policies your administrators set actually hold.

It also gives each engineer a private coach. Nothing is installed on laptops, no data leaves your
network, and engineers can check for themselves exactly what it collects.

---

## Where engineering organizations are

- **Adoption has been bottom-up.** Champions tried approaches first and the rest of the
  organization followed what worked. Many teams were given a charter to find their own way, which
  produced a large variety of team-grown skills, commands, agents and conventions.
- **That is turning top-down.** Boards ask whether the spend is justified. Leadership wants every
  team on the best approach, and soon. Picking the approaches that genuinely work, at a sensible
  cost, has become a real task.
- **Usage has run ahead of measurement.** Few organizations measure impact at all, and fewer
  reliably. Gains in lead time are real but vary a great deal.
- **Cost is watched, with blunt tools.** Leadership pieces cost together from the vendor console,
  observability dashboards and spreadsheet exports, then asks individuals why they spend more, or
  less, than others. Engineers know, and dislike it.
- **Some organizations have not allowed AI coding agents at all.** The obstacle is policy, not
  willingness: nobody can show that the restrictions would actually hold.
- **Next come agents that run with nobody at the keyboard**, and several AI tools used side by side.

---

## What each group needs, and what Cardo gives them

Items marked *(planned)* are designed but not yet built. See [What exists today](#what-exists-today).

### Engineering leadership, and the people running adoption

| The question | What Cardo shows |
|---|---|
| Which of the approaches our teams built should everyone use? | Team-grown skills, commands, subagents and MCP servers, and how many people use each. An approach is named once five people use it. How sessions using it compare on cost and on signs of the agent struggling *(planned)* |
| Has what we shipped reached every team? Is everyone on the current version of our instructions? | What share of the organization uses each thing you shipped, and which machines still load an old version of your instructions files. Each team's uptake against the whole organization *(planned)* |
| What are we getting for the spend? | Cost per accepted change and per commit or pull request, spend on rework, and cost per approach, as trends, always beside the volume they are computed from *(planned)* |
| Where is adoption lagging? | Approaches that aren't spreading, and teams a rollout hasn't reached *(the team view is planned)* |
| Do Claude-assisted changes land well? | For your repositories: time to merge and revert rate, Claude-assisted compared with the rest *(on request; needs read access to your git host)* |

**What Cardo deliberately does not give you:**
- rankings of teams or people;
- an "hours saved" estimate;
- productivity per head;
- any view of how an individual works.

Rankings and scores get gamed, and they turn engineers against the tool. A single number nobody can
defend discredits every number beside it.

**About cost per person.** Anthropic's console already shows it. If you want it in Cardo too, Cardo
can list the people over a budget you have published *(planned, on request)*. The list carries only
what the console shows, and each engineer sees their own position first.

### Engineers

- **A private coach page** *(planned for the pilots)*. It makes specific suggestions you can act on,
  for example:
  - "you waited 14 minutes this week on permission prompts for `npm test`, and here is the rule
    that stops it";
  - "six colleagues use `/review` and you haven't tried it".

  You are compared only with your own past. There is no score and no ranking.
- **Nobody else sees your page.** During a pilot, the person running the pilot can open it;
  volunteers are told this.
- **Your manager sees nothing about you** that Anthropic's console doesn't already show them.
- **Never collected:** your session transcripts, prompts, code, file paths or command arguments.
  Your email address is replaced by a pseudonym before anything is stored.
- **The entire installation is a readable settings file.** There is no agent on your machine, and
  the settings file is published so you can check it.

### Security and compliance

- **Evidence that policy holds** *(planned)*. For every machine that reports:
  - no bypass-mode sessions;
  - only approved MCP servers;
  - Claude Code at or above your minimum version.

  Every exception is listed.
- **Coverage** *(planned)*. Everyone Anthropic sees using Claude Code in your organization is also
  reporting, so "no violations seen" means something. Sessions signed in to other organizations'
  accounts on your managed machines are flagged.
- **Where the work happens** *(planned)*. Your own repositories are shown by name. Anything else is
  shown as "external", with its host only.
- **A periodic evidence report** for a risk committee or auditor *(planned)*. It states plainly what
  the evidence cannot see. Events sent to your SIEM follow later.
- **For organizations that haven't allowed Claude Code yet,** the report is designed to make a
  controlled pilot possible.

---

## Why it is built this way

- **It measures approaches, not people.** "Is the skill we shipped used, and does it help?" is a
  fact about the skill. A ranking of engineers is a number people learn to game.
- **It runs entirely in your network.** No phone-home, no hosted version, no benchmark upload.
- **Nothing is installed on laptops.** Your device management pushes one settings file, which turns
  on Claude Code's own telemetry and a set of fire-and-forget hooks. It never makes a session wait,
  and it changes nothing else about how Claude Code behaves.
- **Identity is hashed before storage,** and data is kept for 90 days. Pseudonymous data is still
  personal data, and the documentation says so plainly. Retention is what bounds it.
- **Small groups are never shown.** A team or a home-grown tool is named only once five people are
  in it.
- **Numbers come with their volumes.** A rate is never shown without what it is a rate of, and no
  estimate is shown that cannot be defended.
- **It adds to your existing tools.** It uses Anthropic's own usage data as context rather than
  replacing the console or your observability platform.
- **It is open source (Apache-2.0),** and every design decision is written down in the repository
  with the alternatives rejected and why.

---

## What exists today

| | Status |
|---|---|
| Collection: the settings file, hooks, and a collector that hashes identity and drops content | **Built**, and checked against real Claude Code sessions |
| Artifact usage, instructions versions, context and model-switch cost, and a dashboard | **Built**, and checked against those sessions |
| Reading Anthropic's usage data | **Built**; not yet run against a live organization |
| Coach pages, efficiency comparisons, repository classification, teams from your directory, the board report | **Planned for the pilots** |
| Evidence report, coverage, SIEM events | **Planned** for the security pilot |
| Named cost view, pull-request outcomes, other AI tools | **When an organization asks** |

Cardo is pre-alpha. It has not yet run with a team. Whether its efficiency signals really separate
approaches that work from ones that don't is something the pilots exist to find out.

---

## How the pilots run

1. **Internal trial:** a small team, for two to three weeks.
2. **Adoption pilot:** 15 to 30 of your engineers in three or more teams, for four to six weeks. It
   needs, from your side:
   - a small machine in your network;
   - a champion;
   - the settings file pushed to the volunteers' machines;
   - an export of who is in which team;
   - a read-only Anthropic analytics key.

   Success means leadership made at least one decision from it, at least half the volunteers acted
   on a suggestion, and nobody feels more watched than before. People are interviewed at the end.
   If nobody acted on anything, the pilot's findings say so.
3. **Security pilot:** for an organization that currently doesn't allow Claude Code. A controlled
   group is allowed, on condition of the evidence report.

---

## Where this is heading

- **From variety to standards.** The question moves from "what's spreading" to "is everyone on the
  standard, and what does the standard cost". Cardo keeps showing new approaches as they appear,
  so that standardizing doesn't freeze each team on whatever won first.
- **Agents without a person at the keyboard.** Runs in CI and in the background are labelled by
  workflow, and their cost belongs to a workflow and a repository, not to anyone. The privacy
  tension largely disappears there, and measuring approaches rather than people fits it naturally.
- **Several tools side by side.** Comparing Claude Code with other agents fairly needs a measure
  none of the vendors supplies. Your git host measures every tool the same way.
- **Outcomes, per repository.** Pull-request merge time and reverts, Claude-assisted or not, per
  repository and never per person.
- **What stays constant:** it runs in your network, the privacy boundary holds, and there are no
  rankings.

---

## Common questions

**Does it replace Anthropic's console, or our observability platform?** No. It reads Anthropic's
usage data as context and answers what the console doesn't: which approaches spread and work, by
team, and whether policy holds.

**Can we see individuals?** Only cost, for people over a budget you have published, and only on
request. The engineer sees it first. How any individual works is never shown to anyone but them.

**What does it take to run?** In the reference setup, one machine running ClickHouse, Grafana and
an OpenTelemetry collector, all inside your network.

**Which Claude deployments does it support?** Claude Code on the Claude API with an organization
account. On Bedrock, Vertex or Foundry the telemetry path works, but Anthropic's usage data isn't
available.

**Who holds the key that could re-identify people?** In a pilot, the person running it. In a
deployment, you decide, and the recommendation is your security team.
