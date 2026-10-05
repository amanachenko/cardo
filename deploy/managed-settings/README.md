# Managed-settings bundle

This directory is the **entire client side of Cardo**. There is no agent, daemon or script on
developer machines ([ADR-0011](../../docs/adr/0011-hook-transport-http.md)). If you are an engineer
wondering what Cardo does on your laptop, the answer is in the one JSON file your organization
deployed, and this page explains every line of it.

| File | For |
|---|---|
| `managed-settings.json` | The template an organization deploys through MDM, GPO or Intune. Replace the collector host first |
| `local-evaluation.json` | One engineer trying Cardo against the reference stack on their own machine. No MDM needed |

## What the bundle does

**It turns on Claude Code's own OpenTelemetry and adds thirteen hooks. Nothing else.**
[ADR-0024](../../docs/adr/0024-bundle-configures-telemetry-only.md) makes that a rule, and
`test/bundle_test.go` enforces it.

### `env`: Claude Code's native telemetry

| Setting | Value | Why |
|---|---|---|
| `CLAUDE_CODE_ENABLE_TELEMETRY` | `1` | Turns telemetry on |
| `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | `otlp` | Metrics and events go to the collector. Traces stay off: they are beta and not needed |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | The one protocol the collector accepts |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | your collector | **Inside your network.** Never a third party (INV-7) |
| `OTEL_METRICS_INCLUDE_SESSION_ID` | `true` | Lets hook events and metrics be joined per session |
| `OTEL_METRICS_INCLUDE_VERSION` | `true` | Claude Code version, for the compatibility matrix |
| `OTEL_METRICS_INCLUDE_ACCOUNT_UUID` | `false` | Account identifiers are not needed and are dropped anyway |
| `OTEL_METRICS_INCLUDE_REPOSITORY` | `false` | Which repository you work in is not collected |
| `OTEL_RESOURCE_ATTRIBUTES` | `cardo.cohort=unassigned` | Your team, for team-level views. See *Cohorts* below |

**Content logging, every flag explicitly off** (INV-5). Claude Code already defaults all of these
off. They are written out anyway, so that the decision is on the page rather than assumed:

| Flag | What it would have sent had it been `1` |
|---|---|
| `OTEL_LOG_USER_PROMPTS` | the full text of everything you type |
| `OTEL_LOG_ASSISTANT_RESPONSES` | the full text of every response |
| `OTEL_LOG_TOOL_DETAILS` | tool parameters, file paths, commands, and the names of your own commands and MCP servers. Cardo records those *names* from the hooks anyway; see below |
| `OTEL_LOG_TOOL_CONTENT` | file contents from Read, Write and Edit |
| `OTEL_LOG_RAW_API_BODIES` | complete API requests and responses |
| `OTEL_LOG_MANAGED_SETTINGS` | the organization's resolved managed settings |

### `hooks`: thirteen events, async HTTP

`SessionStart`, `SessionEnd`, `UserPromptSubmit`, `UserPromptExpansion`, `PermissionRequest`,
`PermissionDenied`, `PreCompact`, `PostCompact`, `InstructionsLoaded`, `SubagentStart`,
`SubagentStop`, `PreModelSwitch`, `ConfigChange`. That list is the mandatory tier, and adding to it
needs a recorded decision (INV-4).

Every hook is `async: true`, so it fires and forgets. **Cardo never waits on the network during
your session, and never blocks anything.** If the collector is down, the event is lost and your
session does not notice.

In Claude Code 2.1.281 the `SessionStart` hook is registered and never run, so twelve of the
thirteen can arrive. The session's start is taken from Claude Code's own OTel instead
([note](../../docs/research/2026-09-24-first-real-hook-payloads.md)).

Claude Code sends each hook its full input, and that input includes your prompt text, command
lines and file paths. **The collector discards all of that before anything is written**, keeping
a short allowlist of content-free fields, plus a few facts derived from content, such as the
length of a prompt rather than the prompt itself
([ADR-0023](../../docs/adr/0023-hook-payload-allowlist.md)). The allowlist is in
[`deploy/collector/config.yaml`](../collector/config.yaml), in plain sight.

**One thing on that list is more than Claude Code's own telemetry sends by default:** the names of
slash commands, subagents and MCP servers, including ones engineers define for themselves
([ADR-0025](../../docs/adr/0025-artifact-names-kept-with-guardrails.md)). A name is shown in a view
only once at least five people use it in a week, and never counted per person
([ADR-0029](../../docs/adr/0029-minimum-group-size.md)). What this means for an engineer is
set out in [`docs/design/privacy.md`](../../docs/design/privacy.md).

## What the bundle deliberately does not do

Managed settings can do much more than this, and each of the following would change how Claude
Code behaves for you. Cardo sets none of them:

- **No `allowManagedHooksOnly`.** Your own hooks and plugins keep working.
- **No `allowedHttpHookUrls`.** Defining it anywhere restricts *every* HTTP hook to the list, so
  setting it here would silently break any HTTP hooks you already have. If your organization
  already restricts HTTP hooks, it adds the collector URL to its own list.
- **No permission rules, no MCP policy, no model or effort limits.**
- **No version pin, and never a maximum** (INV-6).

**One side effect cannot be avoided.** When an organization sets the OTel endpoint through managed
settings, Claude Code removes any per-signal OTel endpoints you had configured yourself. If you
were exporting telemetry to a personal tool, that stops.

## Deploying it (platform team)

1. Stand up the collector ([`deploy/collector/`](../collector/)) inside your network, behind TLS,
   and reachable **only** from developer machines. The reference stack's network overlay does this
   ([Serving a team over the network](../compose/README.md#serving-a-team-over-the-network)). The hook endpoint has no authentication of its
   own. A token placed in this file would be readable by every engineer, so it would not be a
   secret ([ADR-0024](../../docs/adr/0024-bundle-configures-telemetry-only.md)).
2. Copy `managed-settings.json`, replace both occurrences of `cardo-collector.internal.example`,
   and set `cardo.cohort` (below).
3. Deploy it to the managed-settings path:

   | OS | Path |
   |---|---|
   | macOS | `/Library/Application Support/ClaudeCode/managed-settings.json` |
   | Linux / WSL | `/etc/claude-code/managed-settings.json` |
   | Windows | `C:\Program Files\ClaudeCode\managed-settings.json` |

   If you already deploy managed settings, **merge** the `env` and `hooks` blocks into your file
   rather than replacing it.
4. **Tell engineers before it arrives**, and link them to this page and to
   [`docs/design/privacy.md`](../../docs/design/privacy.md).

### Cohorts

Team-level views need to know which team a session belongs to. Nothing in Claude Code says so, so
the bundle does: `OTEL_RESOURCE_ATTRIBUTES=cardo.cohort=<team>`. Ship one copy of the bundle per
MDM group with the value changed. Use ASCII only: no spaces, commas or quotes. Views over cohorts
enforce a minimum size of five people a day, so a cohort of two people is never shown as one
(INV-3, [ADR-0029](../../docs/adr/0029-minimum-group-size.md)). A cohort that small is counted as
`other` that day.

### Naming your organization's artifacts

Set `CARDO_ORG_ARTIFACTS` on the collector to a regex that matches what you ship, for example
`^acme-`. It does two things:

- **It separates what you shipped from what engineers built for themselves.** The dashboards show
  both: adoption of yours, and demand for theirs. When several engineers have built the same thing,
  that is a candidate for you to ship.
- **It is the only way an instructions file's name is recorded.** Claude Code sends no content hash
  for instructions files, so staleness is seen through names
  ([ADR-0026](../../docs/adr/0026-stale-instructions-by-versioned-name.md)). Put a version in the
  name of what you ship, such as `.claude/rules/acme-security-v3.md`, or a thin managed `CLAUDE.md`
  that imports `@acme-standards-v7.md`. A machine still loading `-v6` is stale. Names of files
  that do not match are never recorded.

**Strict mode.** If your works council or your engineers need it, set
`CARDO_ARTIFACT_NAMES=org-only`. Every command, subagent and MCP server name that does not match
`CARDO_ORG_ARTIFACTS` is then recorded as `custom`, as Claude Code's own telemetry does by default.
Say which mode you run when you tell engineers about the rollout.

## Trying it on your own machine (one engineer, no MDM)

This path needs no organization, no admin key and nobody's approval. It is also how Cardo's
documented hook shapes get checked against a real Claude Code.

```bash
cd deploy/compose
cp .env.example .env            # set both passwords, and CARDO_SALT (openssl rand -hex 32)
docker compose up -d            # ClickHouse, Grafana, the collector, and a one-shot migrate
```

Then run Claude Code with the local bundle layered on top of your own settings. It applies to that
session only and changes nothing permanently:

```bash
claude --settings /path/to/cardo/deploy/managed-settings/local-evaluation.json
```

Work normally. To see the other events, also: run a slash command, ask for a subagent, answer a
permission prompt, `/compact`, switch `/model`, edit a settings file mid-session, and try auto mode.
`SessionStart` will not appear on Claude Code 2.1.281 (see above).
Then see what arrived:

```sql
-- in http://127.0.0.1:8124/play
SELECT EventName, count() AS n, any(LogAttributes['cardo.received_keys']) AS fields_received
FROM cardo.bronze_hook_events GROUP BY EventName ORDER BY EventName;

SELECT EventName, count() FROM cardo.bronze_otel_logs GROUP BY EventName;
SELECT MetricName, count() FROM cardo.bronze_otel_metrics_sum GROUP BY MetricName;
```

`fields_received` lists every field Claude Code sent, including the ones the collector dropped. It
is how drift from the documented shapes shows up, without any content ever having been stored.
