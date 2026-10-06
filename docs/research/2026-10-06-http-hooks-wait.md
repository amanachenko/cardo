# HTTP hooks wait for the collector, and what a timeout does — snapshot 2026-10-06

> **This is a dated snapshot and is never edited.** If it is stale, write a new dated note. See
> [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Sources:** Claude Code **2.1.289** on Windows 11, against the reference stack on the same
> machine. Sections 1 and 5 come from working sessions with the shipped local bundle. Sections 2 to
> 4 come from isolated `claude -p` runs on Haiku: an empty folder, `--setting-sources project`, and
> a `--settings` file holding only the hook under test plus OTel to the local collector, so that no
> other hook ran. A hook's time is Claude Code's own `hook_execution_complete.total_duration_ms`;
> what a prompt waited is its `duration_ms` less `duration_api_ms`. Quotations are from Claude
> Code's hooks documentation as fetched on this date. **Everything is measured** unless it says
> documented.

## 1. "socket hang up"

In a working session, a `UserPromptSubmit` prompt showed `UserPromptSubmit hook error: socket hang
up`, and no hook row arrived. Claude Code's `hook_execution_complete` for it: three hooks, two
succeeded, one non-blocking error, 7,355 ms in all. The other two were PowerShell command hooks.

The collector's `webhook_event` receiver had its default timeouts, 500 ms to receive a request's
headers and 500 ms to answer. Reproduced against the running collector with a hand-written request:

- an idle POST is answered in about 4 ms;
- headers finished 0.8 s late: the connection is closed with no response, and nothing is stored;
- the body sent 0.8 s after the headers: the event is stored, and the connection is closed with no
  response.

The collector logs nothing in either case. The receiver accepts at most 10 s for each timeout and
refuses 30 s at startup. It also ignores a key it does not know: a misspelt `read_timeoutx` passed
`otelcol validate`. Fixed in `ae14d25`.

## 2. Claude Code waits for every HTTP hook

Documented: *"By default, hooks block Claude's execution until they complete."* And of `async`:
*"Add `"async": true` to a command hook's configuration to run it in the background without
blocking Claude. This field is only available on `type: "command"` hooks."* The default `timeout` is
600 s for `http` hooks, lowered to 30 s on `UserPromptSubmit` and `PreModelSwitch`. `SessionEnd`
hooks share a 1.5 s budget.

One `UserPromptSubmit` HTTP hook, aimed at each kind of collector:

| Hook URL | Hook time | Outcome | The prompt waited |
|---|---|---|---|
| none (baseline, two runs) | — | — | 58 and 68 ms |
| the running collector | 8 ms | success | 70 ms |
| a closed port on `127.0.0.1` (connection refused) | 4 ms | error | 63 ms |
| …with `timeout: 1` | 4 ms | error | 72 ms |
| a name that does not resolve (`.invalid`) | not read | — | 259 ms |
| …with `timeout: 1` | not read | — | 208 ms |
| `192.0.2.1`, which drops packets | 21,029 ms | error | 21,092 ms |
| …with `timeout: 1` | 1,003 ms | cancelled | 1,066 ms |
| a listener that accepts and never answers | 30,003 ms | cancelled | 30,063 ms |
| …with `timeout: 1` | 1,003 ms | cancelled | 1,062 ms |
| …with `timeout: 0.5` | 501 ms | cancelled | 566 ms |

- These runs left `async` out. The shipped bundle's hooks, which set `async: true`, were not timed
  this way; their failure in section 1 was shown under the prompt all the same.
- **The dropped-packet time is Windows' own.** `curl` to the same address gave up after 21.0 s.
  Other operating systems wait longer before giving up on a connection; not measured here.
- **A refused connection costs Claude Code nothing**, though `curl` took 2.0 s for the same port on
  the same machine.
- **A fractional `timeout` works.** A hook cut off at its timeout is counted as `cancelled`, not as
  an error.
- Every run still got its answer: a failed or cancelled `UserPromptSubmit` HTTP hook did not stop
  the prompt.

## 3. Two events where an HTTP hook cannot be used freely

Documented: *"On `PreModelSwitch`, a hook canceled at its timeout blocks the model switch."* On most
other events, *"a timed-out hook renders no decision"*, and the action goes ahead.

Documented: *"`SessionStart` and `Setup` support `command` and `mcp_tool` hooks … They don't support
`http`, `prompt`, or `agent` hooks."* Measured in a working session: `SessionStart:startup` reported
`num_hooks` 3, with four configured, three command hooks and Cardo's HTTP hook. This is consistent
with 2.1.281, where the HTTP hook was registered and never run.

## 4. `PostModelSwitch`

Documented: it receives *"the same fields as PreModelSwitch"*, with two more `source` values,
`"auto"` for a change Claude Code makes itself and `"resume"` for the model restored when a session
resumes, and it *"can't block, because the model has already changed"*.

Measured: resuming a `-p` session without `--model` fired one `PostModelSwitch`, from the account's
default `claude-opus-5-5` to the session's saved `claude-haiku-4-5-20251001`. The hook took 82 ms.
Starting a session with `--model haiku`, and resuming one with `--model sonnet`, fired none.

- **Received keys:** `session_id`, `transcript_path`, `cwd`, `hook_event_name`, `from_model`,
  `to_model`, `requested_model`, `source`, `pricing`, `context_tokens`, `prompt_cache_warm`,
  `cache_ttl`, `estimated_cache_write_usd`.
- **No `prompt_id`.** `PreModelSwitch` in 2.1.281 carried one.
- **Two spellings of the model.** Claude Code's `hook_execution_start` names the hook
  `PostModelSwitch:claude-haiku-4-5`, the canonical name, while the payload's `to_model` is the
  dated ID. The start event carries no `prompt.id` either.
- `source` was not stored: the collector then mapped it for `PreModelSwitch` only. Its value
  `"resume"` here is documented, not observed.

## 5. The repository attribute

With `OTEL_METRICS_INCLUDE_REPOSITORY=true` and the collector classifying it
([ADR-0035](../adr/0035-repository-identity-classified.md)), over a day of working sessions on
2.1.289 to 2.1.291:

- every OTel log record and every metric data point carried `cardo.repo.class` and `cardo.repo`;
- they were on the record or the data point, never on the resource;
- a repository matching `CARDO_ORG_REPOS` was stored as `org` with its `host/owner/name`;
- no `vcs.*` key was stored.

## 6. Not measured

- The round trip from a laptop over a VPN through the TLS proxy, which decides how close a healthy
  hook comes to a one-second timeout.
- How long macOS and Linux wait for a connection that never answers.
- What Claude Code shows the engineer when a hook on an event other than `UserPromptSubmit` times
  out.
