# ADR-0058 — A person's whole history sits on the team they are in now

**Status:** Proposed
**Date:** 2026-11-01
**Evidence:** Judgment, no evidence. The question is which of two accepted records governs the
dates; this record picks one and says what the choice costs.

Narrows [ADR-0034](0034-identity-in-pilots.md) (decision 3) on the dates of each assignment. Teams
still come from a directory file, falling back to the bundle label; what changes is that the dates
are not kept.

## Context

[ADR-0034](0034-identity-in-pilots.md) decision 3 has the directory map each person to a team "with
the dates of each assignment", and says "the dates keep last quarter's numbers on last quarter's
team". The team-reach view does not keep dates: it reads the directory as a hashed
pseudonym-to-team mapping ([ADR-0046](0046-hashed-team-directory.md)) and puts a person's whole
history on the team listed for them now. The two disagree, and the view is what ships first.

## Decision

**1. A person's whole history sits on the team the directory lists for them now.** No join or leave
dates are kept. Every week of the window that a person appears in is counted under their current
team.

**2. The window bounds the shift.** With the default 90 days
([ADR-0053](0053-retention-default-and-organization-choice.md)), a move shifts at most the weeks
inside the window that came before it. Nothing permanent is attributed this way: the trend rows
kept past the window carry no team at all (the trends record, ADR-0055).

**3. When an organization keeps everything, older weeks move with the person.** With no expiry in
force, the shift is not bounded by a window: a person who changed teams in March carries their
whole past under the team they joined. This is the cost of not keeping assignment history, and the
team-reach documentation says so where the window is described.

## Consequences

- A person who moved teams mid-window has the weeks before the move counted under the new team,
  and the weeks after it too — one person, one team, for the whole window.
- The directory carries no dates column, and no history table keyed to a person exists in the
  store. The load stays one mapping: pseudonym and salt version to team.
- The trend rows kept for good stay out of it: they carry no team and no pseudonym.

## Rejected alternatives and why

- **Keep the dates of each assignment, as ADR-0034 decision 3 says.** It keeps last quarter's
  numbers on last quarter's team, and it costs a per-person history of moves between teams, kept
  in the store. Under the default window the gain is at most the weeks before a move, and the
  trend rows — the only rows that outlive the window — carry no team either way. A narrower record
  is right while the window is short; if an organization keeps everything and the shift starts to
  matter, the dates return by a new record, not by quietly changing this one.
- **Attribute each week to the team as of that week, computed from the sessions themselves.** The
  sessions do not carry a team; the directory does, and it is a snapshot of now. Deriving
  membership history from who had sessions when would make a quiet week look like a departure.