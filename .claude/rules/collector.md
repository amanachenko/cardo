---
paths:
  - "deploy/collector/**"
  - "deploy/compose/.env.example"
  - "test/collector_test.go"
  - "test/fixtures/hooks/**"
---

# The collector and the hook allowlist

The ingest path is pure OTel Collector configuration
([ADR-0012](../../docs/adr/0012-ingest-implementation.md)). Hook payloads are an allowlist pinned in
`test/collector_test.go`; adding to it is adding to the mandatory tier (INV-4). After touching
`deploy/collector/`, run `make collector-test` with the collector's own `CARDO_SALT` exported.

Artifact names: `CARDO_ORG_ARTIFACTS` labels what the organization shipped;
`CARDO_ARTIFACT_NAMES=org-only` is the strict mode, and anything but `all` is strict.
Instructions-file names are kept only when they are the organization's
([ADR-0026](../../docs/adr/0026-stale-instructions-by-versioned-name.md)).

## Things that look like good ideas and are not

- Putting a generic hook field (`reason`, `trigger`, `source`, `prompt`, `message`, `error`) on the
  allowlist because the event you are looking at uses it harmlessly. The list is a union across
  events. Map it onto a specific name for that one event, guarded to an enum's shape, as
  `SessionEnd`'s `reason` becomes `session_end_reason`. `TestCollectorScopesGenericHookFields`
  checks the scoping.
- Trusting documentation, and above all a *summary* of documentation, for a hook field name. The
  2026-09-23 note was built from WebFetch summaries and was wrong about four events plus
  `content_hash` and `tool_use_id`. `cardo.received_keys` from a real session is the authority.
- Capturing raw hook payloads to see their real shape. `cardo.received_keys` already records every
  field name that arrived; nothing needs the values of the fields that were dropped.
- Leaving `webhook_event`'s `max_request_body_size` at its 100 KiB default. Larger bodies are
  refused with a 400 and no collector log line; the only trace is a hook error count in Claude
  Code's own OTel. Real `SubagentStop` payloads were lost this way.
- Leaving `webhook_event`'s `read_timeout` and `write_timeout` at their 500 ms defaults. A Claude
  Code busy running other hooks can be slower than that; the receiver then closes the connection
  with no response, Claude Code shows "socket hang up" under the prompt, and late headers lose the
  event. The receiver caps both at 10 s, and ignores a misspelt key without a word.
- `error_mode: ignore` on any collector processor. A statement that errors is skipped and the record
  exported anyway — for the identity transform, that is an unhashed email.
- Treating a blank regex as "no pattern" in OTTL. An empty regex matches everything; a blank
  `CARDO_ORG_ARTIFACTS` kept every private name until each statement special-cased `""`.
- A `filter` processor for the INV-2 tripwire. It drops the record and answers 200, so a transform
  that misses a field loses data and nobody hears of it. The tripwire is a transform that refuses
  the request, as `refuse_weak_salt` does, and logs a REFUSED line.
- Relying on the collector's `${env:...}` to fail on a missing variable. It expands to empty with a
  startup warning, which is why a missing salt is caught by a statement that refuses the batch.
- Keeping an external repository's URL, or a salted hash of it, in tier 1. The collector keeps the
  organization's repositories by name and reduces everything else to `external` plus the host.
  A blank `CARDO_ORG_REPOS` means everything is external, never everything is the organization's
  (ADR-0035).
- A backslash or a double quote in `CARDO_ORG_REPOS` or `CARDO_ORG_ARTIFACTS`. The collector pastes
  the value into a quoted OTTL string, and either one stops it from starting. Write a dot as `[.]`,
  as in `^github[.]com/acme/`. ADR-0035's own example, `^github\.com/acme/`, is the form that fails.
- Upgrading the collector image as a tag bump. OTTL syntax, component names and the exporter's
  columns move between releases; see `deploy/collector/README.md`.
