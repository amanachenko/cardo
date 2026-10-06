# A real `/model` switch through `PostModelSwitch` — snapshot 2026-10-06

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code **2.1.291** on Windows 11, in a new working session with the hooks of
> [ADR-0039](../adr/0039-hooks-wait-at-most-one-second.md) in user settings, against the reference
> stack on the same machine. Field names are from `cardo.received_keys`; the collector stored no
> value of a field it dropped. OTel is the same session's, from bronze. **Everything is
> measured.**

## 1. What arrived

Two `/model` commands, 62 s apart and before the session's first request: Opus to Sonnet, then
back to Opus. Each fired one `PostModelSwitch` with `source` `command`. Each `/model` is also a
prompt in OTel: a `claude_code.user_prompt` event whose prompt id the hook carries.

- **Received keys:** `session_id`, `transcript_path`, `cwd`, `scratchpad_dir`, `hook_event_name`,
  `prompt_id`, `from_model`, `to_model`, `requested_model`, `source`, `pricing`, `context_tokens`,
  `prompt_cache_warm`, `cache_ttl`, `estimated_cache_write_usd`.
- **Two more than the 2.1.289 resume:** `prompt_id` and `scratchpad_dir`. Whether that comes from
  the version or from a `/model` rather than a resume is not known.
- **Kept:** `model_switch_source` `command`, `requested_model` `sonnet` then `opus`, `to_model`
  `claude-sonnet-5-5` then `claude-opus-5-5`, with no date in the ID, matching the hook name.
  `context_tokens` 0, `prompt_cache_warm` false, `cache_ttl` `1h` and `estimated_cache_write_usd` 0
  on both: there was no context to rewrite.

The fixture `test/fixtures/hooks/2.1.291/PostModelSwitch.json` carries these keys, with the zero
and false values kept as observed.

## 2. The hook's time

OTel's `hook_execution_start` and `hook_execution_complete` for `PostModelSwitch:<model>` both
carried `prompt.id`. The hook took 41 ms and 29 ms (`total_duration_ms`), one hook each, both
successful, against a one-second timeout.

`sql/clickhouse/008` says neither the event nor its `hook_execution_start` carries a prompt id,
which held for the 2.1.289 resume. The view matches a `PostModelSwitch` by session and model alone,
so it found both switches anyway. With a prompt id on both sides, a later view could match on it,
and two switches to the same model in one session would no longer share the earlier time.

## 3. In `silver_context_event`

Both switches are there, with `clock` `client` and `switch_source` `command`.

- **The switch to Sonnet** has no next request: it was undone before any request was made.
- **The switch back to Opus** has as its next request the session's first main-thread one:
  `next_request_cost_usd` 0.18, 21,195 cache-creation tokens, 30,360 cache-read tokens. That is the
  session starting, not the switch. Claude Code's own fields say the same: no context, nothing to
  rewrite, an estimate of zero. The view reads the next main-thread request as what a switch cost,
  which holds once a session has context, and not for a switch made before its first main-thread
  request.

## Not measured

- `source` `picker`, or a switch from the SDK.
- A switch in the middle of a conversation on 2.1.291, with a warm cache.
- Whether a resume on 2.1.291 carries a prompt id.
