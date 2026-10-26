# ADR-0044 — The board is a console of four surfaces; Install is its repair room

**Status:** Accepted
**Date:** 2026-10-18
**Depends on:** ADR-0029 (minimum group size), ADR-0043 (poller in the stack), ADR-0045 (catalog), ADR-0046 (directory)

## Context

The board began as an installer: a wizard with a step rail, built to make one machine work without
discipline. It then grew an Overview, a Catalog, a Teams directory and Settings. Install remained a peer
tab, which implied an ongoing task: after a successful run it answered nothing a person opens daily.

Install does answer one question that nothing else does: which step failed, why, and how to fix it —
with per-step cause and recovery, the read-back proof, and the connect step. That is a repair room, not
a workspace.

## Decision

1. The console has four peer surfaces: **Overview, Catalog, Teams, Settings**. Install is not a tab.
2. Install stays reachable in two places: the next-action block on Overview when a check fails or the
   proof is missing, and **Settings → Installation → Open install and repair**.
3. Settings owns the installation knobs a person changes after the fact: account type (single engineer
   or team pilot), project directory, and the way back into the repair room.
4. Account type is a stored setting (`pilot_mode`), not a per-browser preference. It changes which
   steps the installer shows; it does not reinstall, remove data, or alter the running stack.
5. "Reinstall" is not a word the product uses. `cardo up` repairs in place, and the button says so.
   A destructive reset, if it ever exists, is a separate action with a typed confirmation.

## Consequences

- The navigation stops promising daily work that does not exist; four surfaces, each with a job.
- The step-level cause and recovery remain available at the moment they are needed, one click away.
- The installer can grow pilot-only steps without adding permanent navigation weight.
- A reader of the roadmap may wonder where Install went; the Settings group and this ADR answer it.
