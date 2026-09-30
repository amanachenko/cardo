# First real hook payloads and OTel from Claude Code — snapshot 2026-09-24

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code **2.1.281** on Windows 11, running in the Orca terminal
> (`terminal.type`), launched by the operator with
> `claude --settings deploy/managed-settings/local-evaluation.json` in a throwaway project. The
> reference stack received it: otelcol-contrib 0.161.0 with the collector config at commit
> `716daf5`, and ClickHouse 26.6. About twenty minutes and three sessions, following a scripted
> walk through the thirteen events. **Everything below is measured** unless it says documentation.
>
> **What could be measured, and what could not.** Hook *field names* come from
> `cardo.received_keys`, which the collector records for every payload whether or not it keeps the
> field. Hook *values* were stored only for allowlisted fields, so the value or type of any field
> that was dropped is unknown. OTel is stored as sent, minus identity and content attributes.

## 1. Which events arrived

| Event | Rows | |
|---|---|---|
| `InstructionsLoaded` | 9 | |
| `UserPromptSubmit` | 13 | |
| `SubagentStop` | 8 | 6 are an internal helper, not a subagent anyone asked for (section 6) |
| `SessionEnd` | 3 | including one sent as Claude Code exited on `/exit` |
| `UserPromptExpansion` | 2 | |
| `PermissionRequest` | 2 | |
| `SubagentStart` | 2 | |
| `PreModelSwitch`, `ConfigChange`, `PreCompact`, `PostCompact` | 1 each | |
| **`SessionStart`** | **0** | **Registered, never run** (below) |
| `PermissionDenied` | 0 | Not exercised. Auto mode allowed the `git push --force` meant to provoke it, and the push then failed because the directory is not a repository |

**`SessionStart` HTTP hooks do not run.** Claude Code's own OTel shows the hook registered
(`claude_code.hook_registered`: `hook_event=SessionStart`, `hook_type=http`,
`hook_source=flagSettings`). On every `SessionStart` (`startup`, `compact` and `clear`),
`claude_code.hook_execution_complete` reports two hooks run. Those are the operator's own two
`SessionStart` command hooks in user settings. The same arithmetic holds on every other event: for
`UserPromptSubmit`, one command hook plus Cardo's HTTP hook makes two. The hooks documentation,
fetched the same day, lists `http` as a supported type for `SessionStart`.

## 2. Observed field names

Common to every event: `session_id`, `transcript_path`, `cwd`, `scratchpad_dir`,
`hook_event_name`. `prompt_id` appears once a prompt exists. `permission_mode` appears on
`UserPromptSubmit`, `UserPromptExpansion`, `PermissionRequest` and `SubagentStop` only.

| Event | Event-specific fields observed | 2026-09-23 note said |
|---|---|---|
| `UserPromptSubmit` | **`prompt`** | `user_input` |
| `UserPromptExpansion` | `expansion_type`, `command_name`, `command_source`, `command_args`, `prompt` | `command_name`, `expanded_prompt` |
| `SessionEnd` | **`reason`** | `session_end_reason` |
| `PreCompact` | **`trigger`**, `custom_instructions` | `compaction_reason` (and token counts, in the 09-20 note) |
| `PostCompact` | **`trigger`**, `compact_summary` | `compaction_reason` |
| `ConfigChange` | **`source`**, `file_path` | `config_source` |
| `InstructionsLoaded` | `load_reason`, `memory_type`, `file_path`, and `globs`, `trigger_file_path` on lazy loads. **No `content_hash`** | `load_reason`, `file_path`, `content_hash` |
| `PermissionRequest` | `tool_name`, `tool_input`, `effort`, and `permission_suggestions` on the main thread or `agent_id`, `agent_type` in a subagent. **No `tool_use_id`** | `tool_name`, `tool_use_id`, `tool_input` |
| `SubagentStart` | `agent_id`, `agent_type` | the same |
| `SubagentStop` | `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message` (not always), `background_tasks`, `session_crons`, `stop_hook_active`, `effort` (not always) | `agent_id`, `agent_type`, `last_assistant_message` |
| `PreModelSwitch` | `from_model`, `to_model`, `requested_model`, `source`, `pricing`, `context_tokens`, `prompt_cache_warm`, `cache_ttl`, `estimated_cache_write_usd` | `from_model`, `to_model` |

Values seen for kept fields: `memory_type` `User` and `Project`; `load_reason` `session_start`,
`nested_traversal` and `path_glob_match`; `command_source` `projectSettings`; `expansion_type`
`slash_command`; `permission_mode` `default` and `auto`; `effort.level` `xhigh`.

`effort` appears only on `PermissionRequest` and `SubagentStop`. After `/clear`, the new session's
`InstructionsLoaded` events arrived just *after* its first `UserPromptSubmit`: loading is lazy. No
`InstructionsLoaded` with `load_reason=compact` followed the `/compact`.

### Why the 2026-09-23 note was wrong

That note was built from WebFetch summaries of the hooks page, not from the page itself. A re-fetch
on 2026-09-24 reports `reason`, `trigger` and `source` as the matcher fields for those events, and
does not list `content_hash`. But it still says `UserPromptSubmit` carries `user_input` and
`PermissionRequest` carries `tool_use_id`, and the real payloads have neither. Either the page moved
overnight, or the summarizer turned matcher names into plausible field names. There is no telling
which. **A summarized documentation page is not evidence of a field name.** `cardo.received_keys`
from a real session is.

## 3. Content in hook payloads

For the record of what the collector discards: `prompt`, `command_args`, `custom_instructions`,
`compact_summary`, `last_assistant_message`, `tool_input`, `permission_suggestions` (proposed
permission rules, which embed paths and commands), `globs`, `trigger_file_path`, `file_path`,
`cwd`, `transcript_path`, `scratchpad_dir`, `agent_transcript_path`.

A sweep of every stored value in all three collector tables found none of: the operator's username,
a drive or home path, `.claude`, `@`, the sandbox's file names, the words typed in the test, or the
command Claude ran. The only names stored were artifact names (section 7).

## 4. Delivery

- **The hook receiver refuses bodies over 100 KiB**, with HTTP 400 and
  `request body exceeds maximum allowed size: limit is 102400 bytes`, and **the collector logs
  nothing**. It is `webhook_event`'s default. Two `SubagentStop` events never arrived, and for exactly
  those two executions Claude Code's OTel recorded a non-blocking hook error. One was during
  `/compact`, where the reply is a whole summary. The size limit is the only refusal that could be
  reproduced; for the second event it is the likeliest cause, not a proven one.
  `max_request_body_size` raises the limit: at 4 MiB, a 3 MB body was accepted and a 5 MB body
  refused.
- **The only trace of a lost hook is on the client.** `claude_code.hook_execution_complete` carries
  `num_non_blocking_error`. The collector sees nothing it can count.
- `SessionEnd` arrives even when the process is exiting on `/exit`.
- Plain HTTP to `127.0.0.1` is allowed for HTTP hooks.
- `--settings` applies both the `env` block (OTel arrived) and the `hooks` block
  (`hook_source=flagSettings`).

## 5. OTel on an individual account

**Claude Code emits OTel on an individual (non-organization) account**, which
[risks.md](../../risks.md) #12 had left open. `user.email` is present, so the collector produced a
pseudonym. `organization.id` is present, with one value across the run.

Resource attributes: `host.arch`, `os.type`, `os.version`, `service.name`, `service.version`, and
the bundle's `cardo.cohort`, which is also copied onto every log record. Record attributes on every
event: `session.id`, `prompt.id`, `app.version`, `terminal.type`, `organization.id`,
`event.name`, `event.sequence`, `event.timestamp`.

| Event | Rows | Notable attributes |
|---|---|---|
| `api_request` | 33 | `model`, `effort`, `query_source`, `agent.name`, `skill.name`, token counts, `cost_usd`, `cost_usd_micros`, `ttft_ms`, `speed` |
| `assistant_response` | 21 | `response_length`, `model`, `query_source`; the response itself redacted |
| `user_prompt` | 18 | `prompt_length`, `command_name`, `command_source` |
| `tool_decision` | 9 | `tool_name`, `tool_use_id`, `decision`, `source` (`config`, `user_temporary`, `user_reject`), `tool_source` |
| `tool_result` | 8 | `tool_name`, `tool_use_id`, `success`, `duration_ms`, `error_type`, payload sizes |
| `permission_mode_changed` | 13 | `from_mode`, `to_mode`, `trigger` (`shift_tab`) — **undocumented; plan-mode entry and exit** |
| `compaction` | 1 | `trigger`, `pre_tokens`, `post_tokens`, `duration_ms`, `success` — **undocumented; the token counts the hooks lack.** Measured: 8731 to 5223 tokens in 67 s |
| `subagent_completed` | 2 | `agent_type`, `agent.source`, `is_built_in`, `model`, `final_model`, `total_tokens`, `total_tool_uses`, `duration_ms` — undocumented |
| `skill_activated` | 2 | `skill.name`, `skill.source`, `invocation_trigger` — undocumented |
| `hook_registered`, `hook_execution_start`, `hook_execution_complete` | 29, 62, 62 | `hook_event`, `hook_name`, `hook_type`, `hook_source`, success and error counts — undocumented |
| `managed_settings_resolved` | 1 | `managed_settings.sources`, `.source_behavior` — undocumented; a fleet-conformance input for tier 0 |
| `plugin_loaded`, `mcp_server_connection`, `feedback_survey` | 2, 1, 2 | undocumented |

Metrics: `session.count` (`start_type=fresh`), `token.usage`, `cost.usage`, `active_time.total`,
`code_edit_tool.decision`, `lines_of_code.count`.

## 6. `SubagentStop` fires for a hidden helper

Six of the eight `SubagentStop` rows have no matching `SubagentStart`. Each is followed within one
to three seconds by an `api_request` with `query_source=prompt_suggestion`: it is Claude Code's
prompt-suggestion helper, which runs after every turn. Its `agent_type` is not one of the built-in
names the collector knew, so under the rules of the time it was stored as `custom`, and the real
name is unknown. OTel's `subagent_completed` counted only the two subagents that were asked for.
**Counting subagents from `SubagentStop` over-counts.** Pair starts and stops by `agent_id`, or use
OTel.

## 7. Artifact names with every content flag off

- **`skill.name` carries the real name of a custom slash command** on `api_request` and on the
  `cost.usage` and `token.usage` metrics: `cardo-hello` and `standup` here, both project commands.
  Claude Code's own `skill_activated` event says `custom_skill` for the same invocation, and
  `user_prompt` says `command_name=custom`. The redaction that `OTEL_LOG_TOOL_DETAILS` controls is
  not applied consistently.
- `agent.name` is `custom` for a project-defined subagent, and `query_source` is `agent:custom`.
- `plugin.name` named two built-in plugins (`telemetry`, `agents-md`). A private plugin's name was
  not observed either way.
- `hook_name` on the hook-execution events is `Event:matcher`, for example
  `PreModelSwitch:claude-sonnet-5`. A matcher can name a tool or an MCP server.

## 8. Permission waits

`PermissionRequest` has no `tool_use_id`, so it cannot be joined to `tool_decision` the way the
2026-09-23 note planned. The join that works is on `session_id`, `prompt_id` and `tool_name`,
taking the next `tool_decision` whose `source` is a person's (`user_*`). It gave:

| Tool | Asked by | Answer | Wait |
|---|---|---|---|
| `Edit` | main thread | `user_reject` | 13.6 s |
| `Bash` | the Explore subagent (the hook carries `agent_type`) | `user_temporary` | 12.0 s |

A call allowed by configuration or by auto mode (`source=config`) produces no `PermissionRequest`.

## What was not established

- The payloads of `SessionStart` and `PermissionDenied`.
- Whether `InstructionsLoaded` fires for an `@`-imported file (`load_reason=include`), and with
  which `file_path`.
- The types and values of the `PreModelSwitch` fields and of `trigger`, `reason` and `source`,
  beyond the matcher values the documentation lists. None was on the allowlist, so none was stored.
- The real `agent_type` of the prompt-suggestion helper.
- Whether the second lost `SubagentStop` was refused for size.
- Why the first of the three sessions has hook rows and no OTel. The collector was recreated at
  18:53 UTC, after that session ended.
