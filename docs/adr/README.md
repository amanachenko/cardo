# Architecture Decision Records

How these work: **[ADR-0000 — How we record decisions](0000-adr-process.md)**. Read it before writing
one. The short version: ADRs are immutable, "Rejected alternatives and why" is mandatory, and every ADR
names its evidence source.

The hard rules that these decisions produce live in
**[`../design/invariants.md`](../design/invariants.md)**. Violating one requires a superseding ADR.

| # | Title | Status | Date |
|---|---|---|---|
| [0000](0000-adr-process.md) | How we record decisions | Accepted | 2026-09-22 |
| [0001](0001-unit-of-analysis.md) | The unit of analysis is the enablement artifact, not the engineer | Accepted | 2026-09-22 |
| [0002](0002-three-tier-data-model.md) | Three-tier data model with structural boundaries | Accepted | 2026-09-22 |
| [0003](0003-efficiency-before-security.md) | Efficiency first; security is a separate, later, enforcement-first module | Accepted | 2026-09-22 |
| [0004](0004-claude-code-first-neutral-schema.md) | Claude Code first, with a vendor-neutral canonical layer | Accepted | 2026-09-22 |
| [0005](0005-collection-mechanism.md) | Collect via native OTel plus a managed hook pack; never read transcripts | Accepted | 2026-09-22 |
| [0006](0006-pseudonymization.md) | Pseudonymize at the org-edge collector; stable salt in v0.1 | Accepted | 2026-09-22 |
| [0007](0007-enrollment-posture.md) | Thin mandatory core plus opt-in depth | Accepted | 2026-09-22 |
| [0008](0008-outcome-variable.md) | The outcome variable is a friction index | Superseded by 0031 | 2026-09-22 |
| [0009](0009-storage-agnostic-clickhouse-reference.md) | Storage-agnostic semantic layer; ClickHouse + Grafana reference stack | Accepted | 2026-09-22 |
| [0010](0010-layered-schema.md) | Layered schema: raw verbatim, thin canonical views, marts | Superseded by 0023 | 2026-09-22 |
| [0011](0011-hook-transport-http.md) | Hook transport is native async HTTP; zero local binary | Superseded by 0024 | 2026-09-22 |
| [0012](0012-ingest-implementation.md) | Hook path stays pure collector config; one Go binary for the Admin API | Accepted | 2026-09-22 |
| [0013](0013-deployment-topology.md) | Strictly single-tenant, self-hosted, no phone-home | Accepted | 2026-09-22 |
| [0014](0014-version-drift.md) | Minimum version only, tolerant parsing, CI fixture corpus | Accepted | 2026-09-22 |
| [0015](0015-license-and-governance.md) | Apache-2.0, no CLA, private until an organization runs it | Superseded by 0038 | 2026-09-22 |
| [0016](0016-retention.md) | Retention is the primary privacy control | Accepted | 2026-09-22 |
| [0017](0017-v01-scope.md) | v0.1 is the Admin API wedge, then a thin slice | Accepted | 2026-09-22 |
| [0018](0018-provider-scope.md) | v1 targets Claude API direct with an Enterprise licence | Superseded by 0021 | 2026-09-22 |
| [0019](0019-sql-tooling.md) | Plain SQL files with a light migration runner, not dbt | Accepted | 2026-09-22 |
| [0020](0020-name.md) | The project is called Cardo | Accepted | 2026-09-22 |
| [0021](0021-analytics-source-scope.md) | Support both analytics sources; build the Console adapter first | Accepted | 2026-09-23 |
| [0022](0022-clickhouse-access.md) | ClickHouse over the HTTP interface, standard library only | Accepted | 2026-09-23 |
| [0023](0023-hook-payload-allowlist.md) | Hook payloads are reduced to an allowlist at the collector | Superseded by 0025 | 2026-09-23 |
| [0024](0024-bundle-configures-telemetry-only.md) | The managed-settings bundle configures telemetry and nothing else | Accepted | 2026-09-23 |
| [0025](0025-artifact-names-kept-with-guardrails.md) | Artifact names are kept; the views decide who sees them | Accepted | 2026-09-24 |
| [0026](0026-stale-instructions-by-versioned-name.md) | Stale instructions are detected by versioned file names, not content hashes | Accepted | 2026-09-24 |
| [0027](0027-model-switch-cost.md) | The cost of a model switch joins the mandatory tier | Accepted | 2026-09-24 |
| [0028](0028-file-store-is-admin-api-only.md) | The file store serves the Admin API path only | Accepted | 2026-09-25 |
| [0029](0029-minimum-group-size.md) | Views name a group only once five people are in it | Accepted | 2026-09-25 |
| [0030](0030-stakeholders-and-questions.md) | Who Cardo serves, and the questions it will and will not answer | Accepted | 2026-09-25 |
| [0031](0031-what-works-means.md) | What "works" means: spread, then efficiency, then delivery | Accepted | 2026-09-25 |
| [0032](0032-engineer-coach-by-shared-link.md) | The engineer's view is a coach, and in pilots it is a shared link | Accepted | 2026-09-25 |
| [0033](0033-per-person-cost-for-budget-owners.md) | A budget owner may see cost per person, and nothing more | Accepted, not built | 2026-09-25 |
| [0034](0034-identity-in-pilots.md) | In the pilots, pseudonyms stay, the operator holds the salt, and teams come from a directory | Accepted | 2026-09-25 |
| [0035](0035-repository-identity-classified.md) | Repository identity is collected, classified at the collector | Accepted, not built | 2026-09-25 |
| [0036](0036-service-runs-are-not-people.md) | Service runs are labelled, and are never counted as people | Accepted, not built | 2026-09-25 |
| [0037](0037-policy-evidence.md) | Policy evidence: conformance, coverage and other organizations | Accepted, not built | 2026-09-25 |
| [0038](0038-public-before-the-adoption-pilot.md) | Publish before the adoption pilot, under a collective copyright | Accepted | 2026-09-30 |

ADR-0030 to ADR-0037 came out of one design review on 2026-09-25, which asked who Cardo is for and
what each of them needs. Its evidence is
[`2026-09-25-stakeholder-field-notes.md`](../research/2026-09-25-stakeholder-field-notes.md), and
the order they are built in is [`docs/roadmap.md`](../roadmap.md).

## Deliberately deferred

Recorded here so a future session knows these were considered and postponed, not overlooked:

- **An authenticated self-view, or coach delivery by message** — only if pilot volunteers act on
  the coach's suggestions. In the pilots, each volunteer's page is a shared link (ADR-0032).
- **The named cost view for budget owners** — decided, offered to an organization that asks, not
  part of any pilot (ADR-0033).
- **Handing the salt to the organization's security team** — the operator holds it during pilots;
  custody is offered afterwards (ADR-0034).
- **Named security exceptions, and the SIEM feed** — after the evidence report (ADR-0037).
- **Behavioural security detection** — needs content or a baseline (ADR-0003, ADR-0037).
- **Delivery outcomes from the git host** — joined by repository and week; no surface carries a
  commit id, so the commit-SHA join ADR-0008 planned cannot be built (ADR-0031).
- **Views for autonomous agent runs** — the label comes first; views when a pilot runs agents
  (ADR-0036).
- **Full adapters for other agents** — as organizations need them, for comparing tools against each
  other (ADR-0004, ADR-0030).
- **Per-tier salt rotation** — stable salt in v0.1; rotation is the better long-term design (ADR-0006).
- **Local-spool hook fallback** — build when an organization asks (ADR-0011, upheld by ADR-0024).
- **Content hashes of instructions files** — Claude Code 2.1.281 sends none. The field stays on the
  allowlist, so the hash-based design returns without a decision if it appears (ADR-0026).
- **Cross-org benchmark feed** — only ever as a separate opt-in product with its own consent story
  (ADR-0013).
- **Bedrock / Vertex / Foundry support** — out of scope for v1 (ADR-0018, upheld by ADR-0021).
- **A file-store mode for the collector path** — until a pilot team can run a collector but not
  ClickHouse (ADR-0028).
- **Materialized collector-path views and rollups** — the views are plain views over 90-day bronze
  (risks.md #10 and #14).
- **The Claude Enterprise Analytics adapter** — in scope for v1, built second; the Console adapter
  goes first purely because it is testable without an adopting organization (ADR-0021).
