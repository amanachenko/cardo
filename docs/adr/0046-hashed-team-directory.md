# ADR-0046 — The team directory is loaded as a hashed mapping, never as addresses

**Status:** Accepted
**Date:** 2026-10-18
**Depends on:** ADR-0006 (pseudonyms), ADR-0029 (minimum group size), ADR-0034 (identity in pilots).

## Context

Team views need to know which team a session belongs to. The organization already has that mapping in
its identity provider or HR directory, keyed by email. Cardo's promise is that it does not hold a
directory of people: a stored email is a person, and a table of people is exactly what this product
refuses to become.

At the same time the mapping has to be joinable. Sessions carry `pseudonym`, a salted hash produced by
the collectors; the directory must produce the same value from the same salt, or nothing joins.

## Decision

**The file is the operator's; the database gets a hashed mapping.**

- Input: CSV with a header `email,team` (header optional, case-insensitive), one person per line.
  Blank lines and `#` comments are ignored. A line without `@` or without a team is reported as
  malformed with its line number; it is not silently skipped.
- The loader hashes each address with the stack's salt (`CARDO_SALT`) and version (`CARDO_SALT_VERSION`,
  default `v1`) using the same `pseudonym.Hasher` the collector uses. Addresses are lowercased and
  trimmed first, because the collector does the same.
- Storage: `settings_teams(team, pseudonym, batch)` plus `settings_teams_batches(batch, source_hash,
  rows, teams, loaded_at)`. A load is a new batch; the current batch is the newest `loaded_at`. The
  previous batch remains for one generation so a mistaken upload can be reverted by re-loading.
- The uploaded text is **not** stored, and neither is its hash of addresses beyond the mapping itself:
  `source_hash` is the SHA-256 of the file, which proves what was loaded without containing it.
- The preview shows counts only — rows, unique addresses, duplicates, malformed lines, teams, and how
  many teams fall below the floor. It never lists an address, not even masked.
- Coverage answers three numbers, not a list: sessions in seven days, sessions whose pseudonym is in
  the current batch (percentage), and distinct pseudonyms with no team.
- A team with fewer than `min_group_size` people is **not rendered at all**; the screen reports how
  many such teams were hidden. Showing "1 person" would defeat the floor.

## Consequences

- Changing the salt orphans the directory: every pseudonym changes, coverage drops to zero, and the
  operator must reload the file. This is the same trap the collector has, and it is documented in the
  README rather than hidden.
- Rotating the directory is one upload; no cron, no sync job, no service account.
- Team names are organization structure, not personal data, and they are visible. Counts are not,
  below the floor.
- The audit line records the batch, row count and source hash — enough to answer "what was loaded and
  when", not enough to reconstruct an address.

## Rejected alternatives

- **Store emails and hash at query time.** Would make every view a privacy hazard and every backup a
  directory leak.
- **Read the directory live from an IdP or SCIM.** A second credential and a live dependency for a
  mapping that changes weekly; the pilot does not need it.
- **Infer teams from email domains.** Wrong for any organization with shared domains and invisible to
  the operator when it is wrong.
