# ADR-0039 — Every hook waits at most one second, and a model switch is read after it happens

**Status:** Accepted
**Date:** 2026-10-06
**Evidence:** `2026-10-06-http-hooks-wait.md`, measured on Claude Code 2.1.289, with the hooks
documentation it quotes.
**Supersedes:** [ADR-0024](0024-bundle-configures-telemetry-only.md),
[ADR-0027](0027-model-switch-cost.md).

## Context

[ADR-0024](0024-bundle-configures-telemetry-only.md) kept
[ADR-0011](0011-hook-transport-http.md)'s transport, `type: "http"` hooks with `async: true`, and
made each handler `type`, `url` and `async`, with no `timeout`. It rejected synchronous hooks,
because *"they put the collector on the critical path of every prompt, and on `PreModelSwitch` a
timed-out hook blocks the switch outright."*

That rejected alternative is what was built. `async` is only available on command hooks. On an HTTP
hook it does nothing, and Claude Code waits for every response. One `UserPromptSubmit` hook,
measured:

| Collector | The prompt waited |
|---|---|
| healthy | nothing noticeable (8 ms) |
| connection refused | nothing noticeable, with an error shown |
| packets dropped, as for a laptop off the VPN | 21 s on Windows |
| accepts and never answers | 30 s, the event's default timeout |
| either of the last two, with `timeout: 1` | 1 s |

The default timeout is 600 s on most other events. Two more facts came with the measurement:

- **A `PreModelSwitch` hook cancelled at its timeout blocks the model switch.** That was already
  true: a hung collector blocks switches after 30 s today.
- **Claude Code does not run HTTP hooks on `SessionStart`.** Since 2.1.281 the hook has been
  registered and never run.

The team setup chosen for the dogfood, a public DNS record pointing at a private address, produces
the dropped-packet case on every laptop off the VPN.

## Decision

**1. Every Cardo hook is `type: "http"`, `url` and `timeout: 1`, and nothing else.**
- No `async`: it does nothing on an HTTP hook and reads like a promise.
- No `headers`, which would interpolate the engineer's environment into a request.
- No matcher.

A healthy collector answers in milliseconds. One that is unreachable or hung now costs each hooked
event at most one second, instead of 21 s, 30 s or 600 s.

**2. Model switches are read from `PostModelSwitch`, not `PreModelSwitch`.**
- It carries the same fields and cannot block.
- [ADR-0027](0027-model-switch-cost.md)'s table stands unchanged: each field is kept only when its
  value has the type its name means, and `pricing` is not kept.
- `model_switch_source` comes from `source` on either event. The collector still accepts
  `PreModelSwitch` from bundles already deployed.
- `PostModelSwitch` also fires when Claude Code changes the model itself (`auto`) and when a resumed
  session restores its model (`resume`). The views count only the requested switches (`command`,
  `picker`, `sdk`), which is what `PreModelSwitch` reported.

**3. `SessionStart` leaves the bundle.** Claude Code does not run HTTP hooks on it. A session's
start comes from OTel, as it already does.

**4. The published event set is therefore twelve events:** `SessionEnd`, `UserPromptSubmit`,
`UserPromptExpansion`, `PermissionRequest`, `PermissionDenied`, `PreCompact`, `PostCompact`,
`InstructionsLoaded`, `SubagentStart`, `SubagentStop`, `PostModelSwitch`, `ConfigChange`.

This replaces the list [ADR-0005](0005-collection-mechanism.md) published. ADR-0005's decision
stands, and so do the invariants it is the source of: native OTel plus a managed hook pack, and
never a transcript. Only its enumeration is replaced here, which is what INV-4 asks an ADR to do.

**5. The collector's name should resolve only inside the organization's network.**
- Off the network, a lookup that fails returns in about 0.2 s.
- A public name pointing at a private address leaves the laptop waiting until the timeout instead.
- A public record remains possible where internal DNS is not, and decision 1 bounds its cost.

**6. ADR-0024's other decisions stand unchanged.**
- The bundle holds `env` and `hooks` and nothing else.
- It sets no `allowedHttpHookUrls`, `allowManagedHooksOnly`, permission rules, MCP policy or version
  pins.
- Policy stays the organization's decision.
- Cohorts come from `OTEL_RESOURCE_ATTRIBUTES`.

`test/bundle_test.go` enforces the shape.

## Consequences

- **Each hooked event now waits for the collector, which the old record denied.**
  - Healthy, that is milliseconds.
  - Unreachable, it is up to a second per event, and Claude Code shows a hook error or a timeout
    notice under the message.
  - The engineer-facing documents say so, instead of "never waits".
- **A hook that times out loses its event.** A collector slower than a second, or a VPN round trip
  close to it, costs data rather than the engineer's time. The one-laptop check measures the round
  trip. If healthy hooks come near the limit, the limit is revisited with that measurement.
- **`SessionEnd`'s hooks share a 1.5 s budget**, which a 1 s timeout stays inside.
- **Bundles deployed before this keep working.** The collector still accepts `PreModelSwitch`, and
  its rows still feed the views.
- **The model-switch view is redefined in a new migration**, since shipped migrations are
  immutable. It matches Claude Code's own timing event by the model's canonical name, because
  `PostModelSwitch`'s `to_model` is a dated ID and the hook's name uses the canonical one.

## Rejected alternatives and why

- **Keep the default timeouts.** A laptop off the VPN waits 21 s per prompt on Windows, and longer
  elsewhere up to the 30 s cap. A hung collector costs 30 s per prompt, and up to 600 s on other
  events.
- **A timeout on `PreModelSwitch`.** A cancelled `PreModelSwitch` hook blocks the switch. With 1 s,
  a laptop off the VPN could not switch models at all.
- **`PreModelSwitch` with no timeout.** A hung collector still blocks switches after 30 s. So does
  an unreachable one, wherever the operating system waits longer than 30 s for a connection.
- **Command hooks with `async: true` running `curl`.** This is the only true fire-and-forget, but:
  - it starts a process on the laptop for every event;
  - it needs a command per operating system, and one that never prints, since an async hook's
    output reaches the conversation;
  - Windows hook commands are fragile. A third-party `cmd.exe /d /c` hook on the measuring machine
    started an interactive shell instead, and put its banner and the hook payload into every
    prompt;
  - it makes Cardo something that runs on the laptop, which ADR-0011 refused.
- **0.5 s.** Measured to work (501 ms), and it halves the worst case. But it leaves less room for a
  VPN round trip that has not been measured, so 1 s holds until it has.
- **OTel only, no hooks.** OTel exports in batches and never blocks. But it carries none of the
  permission, compaction, instructions or subagent facts ADR-0005 added the hook pack for.
- **A local relay that accepts hooks on the laptop and forwards them later.** Fire-and-forget
  without a process per event, but it is an installed daemon, which ADR-0011 refused.
