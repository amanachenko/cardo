# ADR-0042 — Organizations decide what becomes standard, without watching anyone

**Status:** Proposed
**Date:** 2026-10-14
**Evidence:** Judgment, no evidence. It comes from a discussion between the project's owner and a
contributor about where effort is worth most, from
[the stakeholder field notes](../research/2026-09-25-stakeholder-field-notes.md) for what
organizations ask, and from a read of the repository's documents on 2026-10-14, which found no
stated aim and no rule for ordering work.

Extends [who Cardo serves (ADR-0030)](0030-stakeholders-and-questions.md) and
[how we record decisions (ADR-0000)](0000-adr-process.md) without replacing either. The readable
version, with the current directions and examples, is [`docs/vision.md`](../vision.md).

## Context

- **Cardo has many decisions about what it measures and what it refuses**, but no sentence that
  says what success is. Without one, every piece of work looks equally important, and the order
  of the roadmap depends on whoever writes it.
- **Two people now shape the direction, from different sides.** One starts from the organization:
  the CTO or VP Engineering deciding what to make standard. The other starts from individual
  engineers and teams: what struggle looks like, and how a person finds out. Both sides matter.
  They need one shared aim to argue about where effort is worth most.
- **The order of work has been implicit**, spread across about ten decision records: measuring
  before security, one data source proven before a second, the content of the private page before
  a channel to deliver it. Nothing says which of those rules wins when they disagree.
- **[ADR-0000](0000-adr-process.md) turned down a proposal stage before decisions** as "premature
  for one author". There is more than one author now, and no decision record has ever had the
  status Proposed.

## Decision

### 1. The North Star

> **Organizations decide which ways of working with AI become standard, based on evidence from
> their own teams, without watching anyone.**

- **It is measured by decisions made.** Someone responsible for adoption changes something because
  of what Cardo showed. Examples: making one team's `/review` command the official one, retiring a
  connector nobody calls, going back to teams a rollout missed, letting a first group start using
  Claude Code.
- **It is checked by trust.** Engineers feel no more watched than before. If that gets worse, it
  outweighs any number of decisions.
- **"Organizations", not "engineering leaders".** Decisions happen at every level: a team picking
  up another team's command is one. A sentence that names leaders reads, to an engineer, as
  leadership watching from the top, which is the opposite of the promise.
- **"Without watching anyone", not "nobody feels watched".** The first is a promise the design
  keeps ([the hard rules](../design/invariants.md)). The second is a feeling, and belongs in how
  success is checked.

### 2. How work is ordered

**Build what gets an organization to its first real decision soonest.**

- Work that makes a new decision possible comes before work that improves a decision already
  possible.
- On the way, the assumptions most likely to be wrong are checked before anything is built on
  them.
- No hard rule is ever traded for speed.

For example, chasing the machines on an old version of the organization's instructions is already
possible with what is built, while making a team's command official needs Cardo to know how each
tool arrived. So running Cardo safely at an organization, and recording how each tool arrived, come
before a board report that refines a decision leaders can already make roughly.
[`docs/vision.md`](../vision.md) keeps the current worked example and the resulting order. The
[roadmap](../roadmap.md) keeps the list of work.

### 3. How the direction is challenged

1. A disagreement about direction starts as a GitHub issue, worded for any organization.
2. If it isn't settled quickly, it goes on the Open questions list in
   [`docs/vision.md`](../vision.md), with both sides stated by the people who hold them.
3. When it is ready, it becomes a decision record with the status **Proposed**, discussed in its
   pull request, and Accepted or Rejected when that pull request closes.
4. If the discussion stalls, the project's owner decides. The losing side is recorded among the
   rejected alternatives, with its reasons.

Anything about a real organization (who is trying Cardo, when, and why that changes the order) is
discussed outside the repository, as [ADR-0038](0038-public-under-a-collective-copyright.md)
requires.

This is the light version of the proposal stage ADR-0000 deferred. It adds no new document type,
only a use for the status that already exists.

## Consequences

- **`docs/vision.md` is edited in place; this record is not.** The sentence, the measure, the rule
  and the challenge route change only through a record that replaces this one. The directions, the
  worked example and the open questions change with the work.
- **Some work moves.** Running Cardo safely at an organization moves to the front. The board report
  waits, because it refines a decision leaders can already make roughly.
- **The engineer's side is not second in time.** It runs beside the leaders' side, because the
  signs of struggle that leaders use to compare tools are the same signs an engineer sees on their
  private page.
- **"No decision was made" is a result that can be reported**, and it stops further building until
  the reason is found ([roadmap](../roadmap.md#how-a-pilot-judges-cardo)).
- **The North Star cannot be checked until an organization runs Cardo.** Until then it guides the
  order of work, and nothing more.
- **A contributor who loses an argument can see why**, in the record, and can reopen it when the
  evidence changes.

## Rejected alternatives and why

**For the North Star:**

- **The number of organizations running Cardo.** Easy to count, and it says nothing about whether
  Cardo helps. A tool that is installed and ignored would score well.
- **Influence: "measure the tools, not the people" becoming the accepted way.** It can only follow
  from organizations finding Cardo useful. Aimed at directly, it produces talks, not a working tool.
- **Engineers getting better at working with AI.** Engineers matter, but organizations decide
  whether to run Cardo ([ADR-0030](0030-stakeholders-and-questions.md) turned down "engineers
  first" because nobody would pay for it). Engineers improving is how a decision takes effect, and
  the private page is where it is seen.
- **Return on investment, or hours saved.** Every vendor sells one, and nobody can defend it
  ([ADR-0030](0030-stakeholders-and-questions.md)).
- **Two North Stars, one for leaders and one for engineers.** Two aims compete for the same time,
  and every argument about priority turns into which aim wins.
- **"Engineering leaders decide which ways of working with AI become standard…"** True to the
  intent, but it reads as top-down, and engineers would hear a leader watching them.
- **"…and nobody feels watched."** "Feels" can be read cynically: watched, but without noticing.

**For the order of work:**

- **One working view for each audience first**, so that the story is complete. It makes a good
  demonstration and no decision.
- **The riskiest assumption first**, as the rule itself. It is kept as the way the rule is applied.
  On its own, it validates things no decision needs yet.

**For the challenge route:**

- **Consensus required.** Between two people, a single disagreement stops all work.
- **The owner decides everything.** A contributor who can never win an argument stops making them.
- **Talk first, write the outcome later.** The losing side's reasons get lost, and with them the
  chance to reopen the question when the evidence changes.
- **A shared document outside the repository.** The argument would sit away from the code and the
  decision records it is about, and future contributors would not find it.
