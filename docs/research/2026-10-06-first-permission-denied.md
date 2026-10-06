# The first `PermissionDenied`, and the same denial in OTel — snapshot 2026-10-06

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code **2.1.290** on Windows 11, in a working session in auto mode with the
> local bundle, against the reference stack on the same machine. Field names are from
> `cardo.received_keys`; the collector stored no value of a field it dropped. OTel is the same
> session's, from bronze. **Everything is measured** unless it says otherwise.

## 1. What arrived

Auto mode denied two `Bash` calls in one session, 25 s apart. Each fired one `PermissionDenied`,
the last of the twelve hooked events to arrive from a real Claude Code.

- **Received keys:** `session_id`, `transcript_path`, `cwd`, `scratchpad_dir`, `hook_event_name`,
  `prompt_id`, `permission_mode`, `effort`, `tool_name`, `tool_input`, `tool_use_id`, `reason`.
- **One more than the documented shape:** `reason`. It is a generic name, so it is not on the
  allowlist, and its value was not stored. Whether it is an enum or prose is not known.
- **Kept:** `permission_mode` (`auto`), `effort_level` (`xhigh`, from `effort.level`), `tool_name`
  (`Bash`), `tool_use_id`, `prompt_id`, `session_id`.

The fixture `test/fixtures/hooks/2.1.290/PermissionDenied.json` carries these keys with invented
values.

## 2. The same denial in OTel

Each `PermissionDenied` matched exactly one `claude_code.tool_decision` on `tool_use_id`, with the
same prompt id, `decision=reject` and `source=config`, and no `tool_result`.

- **`config` is also what auto mode's approvals carry:** 234 `config`/`accept` decisions on this
  stack, against these two `config`/`reject`. `silver_tool_call` reads `config` as "a permission
  rule or mode". In OTel alone, an auto-mode denial looks like any other refusal by configuration.
  The hook is what says it was auto mode, and `tool_use_id` joins the two.
- **Two clocks again:** the hook row's receive time was 0.41 s after OTel's decision time the first
  time, and 0.44 s before it the second.

## 3. Three hook rows without an event name

On the same stack that day, two `UserPromptSubmit` rows and one `SubagentStop` row, received
between 13:30 and 13:42 UTC, had an empty `EventName`. Their `hook_event_name` attribute and the
rest of the allowlist were intact. They were the only hook rows between 12:30 and 15:00, so the
views did not see them.

- **Cause not established.** Which collector configuration was running then was not recovered.
- **Not reproduced.** The configuration merged the same day names every event: a session after
  the collector was redeployed sent three, all named.
- **Repaired by hand:** a copy of each row with `EventName` taken from `hook_event_name`, then the
  original deleted. `EventName` is in the table's sort key, so it cannot be updated in place.
  In `SELECT * REPLACE (LogAttributes['hook_event_name'] AS EventName) ... WHERE EventName = ''`,
  ClickHouse resolves `EventName` to the alias and matches nothing; filter in a subquery first.
- **To check for a recurrence:** `SELECT count() FROM cardo.bronze_hook_events WHERE EventName = ''`.

## Not measured

- A denial of a tool other than `Bash`, or from inside a subagent, where `agent_id` and
  `agent_type` might appear.
- Whether a deny rule in settings, outside auto mode, fires `PermissionDenied`. The documentation
  says it fires on auto-mode denials only.
- The value of `reason`.
