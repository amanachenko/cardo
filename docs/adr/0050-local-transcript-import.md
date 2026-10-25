# ADR-0050 — Cardo never reads transcripts on its own; `cardo import` is the one exception

**Status:** Accepted
**Date:** 2026-10-18
**Narrows:** [INV-1](../design/invariants.md), [ADR-0005](0005-collection-mechanism.md)
**Related:** ADR-0013 (no phone-home), ADR-0048 (coach)

## Context

INV-1 says no code path may open anything under `~/.claude/projects`, "not for debugging, not once,
not behind a flag". That rule exists because reading transcripts is the cheapest way to get rich
signal, and refusing it is what lets a sceptical engineer believe the privacy claim.

The importer exists because the product's other promise — zero discipline — failed on day one: every
engineer had months of history that no dashboard could see, and the only ways to get it were (a) wait
for new telemetry or (b) read what Claude Code already wrote locally. The importer reads only session
ids, timestamps, model names, token usage and the *length* of each prompt. It discards the text,
never writes it, and never sends anything anywhere.

The CI test that guards INV-1 passed vacuously: the path is assembled with
`filepath.Join(homeDir(), ".claude", "projects")`, so the literal the test searched for never
appeared. That is the exact failure mode the invariants file warns about: code routed around the rule
while the document still claims otherwise. The test now also catches the assembled-path form.

An earlier version of this record also proposed `cardo watch`: a background service that re-read the
transcripts every minute and installed itself as a launchd agent or a systemd user unit. It is not
part of this decision. The owner narrowed the exception, and the watcher is gone.

## Decision

1. **Cardo never reads transcripts on its own.** No automatic code path — collector, hooks, poller,
   views, server, installer — may open anything under `~/.claude/projects/` or its platform
   equivalents. Not for debugging, not once, not behind a flag. Nothing runs in the background on an
   engineer's machine unless they choose to run it.
2. **`cardo import` is the one exception, and it is a command a person runs.** It runs only when the
   person invokes it on their own machine, under their own account. There is no remote trigger, no
   server-side caller, and no service installed afterwards. It is not part of "install everything":
   the installer's `history` step is an unticked checkbox, and "install all" never takes it.
3. **What it may read is fixed and narrow:** session id, timestamps, model, token counts, prompt
   uuid/parent chain and prompt length. No prompt text, no tool inputs or outputs, no file paths, no
   diffs. `internal/transcripts` returns `Len`, never the string.
4. **What it writes is the same bronze shape as telemetry,** marked `cardo.import=1` and
   `cardo.cohort=imported`, so imports can be identified and deleted without touching live rows.
5. **A session that already arrived through live telemetry is skipped,** and the importer says how
   many it skipped. Counting such a session twice would double its cost, tokens and prompts.
6. **The CI guard gets stricter, not looser.** The INV-1 test fails when a Go file other than the
   named import files assembles the transcript directory, and the allowlist is asserted to match real
   files. A future feature that wants the transcripts has to edit this ADR, not a list of tokens.

## Rejected alternatives

- **Keep INV-1 literal and drop the importer.** The product then shows nothing for existing work, and
  the first real user sees an empty console. It also does not reduce exposure: the transcripts exist
  either way; the question is who reads them and what leaves.
- **Read transcripts inside the collector.** That would put transcript access on the always-on path,
  which is strictly worse: it runs without consent and ships rows worth of content risk.
- **A background watcher (`cardo watch`) that re-imports every minute.** It reads files the person did
  not ask it to read, on a schedule, on a machine they may no longer be sitting at. It also wrote the
  salt and the ClickHouse password into world-readable files. A one-time command, chosen by the
  person, is the whole of what this exception needs.
- **Ask people to export with a vendor tool and paste the result.** Same data, more steps, more
  places to leak.
- **Quietly rename the path or string-concatenate around the test.** This is what the code
  accidentally did. Making the test blind is not a decision.

## Consequences

- The privacy story is two-tier and must be told that way: **collection is content-free and automatic;
  the import is metadata-only, manually invoked, and leaves nothing running.** README,
  `docs/overview.md` and `docs/console-coach-history.md` say exactly that.
- A person who never runs `cardo import` is in exactly the position INV-1 described.
- The importer is a local capability, not an organizational one: it sees one machine, and cross-machine
  history still needs an explicit export/import (roadmap, Step 3).
- Cost and tool-call quality differ between the two paths: imported sessions have no tool calls and
  are priced at list price, and every surface labels them accordingly.

## Evidence

`cardo import` on 2026-10-18 read 415 files / 84 sessions / 2505 prompts and produced 51 509 rows with
zero prompt text: `internal/transcripts` records `Prompt{Len: n}` and the writer emits only ids,
models, token counts and costs. `go test ./test/ -run TestINV1` passes with the stricter path check,
and `internal/transcripts` has its own tests: the lengths and token counts are right, and no returned
field contains prompt text.
