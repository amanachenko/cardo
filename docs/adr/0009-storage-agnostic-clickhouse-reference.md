# ADR-0009 — Storage-agnostic semantic layer; ClickHouse + Grafana reference stack

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-21-prior-art-survey.md`; `2026-09-20-claude-code-telemetry-surfaces.md`
(cardinality defaults).

## Context

A stated requirement is that admins can run their own analysis freely. That phrase rules out most
options on its own, because it means SQL.

Two structural facts:

- **The valuable semantic payload is in OTel logs/events, not metrics.** Metrics carry aggregates;
  `tool_result`, `tool_decision`, `user_prompt` and the hook events carry the story.
- `OTEL_METRICS_INCLUDE_SESSION_ID` defaults to `true`, so even the metrics are session-cardinality.

Prior art also shows the plumbing is already built: `ColeMurray/claude-code-otel` (502 stars, MIT) is a
complete collector + Prometheus + Grafana stack, and Grafana publishes Claude Code dashboards 25052 and
25255.

## Decision

The project **is** the semantic layer — hook pack, collector config, pseudonymization processor, SQL
transforms and dashboard definitions — installable onto whatever backend the organization already
runs (Grafana, Datadog, ClickHouse).

Reference stack for greenfield users: **ClickHouse + Grafana**, shipped as a docker-compose deployment
that borrows from `claude-code-otel` and the published Grafana dashboards rather than rebuilding them.

Plus a **DuckDB/Parquet evaluation mode** for a single team or a first look.

## Consequences

- An organization already running Datadog or Grafana can adopt Cardo without standing up a second
  storage stack. That is far easier for an existing platform team to accept.
- The project's value is unambiguous: nobody can dismiss it as "just a Grafana dashboard," because the
  dashboards are the least of it.
- DuckDB mode means a five-engineer pilot needs no infrastructure approval at all — which is how a
  first pilot actually starts.
- Two storage targets to keep working in CI.

## Rejected alternatives and why

- **Prometheus + Loki + Grafana** (the easy fork of `claude-code-otel`). Prometheus is actively hostile
  to session-level cardinality and LogQL is a poor language for joining sessions to skill invocations to
  outcomes. Inheriting it would silently cap the product at metric dashboards and make exactly the
  interesting questions unanswerable.
- **Postgres / TimescaleDB.** Familiar to everyone, fine at small scale, will hurt at fleet scale.
- **DuckDB + Parquet only.** Near-zero ops, excellent for one team; does not serve a concurrent
  multi-user deployment.
- **Fork `claude-code-otel` as the product base.** Fastest to a working demo, but ties the product to
  one backend and makes it look like a dashboard pack.
- **Contribute upstream to New Relic's Preflight** (Apache-2.0, hooks-based, closest architecture). Its
  model is local session-log reading, which INV-1 forbids — we would be fighting its design.
