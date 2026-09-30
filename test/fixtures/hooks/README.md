# Hook payload fixtures

Replayed through the real collector by `test/collector_test.go`, in CI and via `make
collector-test`, to check that what reaches ClickHouse is what the ADRs say it is
([ADR-0023](../../../docs/adr/0023-hook-payload-allowlist.md),
[ADR-0025](../../../docs/adr/0025-artifact-names-kept-with-guardrails.md) to
[ADR-0027](../../../docs/adr/0027-model-switch-cost.md)). Both directories are replayed on every
run.

Every content-bearing value in a fixture contains a marker: `CONTENT-MARKER`, a path under
`marker-user`, or an address at `example-corp.com`. The test fails if any marker reaches storage.
That makes INV-5 a byte search over what was actually written, rather than an inspection of what
was configured. Artifact names carry no marker, because by default they are stored
(ADR-0025). The strict mode is checked by asserting the stored name instead.

## `2.1.281/`: observed field names, synthetic values

The **field names** are those Claude Code 2.1.281 sent on 2026-09-24, as recorded in
`cardo.received_keys` during two real sessions
([first](../../../docs/research/2026-09-24-first-real-hook-payloads.md),
[second](../../../docs/research/2026-09-24-second-real-session.md)). The **values** are
invented. The collector never stored a dropped field's value, so no raw payload was captured, and
none is here.

- **Types confirmed:** the `PreModelSwitch` cost fields, by a stored row in the second session. Its
  enum values (`source` `command`, `cache_ttl` `1h`) are used here too.
- **Types still guessed**, because the field was never kept: `pricing`, `globs`,
  `background_tasks` and `permission_suggestions`.

`SessionStart` and `PermissionDenied` are missing: the first never ran, and the second was not
provoked. `documented/` covers them.

| Fixture | Covers |
|---|---|
| one per observed event, 11 | The observed shape, carrying content wherever the event carries it |
| `InstructionsLoaded.user` / `.nested` / `.org-rule` / `.managed-windows` | The derivations from `file_path`, including an organization's versioned rule name, which is kept only when it matches `CARDO_ORG_ARTIFACTS` |
| `InstructionsLoaded.include` | A versioned file that a `CLAUDE.md` imports with `@`: `load_reason=include`, and the importer's path in `parent_file_path`, which is not stored |
| `UserPromptExpansion.personal` | A command an engineer defined, kept by name unless the strict mode is on |
| `PermissionRequest.subagent` / `.mcp` | A request from inside a subagent; an MCP tool name |
| `SessionEnd.prose-reason` | Prose under a field that should be an enum: not stored |
| `PreModelSwitch.wrong-types` | Text under numeric and boolean names: not stored |

The test adds one payload of its own: a `SubagentStop` past the receiver's default 100 KiB limit.

## `documented/`: from the documentation of 2026-09-23

Transcribed from the hooks documentation as fetched on 2026-09-23. **Several names in them are not
what Claude Code sends** (`user_input`, `session_end_reason`, `compaction_reason`, `config_source`,
`content_hash`, `tool_use_id`). They stay because the collector still accepts those names, so an
older or newer client using them keeps working, and because they are the only shapes for the two
events not yet observed. Two variants use made-up names: `SubagentStop.private-agent` and
`PermissionRequest.mcp`.

## Recording the next version

Run Claude Code against the reference stack with
`deploy/managed-settings/local-evaluation.json`, then compare `cardo.received_keys` with these
files. Where they differ, write a new directory named for that Claude Code version, and leave the
existing ones as the record. That is the fixture corpus
[ADR-0014](../../../docs/adr/0014-version-drift.md) asks for, built without anyone's prompt text
ever having been captured.
