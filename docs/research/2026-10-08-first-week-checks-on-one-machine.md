# Three of the dogfood's first-week checks, answered on one machine — snapshot 2026-10-08

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** one engineer's working sessions on Windows 11, Claude Code **2.1.281 to 2.1.293**,
> with the local bundle, against the reference stack on the same machine. 13 sessions between
> 2026-09-24 and 2026-10-08, read from bronze with ClickHouse's `readonly` setting on. Counts only: no
> value of a dropped field was ever stored. **Everything is measured** unless it says otherwise.
> One person on one machine settles what Claude Code sends, not what a team does with it.

The [roadmap](../roadmap.md) lists checks for the dogfood's first week, each guarding a view that
relies on it. Three of them are answered here.

## 1. `event.sequence` orders the tool results within a prompt

A retry loop is the same tool failing several times in a row in one prompt, so it needs the order
of the tool results.

- **Present on every OTel log event,** including all 912 `claude_code.tool_result` records.
- **A counter per session.** It starts at 0, and in each of the 13 sessions it is unique and
  increases with time across every event, not only tool results.
- **Within a prompt:** 33 prompts had more than one tool result, the largest 131. In every one,
  ordering by `event.sequence` gives the same order as ordering by timestamp. No two tool results
  in a prompt shared a timestamp, so on this data the timestamp alone would have done. The
  sequence is what holds when two land in the same millisecond.
- `silver_tool_call` does not read it yet.

## 2. Failed tool calls are almost all shell exits

- **16 of 912 tool results failed.**
  - 13 `Bash` and 1 `PowerShell`, each with `error_type` `ShellError`.
  - 2 `Grep`, with `TelemetrySafeError`.
- **A shell failure is a non-zero exit,** and a failing test run is one. The command is a tool
  parameter, which the collector never stores ([INV-5](../design/invariants.md)), so the two cannot
  be told apart. A failure count that mixes shell exits with other tools' failures mostly counts
  shell exits.
- **No retry loop was seen.** No tool failed twice in a row within a prompt: the 16 failures are
  16 runs of one.

## 3. `claude_code.lines_of_code.count` arrives by `type`, per session, as delta

- **148 points:** 74 `added` and 74 `removed`. Every one has delta temporality and a session id.
- **Every metric on this stack** is delta and has a session id on every point. There are eight
  metric names, among them `commit.count` (16 points), `pull_request.count` (8) and
  `code_edit_tool.decision` (111).
- **A session's counts cannot say which lines were removed.** They may be lines the session added,
  or lines that were already there, so "lines removed soon after they were added"
  ([ADR-0031](../adr/0031-what-works-means.md)'s churn) is not computable from them.

## 4. The repository is on each record, never on the resource

This was first seen on 2026-10-06 ([note](2026-10-06-http-hooks-wait.md)), and it holds on more
data.

- **Log records:** 7,555 of 7,985 carry `cardo.repo.class` on the record; none on the resource.
- **Metric points:** 2,176 of 2,351 carry it on the point; none on the resource.
- **Every class is `org`.** No `vcs.*` key is stored.
- **Rows without one:**
  - the 343 from 2.1.281, recorded before the bundle asked for the repository;
  - one whole 2.1.293 session, which fits a session started outside any repository. That is not
    confirmed, because the working directory is not stored.

## Also found: 212 OTel log rows without an event name

All of them are from 2.1.290, between 13:13 and 13:50 UTC on 2026-10-06: the window in which
[the 2026-10-06 note](2026-10-06-first-permission-denied.md) found three hook rows without one.
That note repaired the hook rows. These OTel rows were not noticed then.

- **Their `event.name` attribute is intact:**
  - 59 `hook_execution_start` and 59 `hook_execution_complete`;
  - 27 `tool_decision` and 27 `tool_result`;
  - 23 `api_request` and 13 `assistant_response`;
  - 2 `user_prompt` and 2 `retention_sweep`.
- **The views select on `EventName`, so they do not see these rows.** That window's tool results
  are missing from the counts above, and from every view.
- **Not repaired here.** The cause is still the one the earlier note could not recover.
- **To check for a recurrence:**
  `SELECT count() FROM cardo.bronze_otel_logs WHERE EventName = ''`.

## What the trials still have to show, across people

- The same ordering, from other machines, operating systems and versions.
- Whether retry loops happen at all, and what share of shell failures are tests.
- The URL forms a team's remotes take, and any `external` host that is not a public one
  ([ADR-0035](../adr/0035-repository-identity-classified.md)).

## Not measured

- macOS and Linux.
- Tool results inside a subagent: whether they carry the parent prompt's id, and how their
  sequence interleaves with the main thread's.
- An `external` repository. Every session here worked in one the pattern matched.
