# Prior art — coding-agent telemetry and observability, snapshot 2026-09-21

> **This is a dated snapshot and is never edited.** This market is moving quickly. If this is stale,
> write a new dated note. See [ADR-0000](../adr/0000-adr-process.md) rule 5.
>
> **Method:** targeted web search and page fetches, 2026-09-21. Availability and name-collision checks
> for [ADR-0020](../adr/0020-name.md) were performed separately on 2026-09-22.

## Headline conclusions

1. **The plumbing is already built and MIT-licensed.** Building a collector, storage and dashboard stack
   from scratch would be waste.
2. **Real competition exists**, with distribution. Datadog in particular.
3. **The white space is narrow and specific**, and it maps onto what Cardo chose to build.

---

## Landscape

| Name | Category | OSS? | Ingests | Claude Code | Security angle |
|---|---|---|---|---|---|
| **Datadog Agent Console** | Observability module | No (Preview) | Claude Code native OTLP + Anthropic usage/cost API; also Cursor etc. | Flagship integration — cost, sessions, "detected problems" (retry loops, skipped checks) | None |
| **Honeycomb** | Observability + Agent Skills | Platform no; agent-skill repo yes | OTel traces via a Claude Code plugin/skill; Honeycomb MCP | Named; skill-based, not a packaged dashboard | None |
| **Grafana Cloud + community dashboards** | Observability | Grafana core yes; dashboards yes (IDs 25052, 25255) | Claude Code native OTLP to Prometheus/Loki/Mimir | Explicit, two published dashboard templates | None |
| **New Relic "Preflight"** | Observability, OSS-first | Apache-2.0 core; cloud dashboards proprietary | Hooks + a local MCP server capturing file reads, edits, commands; optional OTLP | One of ~11 supported agents | Anti-pattern detection (loops, blind edits) — efficiency, not threat |
| **DX (getdx.com)** | DevEx / AI measurement | No | Anthropic admin API + Copilot API, mapped to org hierarchy | Explicit connector, dedicated docs | None |
| **Faros AI** | Engineering intelligence | Community Edition OSS | Copilot/Cursor/Claude/Devin usage APIs + git/Jira + 100 integrations | Named, cross-tool ROI comparison | None |
| **Jellyfish** | Engineering intelligence | No | Multi-tool AI usage and spend joined to delivery metrics | Dedicated "Claude Code Dashboard" product page | None |
| **LinearB** | Engineering intelligence | No | Git/PR metadata + Copilot API | Shallow; metadata only | None |
| **Swarmia** | Engineering intelligence | No | Seat/licence and activity via admin APIs, correlated with PR cycle time | Explicit (Oct 2025) | None |
| **ClawMetry** | Cross-agent OSS observability | MIT, ~417 stars, very active | Reads local session logs of 20-26 agent runtimes, zero-config, read-only | One of many runtimes | Light — a "kill switch" on token-maxing signals, not threat detection |
| **Zenity** | AI agent security / shadow-AI | No | SaaS/cloud/endpoint APIs; agent + MCP inventory, permissions, memory access | Generic | Core focus |
| **Wiz AI-SPM** | Cloud security posture | No | Cloud config scanning; discovers MCP servers and agents in cloud envs | Generic | Core focus |
| **Invariant Labs mcp-scan** | MCP security scanner | Apache-2.0, ~3.1k stars | Static analysis of MCP client configs + a runtime intercepting proxy | Supports Claude Code as an MCP client | Core focus — tool-description injection, tool poisoning, tool shadowing |
| **Palo Alto Prisma AIRS** | AI runtime security | No | API-interception proxy over prompts, responses, tool calls | Named explicitly | Core focus |
| **ccusage** | Local cost/usage CLI | Yes, ~18.6k stars, very active | Local JSONL parsing, pricing tables, cache-token accounting, offline | Claude Code specific | None |
| **ColeMurray/claude-code-otel** | Self-hosted stack | MIT, ~502 stars | Native OTLP; docker-compose collector + Prometheus + Grafana | Claude Code only | None |

---

## Standards

`gen_ai.*` OpenTelemetry semantic conventions remain **experimental / "Development"** as of mid-2026.
The GenAI convention set (spans, metrics, MCP-specific attributes) was split out of the main OTel
repository into `semantic-conventions-genai` in **June 2026, with no tagged release**. Core attributes
(model, token usage, operation name) have been shape-stable since v1.37.

Claude Code's schema is **hybrid**: primary telemetry under its own `claude_code.*` namespace, with a
documented subset of `gen_ai.*` attributes (`gen_ai.system`, `gen_ai.request.model`,
`gen_ai.response.finish_reasons`, `gen_ai.tool.call.id`) layered onto spans for interoperability. Span
*naming* diverges from the convention's `invoke_agent` / `chat` / `execute_tool` pattern even where
attributes align.

**Consequence for Cardo:** there is no stable neutral standard to adopt. The neutral layer is something
we define — see [ADR-0010](../adr/0010-layered-schema.md), which makes it a set of rewritable SQL views
precisely because of this.

---

## White space — what nobody does well

1. **Nobody feeds coding-agent session telemetry into threat detection.** Usage observability (Datadog,
   Grafana, Honeycomb) and agent security (Zenity, Wiz, Prisma AIRS) are two completely disconnected
   pipelines observing the same activity. The raw session data already exists; nobody is using it for
   anomalous-file-access or exfil-shaped tool-call sequences. → [ADR-0003](../adr/0003-efficiency-before-security.md)
2. **Nobody does enablement-artifact analysis.** No product asks whether the skill you shipped is being
   used, whether your rules are actually loading, or which content hash the fleet is on. This did not
   even appear as white space in the survey because nobody has conceived of it.
   → [ADR-0001](../adr/0001-unit-of-analysis.md)
3. **Outcome correlation is entirely paywalled.** DX, Faros and Jellyfish charge specifically for joining
   AI usage to delivery and quality outcomes with org-hierarchy mapping. No OSS project attempts it
   seriously. → deferred in [ADR-0008](../adr/0008-outcome-variable.md)
4. **MCP governance stops at scanning.** mcp-scan is a good scanner and proxy, but nobody has a full MCP
   server lifecycle inventory — who owns it, what changed, version drift — comparable to a CMDB.
5. **Self-hosted + multi-agent + privacy-preserving is nearly empty.** The genuinely self-hosted stacks
   are Claude-only and dashboard-only; the multi-agent tools push you to a vendor cloud for anything
   beyond viewing one machine. → [ADR-0013](../adr/0013-deployment-topology.md)

---

## Already solved — do not rebuild

- **Local usage and cost parsing.** `ccusage` (~18.6k stars, actively maintained) already handles JSONL
  parsing, pricing-table maintenance, cache-token accounting and offline mode.
- **Collector + dashboard boilerplate.** `ColeMurray/claude-code-otel` (~502 stars, MIT) is a full
  docker-compose collector + Prometheus + Grafana stack; Grafana publishes dashboards 25052 and 25255.
  Fork or import rather than rebuild. → [ADR-0009](../adr/0009-storage-agnostic-clickhouse-reference.md)
- **CLI instrumentation wrappers.** Legacy pattern now that native OTel exists. Do not build a `claude`
  command wrapper.
- **Multi-runtime log-location adapters.** ClawMetry already did the unglamorous work of knowing where
  20+ agent CLIs write session data and how to normalize it. Expensive to redo — and irrelevant to
  Cardo, which does not read session logs at all (INV-1).
- **MCP security primitives.** `mcp-scan` (Apache-2.0, ~3.1k stars) implements tool-description injection
  detection, tool poisoning checks and cross-origin / tool-shadowing detection with a runtime proxy.
- **DORA / delivery-metrics math.** Well-trodden by LinearB, Swarmia, Faros CE and Four-Keys-style OSS.
  Borrow rather than re-derive if outcome correlation is ever built.

---

## The competitive risk, stated plainly

**Datadog Agent Console is a real competitor with real distribution.** It consumes the same native
OTLP that Cardo does, plus the Anthropic admin API, and already does retry-loop detection. For any
organization already paying for Datadog, "enable the module you already have" is a strong
alternative.

Cardo's differentiators are real but none are free:

- self-hosted, single-tenant, no vendor ([ADR-0013](../adr/0013-deployment-topology.md))
- the pseudonymity architecture and the tier split ([ADR-0002](../adr/0002-three-tier-data-model.md),
  [ADR-0006](../adr/0006-pseudonymization.md))
- the enablement-artifact framing ([ADR-0001](../adr/0001-unit-of-analysis.md))
- hook-derived signals nobody else collects ([ADR-0005](../adr/0005-collection-mechanism.md))

Tracked as `risks.md` #2.

## Source URLs

- https://docs.datadoghq.com/ai_agents_console/ and https://www.datadoghq.com/blog/claude-code-monitoring/
- https://www.honeycomb.io/technologies/ai-agents and https://github.com/honeycombio/agent-skill
- https://grafana.com/grafana/dashboards/25052-claude-code/
- https://newrelic.com/press-release/20260608 and https://github.com/newrelic-experimental/preflight
- https://getdx.com/blog/dx-releases-integration-with-claude-code/ and https://docs.getdx.com/connectors/claude-code/
- https://www.faros.ai/copilot-module
- https://jellyfish.co/platform/claude-code-dashboard/
- https://www.swarmia.com/changelog/2025-10-27-claude-code/
- https://github.com/ccusage/ccusage
- https://github.com/ColeMurray/claude-code-otel
- https://github.com/vivekchand/clawmetry
- https://zenity.io/platform/mcp-security
- https://github.com/invariantlabs-ai/mcp-scan
- https://www.paloaltonetworks.com/blog/network-security/securing-the-ai-frontier-prisma-airs-claude-code/
- https://opentelemetry.io/blog/2026/genai-observability/
