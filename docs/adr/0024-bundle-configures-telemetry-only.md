# ADR-0024 — The managed-settings bundle configures telemetry and nothing else

**Status:** Accepted
**Date:** 2026-09-23
**Evidence:** `2026-09-23-hooks-otel-collector-surfaces.md` (current settings documentation).
**Supersedes:** [ADR-0011](0011-hook-transport-http.md).

## Context

[ADR-0011](0011-hook-transport-http.md) chose native async HTTP hooks with nothing installed on
developer machines. That stands. Its decision also said: *"`allowedHttpHookUrls` pinned in managed
settings."* The architecture diagram listed `allowManagedHooksOnly` alongside it, and
[ADR-0007](0007-enrollment-posture.md)'s context mentions it as the way to stop the pack being
disabled.

The current settings documentation changes what those two keys would do:

- **`allowedHttpHookUrls`**, when defined at any settings level, restricts HTTP hooks *from every
  source* to the merged list. An organization that has never defined it has no restriction. The
  moment Cardo's bundle defines it, every HTTP hook an engineer already had silently stops firing
  unless its URL happens to be on the list.
- **`allowManagedHooksOnly`** blocks every user, project and plugin hook, disables command-sourced
  plugins, and narrows `statusLine` and friends to managed settings only.

And neither is needed to keep the pack reliable. `disableAllHooks` cannot disable managed hooks
unless it is itself set at the managed level, so an engineer cannot switch Cardo's hooks off from
their own settings either way.

## Decision

**1. ADR-0011's transport decision stands unchanged.** The hook pack is native `type: "http"`
hooks with `async: true`, posting to the org-edge collector. No binary, daemon or script runs on
developer machines. A local spool stays deferred.

**2. The bundle holds two top-level keys and no others:** `env` for telemetry variables, and
`hooks` for the thirteen events. Each handler is `type`, `url` and `async`, and nothing else: no
`headers`, which would interpolate the engineer's environment into a request, and no `timeout` or
matcher. `allowedHttpHookUrls`, `allowManagedHooksOnly`, permission rules, MCP policy and version
pins are all absent. `test/bundle_test.go` enforces the whole shape.

**3. Policy stays the organization's decision.** An organization that already restricts HTTP hooks
adds Cardo's URL to its own `allowedHttpHookUrls`. One that wants any of the other controls sets
them in its own managed settings, where they are its call and not a side effect of installing
Cardo. The deployment README says so.

**4. Cohorts come from `OTEL_RESOURCE_ATTRIBUTES`.** The bundle sets `cardo.cohort=unassigned`, and
the platform team ships one bundle per MDM group with that value changed. It is attached to every
OTel record, and gold views aggregate by it, with a minimum cohort size (INV-3).

## Consequences

- Installing Cardo changes nothing about how Claude Code behaves for an engineer, except that it
  now sends telemetry. That is the property ADR-0011 was reaching for with "the entire client side
  is a readable config file", and it is now a tested one.
- One side effect remains and cannot be designed away. When `OTEL_EXPORTER_OTLP_*` is set in managed
  settings, Claude Code removes the developer's own per-signal endpoints. An engineer already
  exporting OTel to a personal tool loses that. The README states it.
- Cardo does not prevent hooks being aimed at other hosts. That is an exfiltration control, it
  belongs to the organization's security policy, and when Cardo's security module exists
  ([ADR-0003](0003-efficiency-before-security.md)) it will report on it rather than impose it.

## Rejected alternatives and why

- **Pin `allowedHttpHookUrls` to the collector, as ADR-0011 said.** It silently breaks every
  existing HTTP hook in the organization that is not on the list. The first ticket filed about Cardo
  would be "installing it broke my hooks".
- **`allowManagedHooksOnly: true`.** It disables every personal hook and most plugins. It is also
  unnecessary, because managed hooks cannot be disabled from user settings anyway.
- **A shared token header** checked by the collector's `required_header`. The bundle is readable by
  every engineer on every machine, so the token is not a secret, only a speed bump. Restricting the
  collector at the network is what actually works, and the deployment README says so.
- **Synchronous hooks.** They would guarantee delivery ordering. But they put the collector on the
  critical path of every prompt, and on `PreModelSwitch` a timed-out hook blocks the switch outright.
