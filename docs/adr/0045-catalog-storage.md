# ADR-0045 — The enablement catalog is versioned files plus metadata rows

**Status:** Accepted
**Date:** 2026-10-18
**Depends on:** ADR-0024 (managed settings bundle), ADR-0040 (spread is a vote only for what teams
chose), ADR-0043 (the board is the operator's surface).

## Context

The console needs a place for the artifacts an organization rolls out and measures: skills, slash
commands, subagents, MCP servers, reference documents, settings bundles and pilot cohorts. Three
shapes were possible:

1. Store the file body in ClickHouse as a String column.
2. Store only a path to a directory the operator manages by hand.
3. Store the file on disk in a versioned layout, and keep metadata and hashes in ClickHouse.

The forces are the ones this project always has: the binary stays dependency-free, the store stays
auditable, and a change must be explainable after the fact. A body in ClickHouse would put markdown
and JSON into the same table the views scan, make every dashboard query heavier, and turn a file
restore into a database restore. A bare path would lose the hash, the version, and any evidence that
what the views label "org artifact" is what was actually rolled out.

## Decision

**Files live on disk; metadata lives in ClickHouse.**

- Layout: `<compose-dir>/catalog/<kind>/<slug>/<version>/content<ext>`. Versions are immutable; a new
  upload is a new directory. Rollback creates no file: it re-points the approved version.
- `cardo.settings_artifact_versions` holds one row per version: `slug, version, kind, name, owner,
  status, content_hash, path, note, created_at, approved_at`. Current version = `argMax(version)`.
- `cardo.settings_changes` is the audit line for every write: action, object, version, before/after
  hash, note. It is what the console shows under "Recent changes" and what an auditor reads.
- Status is `draft | approved | retired`. **Only an approved version is what the views may call an
  organization artifact**; drafts exist for the operator and change nothing downstream.
- Validation happens before anything is written: kind allow-list, slug `^[a-z0-9][a-z0-9-]{1,63}$`,
  256 KiB ceiling, per-kind structure (frontmatter, JSON shape, reference headings), and a secret
  scan that refuses obvious keys. A rejected upload writes no file and no row.
- The diff shown before saving is textual and per line, computed against the currently approved
  version of the same slug. It is stored only as a `note`, never as content.

## Consequences

- Backup is two things, not one: the ClickHouse volume and `catalog/`. The installer already owns the
  compose directory, so both sit under the same path; the docs say so.
- The catalog survives a ClickHouse wipe as files, but loses status and history. That is the accepted
  trade: content is the expensive part, metadata is reconstructible.
- Reach is read from `gold_artifact_usage` by `(kind, artifact)` for the latest week; below the
  minimum group size the column shows a dash, not a number (ADR-0029).
- No artifact body is ever served as HTML. Markdown is shown as text, JSON is shown as text.

## Rejected alternatives

- **Bodies in ClickHouse.** Heavier views, mixed concerns, and a restore that needs the database.
- **A git repository as the catalog.** Attractive for diffs, but it adds a second version-control
  system, credentials and a sync story to a product whose promise is that nothing needs maintaining.
- **Immediate publish without a draft step.** One mis-drop would become the organization's standard
  with no undo and no record of who approved it.
