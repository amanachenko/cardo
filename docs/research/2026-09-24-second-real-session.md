# A second real session: imports, rules, model-switch values and two clocks — snapshot 2026-09-24

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code **2.1.281** on Windows 11. Same machine, sandbox project and launch as
> [the first session](2026-09-24-first-real-hook-payloads.md), about an hour later. The collector
> config was at commit `5766a7f`, run with `CARDO_ORG_ARTIFACTS=^cardo-` and
> `CARDO_ARTIFACT_NAMES=all`.
>
> The session ran from 20:16 to 20:19 UTC and had five steps: read a file under `api/`, `/model`, a
> Bash `rm -rf` of a folder that does not exist (in auto mode), `/compact`, and `/exit`. Since the
> first session, the sandbox's `CLAUDE.md` had gained an `@cardo-standards-v2.md` import, and it now
> had a path-scoped rule, `.claude/rules/cardo-security-v1.md` (`paths: api/**`). The measurement
> limits are the first note's: field names come from `cardo.received_keys`, and values exist only
> for allowlisted fields. **Everything below is measured** unless it says otherwise.

## 1. What arrived

12 hook rows and 77 OTel log records:

| Hook event | Rows |
|---|---|
| `InstructionsLoaded` | 5 |
| `UserPromptSubmit` | 2 |
| `PreModelSwitch`, `PreCompact`, `PostCompact`, `SubagentStop`, `SessionEnd` | 1 each |

- **`SessionStart`: none.** Its HTTP hook again did not run, on startup or after `/compact`.
  Claude Code's `hook_execution_start` reported two hooks each time, which were the operator's own
  two command hooks.
- **`PermissionDenied`: none** (section 6).

Both mappings added after the first session populated: `compaction_reason=manual` and
`session_end_reason=prompt_input_exit`.

A sweep of every stored attribute, hook and OTel, found no path, username, project name, email,
command line or prompt text, and no row had a body.

## 2. Instructions files

| `load_reason` | `memory_type` | `instructions_file` | `instructions_name` | Keys beyond the common ones |
|---|---|---|---|---|
| `session_start` | `User` | `CLAUDE.md` | | |
| `include` | `Project` | `other` | **`cardo-standards-v2.md`** | **`parent_file_path`** |
| `session_start` | `Project` | `CLAUDE.md` | | |
| `nested_traversal` | `Project` | `CLAUDE.md` (in `api/`) | | `trigger_file_path`, `prompt_id` |
| `path_glob_match` | `Project` | `rule` | **`cardo-security-v1.md`** | `globs`, `trigger_file_path`, `prompt_id` |

- **An `@`-import fires `InstructionsLoaded`**, at session start, with `load_reason=include`. It
  carries a field not seen before: `parent_file_path`, the path of the importing file. The
  collector dropped it, as it drops every path. The imported file's own name was kept because it
  matches the organization's pattern. So [ADR-0026](../adr/0026-stale-instructions-by-versioned-name.md)'s
  route works: a thin `CLAUDE.md` that imports a versioned file shows which version each machine
  loaded.
- **A path-scoped rule loads when a matching file is first read.** It arrives with
  `load_reason=path_glob_match` and the `prompt_id` of the prompt that caused it, about 50 ms after the
  nested `api/CLAUDE.md` loaded for the same read.
- The import is classified `other`, since it is neither a `CLAUDE.md` nor a rule. `load_reason`
  says what it is.

## 3. `PreModelSwitch` values

| Field | Stored value |
|---|---|
| `from_model` → `to_model` | `claude-sonnet-5` → `claude-opus-5-5` |
| `requested_model` | `opus` |
| `model_switch_source` (from `source`) | `command` |
| `context_tokens` | `51321` |
| `prompt_cache_warm` | `true` |
| `cache_ttl` | `1h` |
| `estimated_cache_write_usd` | `0.4106` |

Every value passed its type guard in the collector. Their types are therefore integer, number,
boolean and a short string, as [ADR-0027](../adr/0027-model-switch-cost.md) assumed. `pricing`
arrived again and was dropped.

- **`context_tokens` is the size of the conversation at that moment.** The preceding main-thread
  API request sent 51,264 tokens of context and received 57 tokens of output, and
  51,264 + 57 = 51,321.
- **`estimated_cache_write_usd` is `context_tokens` times one rate**: 51,321 tokens at $8.00 per
  million, to four decimal places.
- **The estimate assumed the whole context would be written again, and it was not.**
  - The first request on the new model read 36,777 tokens from cache and wrote 14,766. So 71% of
    the context was already cached for the new model, and the real cache write was 29% of the one
    estimated.
  - That whole request cost $0.1296, by its own `cost_usd`. Claude Code's estimate for the cache
    write alone was $0.4106.
  - The first session's switch, in the other direction, went the same way. Its fields were not
    kept then, so there is no estimate to compare, but the next request wrote 15,558 tokens of a
    43,668-token context.
  - The prefix that was already warm is presumably Claude Code's own system prompt and tools.
  - **The real cost of a switch is on the next main-thread `api_request`.** It is on the new model,
    and its `cache_creation_tokens` and `cost_usd` are stored as sent.

## 4. Context size

The context of each main-thread API request is `input_tokens + cache_read_tokens +
cache_creation_tokens` on `claude_code.api_request`:

| Request | Model | Context |
|---|---|---|
| 1 | `claude-sonnet-5` | 50,822 |
| 2 | `claude-sonnet-5` | 51,264 |
| 3 | `claude-opus-5-5` | 51,545 |
| 4 | `claude-opus-5-5` | 51,788 |
| `compact` | `claude-opus-5-5` | 53,783 |

- **The first request of this session was 50,822 tokens; in the first session it was 38,667.**
  The sandbox was the same, and its instructions files had grown by less than 1 KB. Within this
  session both models saw the same ~51k. Nothing Cardo receives explains the 12k difference,
  because Claude Code sends no breakdown of what the context holds.
- **OTel `compaction` reported `pre_tokens` 6,505 and `post_tokens` 3,191**, while the main
  thread's context was 51,788. The first session had the same pattern: 8,731 against about 44k.
  `pre_tokens` appears to count the conversation without the fixed prefix. That is two
  observations, not a definition.

## 5. Two clocks

**Hook rows carry the collector's receive time**, because hook payloads have no timestamp. **OTel
rows carry the client's time.**

- 29 hook rows across both sessions could be matched to Claude Code's own `hook_execution_start` for
  the same event, session and prompt.
- Every one of them was stamped **0.7 to 3.3 s before the client says the hook started**, which is
  impossible. The cause is clock offset.
- On this machine, the Docker VM that runs the collector and ClickHouse was 1.1 to 3.1 s behind
  Windows. This was measured by reading ClickHouse's `now64()` between two host readings. The
  offset was not steady: two readings a minute apart differed by more than a second and a half.

**So the first note's permission waits were measured across two clocks.** Measuring on the
client's clock alone gives shorter waits. The start is `hook_execution_start` with
`hook_name=PermissionRequest:<tool>`, and the end is the `tool_decision`:

| Tool | Answer | First note, two clocks | Client clock only |
|---|---|---|---|
| `Edit` | `user_reject` | 13.6 s | 12.3 s |
| `Bash` | `user_temporary` | 12.0 s | 10.6 s |

Each wait was about 1.3 s too long, 10 to 13% of it.

- **`hook_execution_start` for `PermissionRequest` fires on every permission prompt**, as long as a
  `PermissionRequest` hook is registered. The bundle registers one.
- It carries `prompt.id` and names the tool in `hook_name`.
- It is undocumented.

`hook_execution_complete` put 1.9 s and 2.8 s of those waits inside the hooks themselves. The
operator's own `PermissionRequest` command hook ran alongside Cardo's. On the one event where only
Cardo's HTTP hook was registered, `PreModelSwitch`, Claude Code reported 17 ms for the hook
execution. The first session's reading was 11 ms.

## 6. Auto mode, and still no `PermissionDenied`

In auto mode, `rm -rf ~/cardo-no-such-folder` ran:

- `tool_decision` was `accept` with `source=config`;
- there was no `PermissionRequest`, and no `PermissionDenied`.

The first session's `git push --force` went the same way.

The sandbox's settings allow only `Bash(echo:*)`. Either auto mode reports its classifier's
approvals as `config`, or a rule in the operator's user settings allowed both commands. Those
settings were not examined. `PermissionDenied` remains unobserved.

## 7. `SubagentStop` from compaction

- **`/compact` fired `SubagentStop`**, with an empty `agent_type` and no `SubagentStart`, right after
  the `compact` API request. The first session's `/compact` did the same.
- This session made no prompt-suggestion requests, and fired no `SubagentStop` after its ordinary
  turns. **The prompt-suggestion helper does not run after every turn.**

Counting subagents still means pairing starts and stops on `agent_id`.

## What was not established

- The payloads of `PermissionDenied` and `SessionStart`.
- Whether auto mode's approvals always say `source=config`.
- Whether `hook_name` carries an MCP server's name. No MCP tool was called.
- What the fixed context at the start of a session is made of, and why it differed by 12k tokens
  between two sessions an hour apart.
