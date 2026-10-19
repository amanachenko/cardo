# ADR-0050 — Local transcript import is allowed; collection still never reads transcripts

**Status:** **Proposed** — narrows INV-1, so it needs the owner's sign-off before merge
**Date:** 2026-10-18
**Narrows:** [INV-1](../design/invariants.md), [ADR-0005](0005-collection-mechanism.md)
**Related:** ADR-0013 (no phone-home), ADR-0048 (coach), ADR-0049 (price catalogue)

## Context

INV-1 says no code path may open anything under `~/.claude/projects`, "not for debugging, not once,
not behind a flag". That rule exists because reading transcripts is the cheapest way to get rich
signal, and refusing it is what lets a sceptical engineer believe the privacy claim.

`cardo import` and `cardo watch` now read those files anyway. They exist because the product's other
promise — zero discipline — failed on day one: every engineer had months of history that no
dashboard could see, and the only ways to get it were (a) wait for new telemetry or (b) read what
Claude Code already wrote locally. The importer reads only session ids, timestamps, model names,
token usage and the *length* of each prompt. It discards the text, never writes it, and never sends
anything anywhere. But the letter of INV-1 is still broken, and the CI test that guards it passed
vacuously: the path is assembled with `filepath.Join(homeDir(), ".claude", "projects")`, so the
literal the test searches for never appears.

Leaving that as-is would be the exact failure mode the invariants file warns about: code routed
around the rule while the document still claims otherwise.

## Decision

1. **Split the rule by path, not by intent.** INV-1 becomes: *the collection path never reads
   transcripts* (collector, hooks, poller, views, server). It keeps its original force for everything
   that runs without the person's participation.
2. **The local import is a separate, opt-in path.** It runs only when the person invokes `cardo
   import`, installs `cardo watch -install`, or runs the installer's visible `history` step. It runs
   on their own machine, under their own account; there is no remote trigger and no server-side
   caller. The `history` step being on by default is recorded as an open gap below: it should become
   a checkbox, not an assumption.
3. **What it may read is fixed and narrow:** session id, timestamps, model, token counts, prompt
   uuid/parent chain and prompt length. No prompt text, no tool inputs or outputs, no file paths, no
   diffs. `internal/transcripts` returns `Len`, never the string.
4. **What it writes is the same bronze shape as telemetry,** marked `cardo.import=1` and
   `cardo.cohort=imported`, so imports can be identified and deleted without touching live rows.
5. **The CI guard gets stricter, not looser.** The INV-1 test now fails when a Go file other than the
   named import files assembles the transcript directory, and the allowlist is asserted to match real
   files. A future feature that wants the transcripts has to edit this ADR, not a list of tokens.

## Rejected alternatives

- **Keep INV-1 literal and drop the importer.** The product then shows nothing for existing work, and
  the first real user sees an empty console. It also does not reduce exposure: the transcripts exist
  either way; the question is who reads them and what leaves.
- **Read transcripts inside the collector.** That would put transcript access on the always-on path,
  which is strictly worse: it runs without consent and ships rows worth of content risk.
- **Ask people to export with a vendor tool and paste the result.** Same data, more steps, more
  places to leak.
- **Quietly rename the path or string-concatenate around the test.** This is what the current code
  accidentally does. Making the test blind is not a decision.

## Consequences

- The privacy story becomes two-tier and must be told that way: **collection is content-free and
  automatic; import is metadata-only and manually invoked.** README, `docs/overview.md` and
  `docs/console-coach-history.md` say exactly that, and no surface claims "never reads transcripts"
  without the qualifier any more.
- A person who never runs `cardo import` is in exactly the position INV-1 described.
- The importer is a local capability, not an organizational one: `cardo watch` sees one machine, and
  cross-machine history still needs an explicit export/import (roadmap #5).
- Cost and tool-call quality differ between the two paths: imported sessions have no tool calls and
  are priced at list price, and every surface labels them accordingly.
- If the owner rejects this ADR, the importer and watcher must be removed from the release; leaving
  them in while INV-1 stands unchanged is not an acceptable third state.
- **Open gap:** the installer's `history` step imports and installs the watcher by default. Consent is
  visible (it is a named step) but not granular; it should become an explicit checkbox before a
  wider pilot.

## Evidence

`cardo import` on 2026-10-18 read 415 files / 84 sessions / 2505 prompts and produced 51 509 rows with
zero prompt text: `internal/transcripts` records `Prompt{Len: n}` and the writer emits only ids,
models, token counts and costs. `go test ./test/ -run TestINV1` passes with the stricter path check.
