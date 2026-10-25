# ADR-0053 — Retention is 90 days by default, and the organization may change it

**Status:** Accepted
**Date:** 2026-10-25
**Evidence:** Judgment, no evidence. The window itself was settled in
[ADR-0016](0016-retention.md); this record changes only who may change it.
**Supersedes:** [ADR-0016](0016-retention.md) on the fleet-core period only

## Context

ADR-0016 set the fleet-core retention at 90 days and called retention "the primary privacy
control", because a stable salt means the rows stay personal data for as long as the salt exists
([ADR-0006](0006-pseudonymization.md)). That reasoning stands.

What ADR-0016 did not consider is that the window is set by the person who runs Cardo, and that
person is often not the person who owns the data-retention rule. An organization with a works
council, a legal function or a works agreement has a retention rule of its own, and it may be
shorter than 90 days. With a fixed window, such an organization has two bad choices: shorten the
window everywhere in Cardo, which the record does not allow, or leave Cardo's window longer than
the organization's own, which is a promise Cardo cannot keep.

The window is also the only control that actually bounds exposure. Pseudonymization does not: the
salt exists, so the rows are personal data until they are gone. A default therefore is not a
convenience; it is the value the privacy claim rests on.

## Decision

1. **90 days is the default.** An organization that changes nothing keeps 90 days on the fleet-core
   tier, which is what ADR-0016 decided and what the shipped tables' TTL already does.
2. **The organization may change it**, through the setting the console already exposes. The value
   the console shows is the effective one, and it says "(default)" when nothing has been set, so an
   operator never has to guess which window is in force.
3. **A longer period is a decision the engineers should hear about.** The rows that live longer
   are the ones tied to a code name, and a code name is pseudonymous, personal data. When the window
   is above the default, the console says so and names what the organization has to tell its
   engineers.
4. **When an engineer notice exists, it states the value.** The notice is the organization's own
   disclosure to the people being measured; a retention window it does not mention is a window the
   engineers cannot question. The notice is not built yet, and this record says what it must carry
   when it is.
5. **The other tier periods are unchanged.** The security tier stays at one year, the personal tier
   at 30 days, and aggregates that carry no pseudonym stay indefinitely
   ([ADR-0016](0016-retention.md), [ADR-0002](0002-three-tier-data-model.md)). Only the fleet-core
   period is decided here.

## Consequences

- **The console shows the effective window, not the stored one.** An unset value reads as
  "90 (default)", and the explanation of why 90 days is the default sits next to it: it is long
  enough to compare a few weeks before and after a tool is rolled out, and short enough that rows
  tied to a code name do not linger. The grouped totals carry no code name and are kept anyway, so
  long-term trends do not need longer raw data.
- **An organization that must delete sooner can.** A shorter window is applied by the poller after
  every run and by `cardo prune`, and it moves the tables' TTL with it, so it works in both
  directions.
- **Setting the window to zero still means "keep everything"**, and it is now the operator's
  explicit choice rather than the state a fresh installation happens to be in.
- **The default is a floor for the claim, not a ceiling for the operator.** It can be lowered, which
  the five-person group size may not be.
- **The engineer's notice has to carry the number.** Until the notice exists, an organization that
  raised the window above 90 days has said it in the console and nowhere else.

## Rejected alternatives and why

- **A fixed 90 days, with no way to change it.** Simpler, and it keeps one number to explain. It
  also means an organization whose own retention rules are shorter cannot honour them in Cardo
  without a code change, and that is a promise Cardo would be silently breaking on their behalf.
- **No default at all.** Every window then has to be chosen, and an unconfigured installation
  would keep everything, which is the opposite of what ADR-0016 decided and the worst possible
  default for a privacy control.
- **Let the salt's custody decide the window.** The salt holder is the security team, and it holds
  a key, not a retention rule. Keeping the two apart is what lets either be changed without the
  other.
- **A default per organization type, chosen at install time.** The single-engineer setup and the
  team pilot would get different windows, and the smaller one would get the weaker privacy posture
  for no reason that survives contact with an engineer's objection.
