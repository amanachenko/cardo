# Where Cardo is going

> **The aim, what Cardo is and is not, where it is heading, and how we choose what to build next.**
> This page is edited in place as the work moves. The North Star and the rule for choosing work
> are pinned in [the North Star decision (ADR-0042)](adr/0042-the-north-star.md), and change only
> through a new decision that replaces it. What exists today is in
> [architecture.md](design/architecture.md); the list of work is in the [roadmap](roadmap.md).

## The North Star

> **Organizations decide which ways of working with AI become standard, based on evidence from
> their own teams, without watching anyone.**

**What counts as a decision.** For example:

- making the `/review` command one team built the official one for everyone;
- retiring a connector (an MCP server) that was rolled out to everyone and that almost nobody
  calls;
- going back to two teams that a rollout missed;
- asking the machines still loading an old version of the organization's instructions to update;
- letting a first group of engineers start using Claude Code, because the evidence shows the
  organization's rules hold.

A decision can be made at any level. A team picking up another team's command is a decision, and
so is a leader making that command official.

**How we know it is working:**

- **Decisions get made.** The person who answers for AI adoption changes something because of what
  Cardo showed, and can say what.
- **Nobody feels more watched than before.** Engineers are asked before and after. If the answer
  gets worse, something is wrong, however good the charts look.
- **If nobody decides anything, that is a result too.** It means Cardo is wrong in its current
  form, and we find out why before building more.

The exact checks for an organization trying Cardo are in the roadmap, under
[how a pilot judges Cardo](roadmap.md#how-a-pilot-judges-cardo).

## Who it is for, and where engineers and teams fit

Organizations decide whether to run Cardo, so the people who answer for AI adoption come first:
the CTO or VP Engineering, whoever runs adoption week to week, and the champions who build their
teams' tools. Engineers come next, through a private page of their own. Security comes after them,
through proof that the organization's rules hold. The reasoning, and the alternatives turned down,
are in [who Cardo serves (ADR-0030)](adr/0030-stakeholders-and-questions.md).

Engineers and teams are not a side audience. They are where every decision lands, and one set of
evidence serves both sides. For example, if sessions that use a team's `/review` command fail fewer
tool calls, that tells a leader the command is worth making official. The same fact, on an
engineer's private page, becomes "11 people here use `/review`, and you haven't tried it yet".
Nothing about one engineer ever reaches anyone else.

## What Cardo is, next to what you already have

Anthropic's console shows who uses Claude Code and what it costs. Cardo shows which of the ways
your teams use it are worth making standard, whether what you rolled out reached everyone, and
whether your rules hold. It runs in your own network, and it measures the tools, not the people.

**Three things set it apart:**

1. **Which approach wins, down to the single command or skill.** Not "AI usage is up 30%", but
   "this team's review command spread to four teams, and sessions that use it go better".
2. **Nothing leaves your network.** It runs on your machines. There is no hosted version, and no
   data is sent to us or anyone else. Engineers can read the one settings file that turns it on.
3. **Proof that your rules hold.** For each machine: no sessions with the safety checks bypassed,
   only approved connectors, and a Claude Code that is new enough.

**What it is not:**

- **Not a return-on-investment calculator.** DX, Jellyfish and Faros sell that across tools, tied
  to delivery data. Cardo gives no "hours saved", because nobody can defend that number, and one
  indefensible number discredits every number next to it.
- **Not a replacement for Anthropic's console or for Datadog.** It reads the console's numbers as
  context, and answers the questions they don't.

## Directions

### Now

- **What works, and what to make standard**, for leaders. Which tools teams built are spreading,
  whether sessions that use them go better, and whether what the organization rolled out reached
  everyone. Example: "the review skill pushed to everyone on 3 November reaches 80% of people, but
  sessions that use it look no different from before".
- **Signs that the agent is struggling, and the engineer's private page**, for engineers. Failed
  tool calls, the same step retried again and again, conversations squeezed because they got too
  long. The private page shows a person their own figures against their own past, then one
  suggestion at a time, drawn from what colleagues adopted.
- **Getting real data in, safely**, for whoever runs Cardo. A quickstart on one laptop, a check
  that the data arrived, dashboard logins that cannot read anyone's personal rows, and a notice the
  organization gives its engineers.

### Next

- **Cost that holds up in a board meeting.** Cost per accepted change, money spent redoing work,
  and cost per approach, each shown beside the volume it is computed from, and labelled as an
  estimate. It needs data from more than one team.
- **Proof that policy holds**, for security. Example: "every machine that reported this month is
  on the minimum version or later, and three sessions used a connector that isn't approved".
  **The exception:** for an organization that hasn't allowed Claude Code yet, this comes first.
  "Let a first group start" is the only decision it can make, and this is the evidence it needs.

### Later, when an organization asks

- Pull-request outcomes from the git host, such as time to merge and reverts, per repository and
  never per person.
- Other AI coding tools side by side.
- Agents that run with nobody at the keyboard, in CI or in the background.
- A private, opt-in personal layer with richer detail about what the engineer was working on, seen
  only by them.
- Support for organizations on Claude Enterprise seats, whose usage data comes from a different
  Anthropic API.
- Sending security events to the organization's SIEM, once the policy report exists.

### Never

- Rankings of people or teams, or a league table of any kind.
- "Hours saved", or a return-on-investment estimate.
- Anything divided by headcount, such as pull requests per engineer.
- A hosted version, or any data leaving the organization's network, including a benchmark that
  compares organizations.
- A view of how one person works, for anyone but that person.

**Not planned:** security detection based on behaviour, such as spotting unusual activity, because
it would need the content Cardo does not collect.

The hard rules behind this list are in [invariants.md](design/invariants.md), and what Cardo
collects and never collects is in [privacy.md](design/privacy.md).

## How we choose what to build next

**The rule: build what gets an organization to its first real decision soonest.**

- Work that makes a new decision possible comes before work that improves a decision already
  possible.
- On the way, check the assumptions most likely to be wrong before building on them.
- Never trade away one of the hard rules to get there faster.

**A worked example, as of October 2026.** The first decisions a leader could make are:

1. make a team's command official;
2. go back to the teams a rollout missed;
3. retire something nobody uses;
4. chase the machines still on old instructions.

The fourth is already possible with what is built, and so is spotting a team-built command that is
spreading. What is missing is an organization running Cardo safely, so that comes first. The first
three all need Cardo to know how each tool arrived, rolled out to everyone or picked up by choice,
and the second also needs to know who is in which team. Those come next. The board report improves
a decision a leader can already make roughly, and it needs several teams' data anyway, so it waits.

**So the work goes in three steps:**

1. **Run it safely:** the laptop quickstart and the check command, dashboard logins that cannot
   read personal rows, and the notice for engineers.
2. **Two tracks, side by side:**
   - **the leaders' side:** how each tool arrived, then teams from a file;
   - **the engineer's side:** the signs of struggle, then the private page (the code-name command,
     the page with figures only, and its own dashboard server), then suggestions on it.

   The signs of struggle feed both tracks. Leaders compare tools with them, and each engineer sees
   their own.
3. **Later:**
   - the board report;
   - proof of policy, unless the exception above applies;
   - protecting the collector from made-up data, before Cardo serves a second team;
   - labelling automated runs;
   - which repositories have instructions files.

   Combining several laptops' data into one store is done only if an organization runs Cardo on
   laptops rather than on one shared server.

Each piece of work, and who has taken it, is in the [roadmap](roadmap.md#work-items).

## The two tracks

- **The leaders' side** answers "what should become standard?": how each tool arrived, teams, then
  the board report.
- **The engineer's side** answers "what does struggle look like, and how does a person find out?":
  the signs of struggle, then the private page. The best person to define those signs is someone
  who knows from experience what an agent getting stuck looks like. Each sign is written down with
  the result that would prove it wrong, before anyone looks at the data, because none of them has
  been tested with a team yet ([risks](../risks.md), first entry).
- **Running it safely** is shared.

Who leads each track is marked in the roadmap.

## How to disagree with the direction

Everything on this page is open to challenge, and challenging it is welcome. Agreement should come
out of the discussion, not be imposed.

1. **Open a GitHub issue** that says what you would change, why, and gives an example. Word it for
   any organization: no real organization's name, plans or data.
2. **If it isn't settled quickly, it goes on the Open questions list below**, with both sides
   stated fairly. Each side writes its own.
3. **When it is ready to decide, it becomes a decision record with the status "Proposed"**, and is
   discussed in its pull request ([how decisions are recorded](adr/0000-adr-process.md)).
4. **If the discussion stalls, the project's owner decides.** The side that didn't win goes into
   the record's "rejected alternatives" with its reasons, so the argument is kept and can be
   reopened when the evidence changes.

Changing the North Star itself takes a new decision that replaces
[ADR-0042](adr/0042-the-north-star.md).

## Open questions

### Should teams get a page of their own?

- **Today:** teams appear only in leaders' views, as each team's uptake against the whole
  organization, never ranked. Teams benefit through their champions, whose tools are seen
  spreading, and through each member's private page.
- **For a team page:** a team could see its own figures against its own past, the way an engineer
  does. Teams often own the tools and rules that cause struggle, so they could fix them without
  waiting for a leader to notice.
- **Against, for now:** a manager can ask to see it, and once every team has one, teams get
  compared, which is the ranking Cardo refuses. Teams of fewer than five are shown as "other", so
  many small teams would see nothing. It would need its own decision record.

## Words we use

- **Organization:** whoever runs Cardo, for its own engineers.
- **Adoption owner:** the person who answers for AI adoption, often the CTO or VP Engineering. They
  make the decisions the North Star is about.
- **Operator:** the person who runs Cardo and the adoption work day to day, and reports to the
  adoption owner. During a trial they hold the key that could reveal who is behind a code name, and
  they are the only one who logs in to the engineers' private pages.
- **Champion:** an engineer who builds their team's tools and spreads them. Seeing that work spread
  is their reward, and their case for making it official.
- **Artifact:** something a team builds, or the organization ships, that shapes how Claude Code
  works: a skill, a slash command, a subagent, a connector (MCP server), a permission rule, or an
  instructions file such as `CLAUDE.md`. Cardo measures these, not people.
- **Chosen or rolled out:** an artifact a team picked up because they wanted it, or one pushed to
  everyone. A chosen artifact's spread is a vote. A rolled-out artifact's spread only shows the
  rollout's reach, and whether it helps has to be measured separately.
- **Signs of struggle:** the measures that suggest the agent is having a hard time. Older documents
  call them the "efficiency family".
- **The private page, or coach:** each engineer's own page, seen only by them. It compares them
  only with their own past, and gives no score.

Every document written or edited from now on follows this page's style: plain words, and an
example for each claim.
