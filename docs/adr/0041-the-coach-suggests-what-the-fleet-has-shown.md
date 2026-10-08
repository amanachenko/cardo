# ADR-0041 — The coach suggests only what the fleet has shown

**Status:** Accepted — **not built**
**Date:** 2026-10-08
**Evidence:** Judgment, no evidence. A contributor's proposal of 2026-10-07 described a coach that
maps patterns in a person's sessions to tools.

Extends [ADR-0032](0032-engineer-coach-by-shared-link.md) without superseding it: the coach, its
shared links and its rules stand. This ADR says where its suggestions come from.

## Context

ADR-0032 makes the coach a list of suggestions, each tied to something the engineer can fix, with
the engineer compared only with their own past. It does not say where a suggestion comes from.

A contributor proposed a catalog of patterns and the tools that fix them: many searches for symbol
definitions lead to a code-search tool, a wall of denied `cat` commands to a read wrapper, long
waits on `npm test` to an allow rule. Most of those patterns cannot be seen in tier 1. The
collector deletes tool parameters ([INV-5](../design/invariants.md)), so Cardo sees that `Grep`
ran, not what it searched for, and that a `Bash` call waited, not which command. ADR-0032's own
example, fourteen minutes of prompts for `npm test`, is one of them. A catalog precise enough to
trust needs what the person was working on, and every request to make it more accurate is a
request for content.

Adoption turning top-down ([ADR-0040](0040-spread-is-a-vote-only-for-what-teams-chose.md)) also
changes what the coach is for. Once leadership decides an approach should be used, the coach is
how that decision reaches the people it would help, one person at a time, with no manager in
between.

## Decision

**1. A suggestion names only something the fleet has shown:** an artifact or rule the
organization's own engineers adopted, above the minimum group size
([ADR-0029](0029-minimum-group-size.md)), or one the adoption owner made official. An artifact
whose measured effect is negative is not suggested. There is no hand-made catalog of patterns and
tools.

**2. What prompts a suggestion is a pattern tier 1 can see:** counts and classes from the person's
own sessions, such as compactions, edit rejections, failed tool calls, a cold cache on large
requests, or a tool mix that peers moved away from once they adopted the artifact. A suggestion
that would need a command, a path, a parameter or a prompt waits for tier 2
([ADR-0002](0002-three-tier-data-model.md)), which is opt-in, visible only to the person, and not
built.

**3. One suggestion at a time, with the number that prompted it,** from the person's sessions
against their own past. Peers appear only as a count of people using the artifact, never as a
comparison with how they work.

**4. Figures first.** In a pilot, a volunteer's page shows only their own figures in its first
week, and suggestions start in the second. The first week is the baseline against which the
pilot's "acted on a suggestion" is read.

## Consequences

- One analysis serves two audiences. What tells the adoption owner an approach works is what tells
  an engineer to try it. Nothing about one engineer reaches anyone else (INV-3); what reaches the
  engineer is drawn from the fleet.
- The coach is built after the artifact views and from them: a suggestion needs the spread and
  efficiency figures of what it names. Early in the dogfood, before anything home-grown has spread
  past five people, the coach can suggest only the organization's own artifacts.
- Tier-1 suggestions are coarse. That is the price of not collecting content, and the adoption
  pilot's "acted on a suggestion" criterion measures whether coarse is enough.
- "Which artifacts they have not tried" stays the most personal thing on the page, and
  [risks.md](../../risks.md) #17 applies unchanged.

## Rejected alternatives and why

- **A catalog of patterns and the tools that fix them.** Precise patterns need content tier 1 does
  not collect, coarse ones give wrong suggestions, and one wrong suggestion gets the page ignored.
  A fixed list also recommends what its author chose, not what the organization's engineers
  adopted.
- **Collecting tool parameters in tier 1 to make suggestions accurate.** INV-5. That precision
  belongs in tier 2, where the person opts in and is the only one who sees it.
- **An opt-in semantic review of a person's sessions,** judging whether the work was good. It
  needs transcripts, which no code path opens in any tier (INV-1).
- **Learning from what each person declines** ("not now" as feedback that reorders suggestions).
  It stores a per-person record of advice declined, which is exactly what a manager would ask to
  see.
