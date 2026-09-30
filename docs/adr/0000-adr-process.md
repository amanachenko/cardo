# ADR-0000 — How we record decisions

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** Judgment. Established at project inception, before any code existed.

## Context

Cardo's design was settled in a long structured interview before implementation began. Roughly half the
value of that conversation was not in what was chosen but in what was *rejected*, and why. That
reasoning is unrecoverable from the code: a future session reading `deploy/collector/config.yaml` can
see that email is hashed, but cannot see that laptop-side hashing and store-raw-mask-on-read were both
considered and rejected for specific reasons.

Several decisions here are also load-bearing for trust in a way that makes them easy to breach
innocently. "Let me just read the transcript to debug this" is a reasonable-sounding thought that
destroys the project's central promise.

## Decision

Architecture Decision Records under `docs/adr/`, numbered sequentially and never renumbered, with
six rules:

1. **ADRs are immutable.** Never edit an accepted ADR. To change a decision, write a new ADR that
   supersedes it and links back; mark the old one `Superseded by ADR-NNNN`. The archive's value is that
   it records what we believed *at the time* — editing it destroys exactly that.

2. **Status is one of:** `Proposed` | `Accepted` | `Superseded by ADR-NNNN` | `Rejected`.

3. **"Rejected alternatives and why" is a mandatory section.** This is the part future readers need most
   and the part that always gets dropped. An ADR without it is incomplete and should be sent back.

4. **Every ADR names its evidence source** — which dated research note supports it, or explicitly
   "Judgment, no evidence." Never let a judgment call read as though it were established fact.

5. **Research notes are dated snapshots and are never updated.** Files in `docs/research/` carry the
   date in the filename and are frozen on write. Claude Code's telemetry surface drifts — attributes are
   already version-gated at `2.1.214+` and `2.1.269+`. An undated document asserting "Claude Code emits
   X" is a landmine for a future session. If a note is stale, write a new dated one; do not edit the old.

6. **Scope rule.** ADR anything expensive to reverse, or anything a future session might innocently
   violate. Everything else lives in commit messages. Do not ADR the choice of linter.

Current-state documents (`docs/design/architecture.md`, `data-model.md`, `privacy.md`) are the opposite:
edited in place, and they must always describe reality rather than intent. When they disagree with an
ADR's stated plan, reality wins and the ADR stays as the historical record.

`docs/design/invariants.md` holds the hard rules. Violating one requires a superseding ADR — not a
code comment, not a "just this once."

## Consequences

- Writing a decision down costs a few minutes; recovering a lost rationale costs hours or produces a
  silent regression.
- The ADR set will contain decisions that later look obvious. That is fine and expected.
- `CLAUDE.md` restates the invariants inline so they are in context for every session without anyone
  having to go looking, and points at `docs/adr/README.md` as the index.

## Rejected alternatives and why

- **A single `decisions.md` log.** Lower ceremony, but loses the supersession chain, has no natural place
  for rejected alternatives, and grows into an unreadable wall that nobody updates.
- **RFCs in addition to ADRs.** Appropriate once there are several contributors debating proposals before
  deciding. Premature for one author; would add a stage with no reviewer in it.
- **Decisions captured only in commit messages and PR descriptions.** Git history is searchable but not
  browsable, and the reasoning gets fragmented across a dozen commits. A future session will not find it.

## Template

```markdown
# ADR-NNNN — <short imperative title>

**Status:** Proposed | Accepted | Superseded by ADR-NNNN | Rejected
**Date:** YYYY-MM-DD
**Evidence:** <dated research note filename> | Judgment, no evidence

## Context
<the forces at play, the constraint that made this a decision at all>

## Decision
<what we are doing>

## Consequences
<what this makes easy, what it makes hard, what it commits us to>

## Rejected alternatives and why
<mandatory — each option considered and the specific reason it lost>
```
