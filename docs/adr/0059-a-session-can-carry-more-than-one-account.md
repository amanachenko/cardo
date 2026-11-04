# ADR-0059 — A session can carry more than one account

**Status:** Accepted
**Date:** 2026-11-04
**Evidence:** Judgment, from behaviour seen on the reference stack: a session in which the person
switched Claude accounts carried two code names, and the coach's figures changed on every reload.
No figure from that stack is recorded here.

## Context

When someone switches Claude accounts in the middle of a session, Claude Code keeps the session id
and sends the new account's email from then on. The collector turns each email into a code name
(the pseudonymization record, [ADR-0006](0006-pseudonymization.md)), so one session's rows carry
two code names: the first account's on the events before the switch, the second's after it.

The silver views were written for one code name per session. `silver_session` picked it with
`anyIf`, which returns whichever row ClickHouse reads first, and every other silver view took the
session's pick. So the whole session, its prompts and its dollars, moved between the two accounts
from one read to the next. The coach, which reads one code name's rows, showed a different number
of sessions and a different cost each time the page was loaded. The same pick sized teams for the
five-person minimum ([ADR-0029](0029-minimum-group-size.md)), counted fleet adoption, filled the
teams page and the artifact people counts, and decided which hook rows the deletion procedure
removed.

A hook row carries no identity at all: the hook allowlist keeps the session id and never the email
([ADR-0023](0023-hook-payload-allowlist.md)). It can only be credited to an account through the
session's other rows.

## Decision

**1. A row belongs to the account on it.** An OTel event carries the code name of the account that
sent it, and the silver views take the code name from the row, never one chosen for the whole
session. A session that moved from one account to another gives each account its own part: its
prompts, requests, cost, tool calls, permission waits, compactions and artifact loads.

For example, a session starts on a work account, which sends 12 prompts costing $4.10, and the
person then switches to a second account, which sends 5 prompts costing $1.30. The first code name
shows 1 session, 12 prompts and $4.10; the second shows 1 session, 5 prompts and $1.30; the fleet
shows 1 session, 17 prompts and $5.40. Every read gives the same numbers.

**2. A row with no code name belongs to the account active at that moment:** the last code name the
session showed before it. A row earlier than any code name, such as the hook that announces the
session's start, takes the session's first. Hook rows are credited this way, and so is an OTel
event that arrived without an email. A session with no code name at all stays unattributed, as
before.

One helper view, `silver_session_account_live`, lists for each session the moment each account
became active. Every view that needs an account for an unnamed row looks it up there, and so does
the deletion procedure, so a page and a deletion always credit a row to the same account.

**3. `silver_session` has one row per session and account.** A session switched once has two rows,
and each counts the session once for its account. What describes the session itself is the same on
each of its rows: when it started and last showed an event, its day, its clock, whether and why it
ended, whether it is a service run and its workflow, its app version, its terminal and how it
started. A session is still counted on the day it started. What describes the account comes from
that account's rows: the code name, the team label and the salt version. Every value is the first
one seen, by time, never `any`, so the same rows always give the same answer.

**4. The fleet counts a session once.** Anything that counts sessions counts distinct session ids,
never rows of `silver_session`. Anything that joins `silver_session` to a row-level view joins on
the session and the code name, or tests the session with `IN`, so that a session's turns are not
counted once per account. People counts were already distinct.

**5. Deleting a person deletes what is credited to them, by the same rule.** Their rows are the rows
that carry their code name and the unnamed rows that the helper view credits to them. The other
account's part of a shared session stays: it is that account's data, and deleting it would delete
someone else's history on a request they did not make.

## Consequences

- The coach shows the same figures on every reload, and each account's cost is the cost of its own
  requests, which is also what Anthropic's console bills that account.
- A session switched between accounts counts once for each account and once for the fleet. Each
  account is sized in its own team for the five-person minimum.
- Stored silver gains a row only for a switched session. The tables keep their columns, so none has
  to be dropped and rebuilt for this change.
- The trend rows of the days and weeks that hold a switched session are recomputed once, by the
  migration that makes this change, because they were written from the old pick.
- A rule for every view written later: take the code name from the row, never from the session, and
  never with `any` over a value that can differ between rows. `.claude/rules/views.md` carries it,
  and a test fails on a view that takes its code name from `silver_session`.
- Clocks limit how exact decision 2 is. A hook row carries the collector's receive time and an OTel
  row the laptop's, and on the reference stack the two were 1 to 3 seconds apart. A hook row sent
  within that margin of a switch can land on the other account. It lands there on every read, so
  the figures are stable; and a switch is a single moment, so very few rows are near one.
- Cardo cannot tell whether the two accounts belong to one person. Code names are never linked to
  each other, and a person with two accounts was already two code names across separate sessions.
  This decision does not change that.

## Rejected alternatives and why

- **One owner per session**, the first code name seen or the one on most rows. It is stable, and it
  is wrong. The second account's prompts and dollars would appear on the first account's page, and
  the coach is one person's own rows ([ADR-0032](0032-engineer-coach-by-shared-link.md)): it must
  never show one code name another's work. The first account's cost would also stop matching what
  the console bills it, by exactly the money the console bills to the second.
- **Keep `any`, as before.** Not stable. ClickHouse returns whichever value it reads first, and that
  changes with merges, parts and threads, so the same session moved between accounts from one read
  to the next. A figure that changes on reload is one nobody can act on.
- **A new session id for each account's part.** It would split the counts the same way, but the
  session id is the key that ties hook rows to OTel rows, and an invented one would have to be
  carried into every join. The fleet would also count one session twice.
- **Credit a hook row through its prompt.** A hook row and the OTel events of the same prompt share
  a prompt id, which does not depend on any clock. But a hook row with no prompt, such as the
  session's start and end, still needs a rule by time, and one rule for every row is one thing to
  explain, test and apply in a deletion. A switch is a single moment, so the time rule errs only on
  rows within seconds of it.
