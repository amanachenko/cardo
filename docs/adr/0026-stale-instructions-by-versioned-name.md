# ADR-0026 — Stale instructions are detected by versioned file names, not content hashes

**Status:** Accepted
**Date:** 2026-09-24
**Evidence:** `2026-09-24-first-real-hook-payloads.md`, section 2: Claude Code 2.1.281 sends no
`content_hash` on `InstructionsLoaded`, and the hooks documentation re-fetched that day does not
list one.

## Context

[ADR-0023](0023-hook-payload-allowlist.md) called `InstructionsLoaded` "the artifact-analytics
keystone", and identified an instructions file by its `content_hash`. The designed `artifact_usage`
mart answered "who is stale?" by comparing hashes against the ones the platform team ships. The
headline end-to-end check in the plan was a dashboard that notices one machine loading an old
`CLAUDE.md`.

The real payload has no hash. It has `file_path`, which is content (INV-5), and which the collector
already reduces to a kind (`CLAUDE.md`, `rule`, …) and a scope (`managed`). Nothing that is kept
distinguishes version 6 of a file from version 7.

## Decision

**1. The collector keeps an instructions file's own name**, with no directory, as
`instructions_name`. It is kept **only when it matches `CARDO_ORG_ARTIFACTS`**. Every other file
name is dropped, in both naming modes ([ADR-0025](0025-artifact-names-kept-with-guardrails.md)).

**2. Staleness becomes a naming convention the organization adopts.** It puts a version in the name
of what it ships:

- a rule such as `.claude/rules/acme-security-v3.md`, or
- a thin managed `CLAUDE.md` that imports `@acme-standards-v7.md`.

A machine still loading `-v6` is stale. The deployment README documents the convention.

**3. `content_hash` stays on the allowlist.** If Claude Code starts sending it, it is kept with no
further decision, and the hash-based design becomes available again.

## Consequences

- Stale detection works only for an organization that versions the names of what it ships. That is
  a real burden, but a small one: a filename convention in the repository that distributes the
  files.
- The managed `CLAUDE.md` is always named `CLAUDE.md`, so its own version can be seen only through
  what it imports.
- **The import pattern is a hypothesis until observed.** The documentation lists
  `load_reason=include`, but no import was exercised in the first real session. A versioned rule
  file loading on `path_glob_match`, with its own path in `file_path`, was observed.
- One field is added to the mandatory tier (INV-4).

## Rejected alternatives and why

- **Wait for Claude Code to send a hash.** There is no date for it, and the dashboard's headline
  question would stay unanswerable until then.
- **A command hook that hashes the file on the laptop.** That means a script on every machine,
  written per operating system (`sha256sum`, `shasum`, `Get-FileHash`). It ends "the bundle is
  settings and HTTP hooks, and nothing runs locally" ([ADR-0024](0024-bundle-configures-telemetry-only.md)),
  and it would fail differently on each machine it failed on.
- **Keep every instructions file's name.** A file name is part of a path. Rule and import names can
  name a project, a customer or an acquisition, which is exactly the content INV-5 keeps out.
- **A salted hash of the path.** Rejected in ADR-0023 for reasons that still hold: it is a
  per-path identifier, open to a dictionary attack by anyone holding the salt, and it says nothing
  about version.
- **Drop stale detection.** The dashboard would still answer "is it loading, where, and for what
  share of the fleet", but not "who is stale", which the operator wants.
