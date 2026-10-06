---
paths:
  - "deploy/managed-settings/**"
  - "test/bundle_test.go"
---

# The managed-settings bundle

The bundle is `env` and `hooks`, nothing else, and each hook is `type`, `url` and `timeout: 1`
([ADR-0039](../../docs/adr/0039-hooks-wait-at-most-one-second.md), carrying ADR-0024 forward).
Installing Cardo must not change how Claude Code behaves for an engineer beyond sending telemetry.
`test/bundle_test.go` enforces the shape.

## Things that look like good ideas and are not

- `async: true` on an HTTP hook. Claude Code accepts it and ignores it: `async` exists only on
  command hooks, and it waits for every HTTP hook. Without a `timeout`, a laptop off the VPN waited
  21 s per prompt and a hung collector 30 s, and other events allow 600 s. Each Cardo hook sets
  `timeout: 1` (ADR-0039).
- A hook on `PreModelSwitch`. One cancelled at its timeout blocks the engineer's model switch.
  `PostModelSwitch` carries the same fields and cannot block.
- A `SessionStart` HTTP hook. Claude Code does not run HTTP hooks on it, and says so; a session's
  start comes from OTel.
- `allowManagedHooksOnly`, `allowedHttpHookUrls`, or any other policy in the bundle. Each silently
  changes Claude Code for every engineer, and `allowedHttpHookUrls` in particular breaks every
  existing HTTP hook not on the list (ADR-0024).
