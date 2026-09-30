# Console adapter fixtures

Recorded payloads for the Claude Code Analytics API, replayed in tests through an `httptest`
server so the adapter is exercised end to end without a network or a key.

**These are constructed from documentation, not captured from a live response.** Section 6 of
[the 2026-09-23 snapshot](../../../docs/research/2026-09-23-admin-analytics-apis.md) explains why
that matters: the parser and the fixtures share a single source, so green tests here demonstrate
self-consistency, not that the field names are right.

The first live call against a real Admin API key should be captured — with `email_address` values
replaced by fictional ones — and added here as `live-<date>.json`, at which point these become
regression fixtures rather than the only evidence. That is what ADR-0014 means by the fixture
corpus being the authority.

| Fixture | What it covers |
|---|---|
| `single-page.json` | The documented response, one actor, one model |
| `page-1.json` / `page-2.json` | Cursor pagination across two pages |
| `api-actor.json` | `api_actor` identity — an API key name rather than an email |
| `drift.json` | Unmodelled fields, which must be reported and stored, never dropped |
| `empty.json` | A successful, authenticated, zero-row day — see risks.md #9 |
