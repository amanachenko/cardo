# Open risks

Each risk names the ADR it threatens. This file is edited in place — close risks by striking them
through with a note on how they were resolved, rather than deleting them.

---

### 1. The friction index is unvalidated

**Update 2026-09-25: the index is superseded, and its replacement is no better validated.** Field
estimates put nearly everyone in auto mode now. There, an allowed call raises no prompt and no edit
is held for review, so two of the index's three components fall towards zero.
[ADR-0031](docs/adr/0031-what-works-means.md) replaces it with an efficiency family:

- failed tool calls and retry loops;
- auto-mode denials;
- compactions;
- churn;
- turns and cost per accepted change.

Every part of that family is a hypothesis the dogfood and the adoption pilot test, and churn is the
least certain. The volume rule below carries over unchanged. **Would falsify the family:** approaches
the adopting organizations know to be better showing no difference in it, compared within the same
people.

The original text follows.

**Threatens:** [ADR-0008](docs/adr/0008-outcome-variable.md), and therefore
[ADR-0001](docs/adr/0001-unit-of-analysis.md).

The friction index was chosen because it is computable from data we already have, **not** because it is
proven to correlate with anything an engineering org cares about. The first deployment is partly an
experiment to find out whether it does.

**Mitigation:** always present it with a volume denominator. Without one it rewards timidity — a session
with zero friction may be a session that accomplished nothing.
**Would falsify it:** artifact adoption changes with no movement in the index, or index movement with no
corresponding change in anything a team recognises as better.

### 2. Datadog Agent Console is a real competitor with real distribution

**Threatens:** the project's reason to exist.

Datadog consumes the same native OTLP Cardo does, plus the Anthropic admin API, and already ships
retry-loop detection. For an organization already paying for Datadog, enabling a module they already have is a
strong alternative.

**Mitigation:** compete where Datadog structurally cannot — self-hosting with no vendor, the tier split
and pseudonymity architecture, the enablement-artifact framing, and the hook-derived signals. All real;
none free.

**Update 2026-09-25: the field is wider than Datadog.** Two changes since this was written:

- **Leadership is now the main audience** ([ADR-0030](docs/adr/0030-stakeholders-and-questions.md)).
  That puts Cardo next to DX, Jellyfish and Faros, which sell cross-tool AI return-on-investment
  tied to delivery data.
- **Anthropic's own analytics overlap more than assumed.** They report usage and cost per person by
  email, and the Enterprise Analytics API documents skill and connector usage.

The ground Cardo holds is narrower and specific:

- which team-grown approach wins, and at what cost;
- anything by team;
- instructions versions;
- policy evidence with coverage;
- self-hosting.

If Anthropic ships artifact-level analytics of its own, the first of those shrinks.

### 3. ~~Admin API and managed settings are unavailable on Bedrock / Vertex / Foundry~~ — RESOLVED

**Resolved 2026-09-22 by [ADR-0018](docs/adr/0018-provider-scope.md)**, upheld by
[ADR-0021](docs/adr/0021-analytics-source-scope.md) when ADR-0018 was superseded on other grounds:
v1 targets Claude API direct, where the full surface is available. Bedrock, Vertex, Foundry and
Claude Platform on AWS are explicitly out of scope for v1. Reopen if an adopting organization requires one.

### 4. OTTL will probably become unwieldy

**Threatens:** [ADR-0012](docs/adr/0012-ingest-implementation.md).

Expressing extraction and shaping for 13 differently-shaped hook payloads in OTTL is expected to get
ugly. When it does, the answer is to write the thin Go receiver service — which costs the
"no code we wrote touches your events" claim for the hook path.

**Mitigation:** the pure-config claim is worth attempting and cheap to abandon. Treat OTTL pain as the
trigger, not as a failure.

**Status 2026-09-23: holding.** The whole collector is about thirty statements and still reads
top to bottom. One allowlist covers all thirteen events, because the list is a union
([ADR-0023](docs/adr/0023-hook-payload-allowlist.md)). The first part that needed real care was the
naming redaction: an empty regex matches everything, so a blank setting disabled it until each
statement special-cased `""`. The next likely strain is silver-side logic creeping into the
collector. Joins belong in SQL, not OTTL.

**Status 2026-09-24: holding, at about fifty statements.** The first real payloads added:

- per-event mappings of generic field names;
- type guards on the model-switch fields;
- one derived name;
- a strict naming mode.

Every statement is still a single line with a single purpose. Two static tests now check the
config's shape as well as its content: generic fields must name their event, and strict-mode checks
must fail closed.

### 5. Stable salt keeps T1 data personal for its full retention

**Threatens:** [ADR-0006](docs/adr/0006-pseudonymization.md).

With a stable salt, tier-1 data remains personal data under GDPR for the whole 90-day window. Accepted
for v0.1 to avoid over-engineering before a single deployment exists.

**Mitigation:** retention is the control ([ADR-0016](docs/adr/0016-retention.md)), stated plainly rather
than hidden behind the word "pseudonymous". `salt_version` is stored from day one so rotation stays
possible.

### 6. The vendor-neutral schema is something we define, not something we adopt

**Threatens:** [ADR-0004](docs/adr/0004-claude-code-first-neutral-schema.md).

`gen_ai.*` OTel conventions are still experimental with no tagged release, so there is no standard to
conform to. Whatever we define may diverge from where the ecosystem lands.

**Mitigation:** [ADR-0010](docs/adr/0010-layered-schema.md) makes the canonical layer a set of SQL views
over verbatim raw storage, so realigning later is a view rewrite plus a backfill, not a migration.

### 7. Hook payload shapes are documented, not observed

**Threatens:** [ADR-0005](docs/adr/0005-collection-mechanism.md), Phase 2 generally.

Every field name in the design came from documentation. Nothing has been verified against a real
payload.

**Mitigation:** the Phase 2 fixture corpus ([ADR-0014](docs/adr/0014-version-drift.md)) becomes the
authority. Exercise all 13 hooks deliberately during the local verification loop before building
anything on top of them. See flagged uncertainties in
[the telemetry snapshot](docs/research/2026-09-20-claude-code-telemetry-surfaces.md).

**Status 2026-09-23: still open, and now demonstrated rather than hypothetical.** Between the
2026-09-20 and 2026-09-23 snapshots the documented names changed: `source` became
`session_start_reason`, `prompt_text` became `user_input`, and the compaction token counts
disappeared from the documented shape. `PermissionDenied` also turned out to fire only on auto-mode
denials, which moved the end of permission-wall timing to the OTel `tool_decision` event
([note](docs/research/2026-09-23-hooks-otel-collector-surfaces.md)).

The collector is built and verified against those documented shapes, and nothing has been observed
from a real Claude Code. The capture attempt, a headless `claude -p` session launched by the
building agent, was blocked by that agent's permission classifier. That is correct: launching a
nested agent session is the operator's call. Two things now make the gap cheap to close. The
collector records `cardo.received_keys`, the name of every field that arrived, including the dropped
ones. And the allowlist keeps both the old and the new names wherever both are content-free. One
engineer running Claude Code with `deploy/managed-settings/local-evaluation.json` against the
reference stack settles it without anyone's prompt text being stored.
**Would falsify the current build:** a mandatory event whose real payload carries none of the
allowlisted fields beyond the common ones, which would mean the derived facts silently never
populate.

**Status 2026-09-24: observed, and it was falsified.** One real session with Claude Code 2.1.281
([note](docs/research/2026-09-24-first-real-hook-payloads.md)) found the following:

- **Four events carried none of their allowlisted event-specific fields:** `SessionEnd`,
  `PreCompact`, `PostCompact` and `ConfigChange`. They send `reason`, `trigger` and `source`. The
  2026-09-23 names came from a summarized documentation page, not from the page.
- **Two fields the design leaned on do not exist.** `InstructionsLoaded` has no `content_hash`, and
  `PermissionRequest` has no `tool_use_id`.
- **`SessionStart`'s HTTP hook is registered and never run.**
- **`SubagentStop` also fires for an internal helper.**
- **The hook receiver silently refused bodies over 100 KiB.**

All of it is now handled:

- The field names are mapped for their event.
- Stale detection moved to versioned names ([ADR-0026](docs/adr/0026-stale-instructions-by-versioned-name.md)).
- The permission join uses prompt and tool.
- Session start comes from OTel.
- The receiver limit is 16 MiB.

The fixtures now carry the observed names.

**Still open:** `SessionStart` and `PermissionDenied` payloads, whether `@`-imports fire
`InstructionsLoaded`, and the types of the model-switch fields. A second short session settles the
last two. The broader lesson stands for the next Claude Code release: documentation, and
especially a summary of it, is a hypothesis about field names.

**Status 2026-09-24, second session: two of those four are settled**
([note](docs/research/2026-09-24-second-real-session.md)).

- **`@`-imports do fire `InstructionsLoaded`**, with `load_reason=include` and a new
  `parent_file_path`. ADR-0026's thin-`CLAUDE.md` route works.
- **The model-switch fields have the types the guards expect.**
- **Two design errors turned up and are fixed in the design docs, not yet in code**, because
  neither is built:
  - Permission waits had one end on each clock, which made them about 1.3 s too long.
  - Claude Code's switch-cost estimate overstates what a switch costs.
- `SessionStart` still does not run, and auto mode approved the second attempt at a denial too.
  The permission-wait start now relies on the undocumented `hook_execution_start` event. That is a
  new place for drift to bite, and the hook row's time is the fallback.

**Status 2026-10-06: `SessionStart` is settled, and the design had misread `async`**
([note](docs/research/2026-10-06-http-hooks-wait.md)).

- **Claude Code runs no HTTP hook on `SessionStart`**, by its own documentation. The bundle no
  longer hooks it ([ADR-0039](docs/adr/0039-hooks-wait-at-most-one-second.md)).
- **`async: true` does nothing on an HTTP hook.** Every hook blocked its event until the collector
  answered, with no limit set. Each now gives up after one second (risk 20).
- **`PostModelSwitch` was observed on 2.1.289** with the same field names as `PreModelSwitch`,
  but no `prompt_id`, and a dated `to_model` where Claude Code's hook name has the canonical one.
  The views match the two spellings.

**Still open:** `PermissionDenied`'s payload, and the value of `PostModelSwitch`'s `source` on a
real `/model`, which the one-laptop check will show.

**Status 2026-10-06, later: `PermissionDenied` is settled**
([note](docs/research/2026-10-06-first-permission-denied.md)). Two auto-mode denials on 2.1.290
each sent one. It carries `reason`, which the documented shape did not, under a generic name, so it
is dropped. All twelve hooked events have now arrived from a real Claude Code.

**Still open:** the value of `PostModelSwitch`'s `source` on a real `/model`.

**Status 2026-10-06, a real `/model`: nothing on this list is open**
([note](docs/research/2026-10-06-model-switch-observed.md)). Two `/model` commands on 2.1.291 each
sent `PostModelSwitch` with `source` `command`, which the views count. The event now also carries
`prompt_id` and `scratchpad_dir`, which the 2.1.289 resume did not. A switch made before a
session's first request has the session's start as its next request, and the view reports that as
the switch's cost.

### 8. The Claude Enterprise Analytics adapter is specified but unevidenced

**Threatens:** [ADR-0021](docs/adr/0021-analytics-source-scope.md).

[ADR-0021](docs/adr/0021-analytics-source-scope.md) commits v1 to supporting Claude Enterprise
organizations, on the strength of a prose sentence stating that the user-activity endpoint reports
"Claude Code (sessions, commits, pull requests, lines of code, tool actions)". **The response shape
was never retrieved.** Field names, nesting, and whether the Claude Code block is per-product or
flattened are all unknown, and are very unlikely to mirror the Console API's
`core_metrics` / `tool_actions`.

The estimate for "second adapter" is therefore an estimate for work whose shape nobody has seen.

**Mitigation:** the Console adapter goes first and proves everything source-independent —
pseudonymization, migrations, storage, dashboard. Retrieve the Enterprise reference before estimating
the second adapter, and treat the current plan for it as a sketch.
**Would falsify it:** the Enterprise user-activity record turns out to carry Claude Code metrics in a
shape that does not map onto the canonical session-day grain at all, making it a second data model
rather than a second source.

### 9. Pointing the poller at the wrong source fails silently

**Threatens:** [ADR-0021](docs/adr/0021-analytics-source-scope.md),
[ADR-0017](docs/adr/0017-v01-scope.md).

An Admin API key issued by a Claude Enterprise organization authenticates successfully against
`/v1/organizations/usage_report/claude_code` and returns **zero rows** — because Enterprise Claude
Code activity is reported by a different API entirely. There is no error. A successful install with
no data is indistinguishable from a correct install at an org with no Claude Code usage.

This is the worst failure shape available: it wastes a first impression, and the operator's natural
conclusion is that the tool does not work.

**Mitigation:** the poller must treat *authenticated, well-formed, zero rows across the whole backfill
window* as a **loud diagnostic**, not a quiet success — naming the Console/Enterprise distinction
explicitly in the message. This is a requirement on the Phase 1 binary, not a nice-to-have.

### 10. The gold marts are views, so ADR-0016's "aggregates indefinite" is promised but not built

**Threatens:** [ADR-0016](docs/adr/0016-retention.md).

[ADR-0016](docs/adr/0016-retention.md) sets tier-1 retention at 90 days and exempts derived
aggregates: *"Gold marts that carry no pseudonym are exempt and retained indefinitely, so
longitudinal trends survive the expiry of the rows behind them."*

The ClickHouse implementation does not do this. `bronze_actor_day` carries
`TTL day + INTERVAL 90 DAY DELETE`, and every gold mart is a **view** computed over it. When bronze
rows expire, the aggregates computed from them vanish too — silently, on a background merge, with
no error anywhere. An organization that has run Cardo for a year can answer "how did adoption move
last quarter" and cannot answer "how did it move last year", which is the question the product
exists to make answerable.

The decision was made on purpose and is recorded here rather than quietly: the alternative was to
build a rollup pipeline before anything had ever run against a live API, and materializing marts
whose shape has not survived contact with real data is the wrong order.

**Mitigation:** the fix is a cohort-only rollup table keyed by `(source, day)`, written by the
poller after each day's `Put` and therefore idempotent, with no TTL. It is not a materialized view:
ClickHouse materialized views fire on INSERT, and the store swaps partitions with
`REPLACE PARTITION`, which does not trigger them. Until that exists, **the practical retention of
every Cardo trend is 90 days, not indefinite**, and `docs/design/architecture.md` says so.
**Would falsify it:** a first organization for which 90 days of trend is enough, which would make the
rollup premature rather than missing.

**Update 2026-09-25: the collector path has the same gap, and the poller cannot close it.** The
collector-path gold marts (`sql/clickhouse/007_gold_collector.sql`) are views over the three
collector bronze tables, which carry the same 90-day TTL. The rollup above is written by the
poller after each day's `Put`; nothing on the collector path runs on a schedule to write one. The
fix there is either a scheduled `cardo` command that rolls up the previous day, or ClickHouse
materialized views on the bronze tables, which do fire on the exporter's plain INSERTs. Neither is
built, for the same reason as above: the marts' shape has seen two real sessions, not a fleet.

### 11. The reference stack has only ever run on one machine

**Threatens:** [ADR-0009](docs/adr/0009-storage-agnostic-clickhouse-reference.md).

The ClickHouse schema, the migration runner, the store and the Grafana dashboard have been verified
end to end — but against a single-node ClickHouse 26.6 on Docker Desktop, seeded with invented
data. Nothing here has met a replicated cluster, a ClickHouse behind a proxy, an older server, or a
Grafana with a pinned older plugin version.

Two specifics are known to be single-node assumptions. `ALTER TABLE ... REPLACE PARTITION` is
synchronous on a non-replicated table and is the mechanism the whole atomic-day-swap design rests
on; on `ReplicatedMergeTree` it is asynchronous and needs `alter_sync`. And `PARTITION BY (source,
day)` produces one partition per source per day, which is fine at 90 days and two sources and would
not be fine at a much longer horizon.

**Mitigation:** the integration tests are written against a real server rather than a mock, so
pointing them at a different ClickHouse is a change of environment variable, not of code. Run them
against the organization's actual server before the first deployment rather than after.

### 12. Nobody can evaluate Phase 1 without already being an organization

**Threatens:** [ADR-0017](docs/adr/0017-v01-scope.md), whose whole argument is that the
Admin-API wedge "needs no deployment approval from anyone" and proves the analysis layer before
anyone is asked for managed-settings access.

That is still true of *deployment*. It is not true of *access*. Measured on 2026-09-23 against a
live personal Pro account ([note](docs/research/2026-09-23-admin-api-individual-account.md)): every
Admin API endpoint returns 403 for an individual account, including the Claude Code Analytics one,
regardless of key type. The gate is the account type and it sits upstream of any data.

So the wedge's real precondition is not "an admin key" but "an organization that already has Admin
API access, and someone inside it willing to mint a key". That is a much narrower door than
ADR-0017 assumes, and it lands precisely on the people Cardo most wants to reach early: an
individual engineer who wants to see whether this is worth advocating for internally cannot run it
at all. They get a 403 and a support link.

This also removes the cheap validation path this project was counting on. Every field name in the
console adapter is still taken from documentation, and the first time any of them meets a real
response will be inside an organization's environment rather than in a test.

**Mitigation, partial:** the 403 now explains itself rather than sending the reader to support, and
says plainly that `/v1/organizations/me` answering 200 does not mean the key works.

**The real answer is probably the other collection path.** `CLAUDE_CODE_ENABLE_TELEMETRY=1` with an
OTLP endpoint is client-side configuration with no documented account-type requirement, so a single
engineer on any plan can point their own sessions at a local collector and see real data. That is
Phase 2's collector rather than Phase 1's poller, and it inverts ADR-0017's ordering. Not acted on:
ADR-0017 was decided with reasons that still hold for a deployment at an organization, and reordering the
phases on the strength of one account's 403 would be overcorrecting. Revisit if a second
organization turns out to be hard to reach.

**Update 2026-09-23:** that path now exists. The collector, the bundle and a local-evaluation
variant of it are built, and one engineer can run the whole thing against the reference stack with
one `--settings` flag. No organization, admin key or approval is involved. Whether Claude Code
emits OTel at all on a Pro account is **still unmeasured**; the documentation neither says so nor
rules it out. The hook half does not depend on that: hooks are settings, not telemetry. ADR-0017's
ordering is still not being revisited on this evidence alone.

**Update 2026-09-24: measured.** Claude Code emits OTel on an individual account, with
`user.email` and `organization.id` on every record
([note](docs/research/2026-09-24-first-real-hook-payloads.md)). The collector path is therefore a
working evaluation route for one engineer with no organization. The Admin API path still is not.

**Would falsify it:** an individual or Team account that does reach the Claude Code Analytics
endpoint. One `curl` settles it, and the measurement note records exactly what was and was not tried
— notably that converting a Console org to a Team org was not tested, because the billing
consequence was undocumented and the cost of being wrong was the operator's money.

### ~~13. The collector path has no evaluation mode without ClickHouse~~

**Closed 2026-09-25 by [ADR-0028](docs/adr/0028-file-store-is-admin-api-only.md):** the file store
serves the Admin API path only, and the collector-path views exist in ClickHouse alone. No pilot
team that could run a collector but not ClickHouse had turned up. A team that does is the trigger
for a superseding ADR. The original text follows.


**Threatens:** [ADR-0009](docs/adr/0009-storage-agnostic-clickhouse-reference.md), whose file store
exists so that "a five-engineer pilot needs no infrastructure approval at all".

The collector writes to ClickHouse only. On the Admin API path the file store and ClickHouse hold the
same columns and publish the same views, and `CLAUDE.md` says a change to one belongs in both. The
collector path does not follow that rule yet, and that was a choice made in the open, not an
oversight.

The case for leaving it: a Phase 2 pilot already needs a collector running somewhere, so "no
infrastructure" is gone in a way it is not for the poller. The reference stack is one `docker
compose up`, and for a single engineer that is the evaluation mode. The case for building it: the
collector's `file` exporter writes OTLP JSON that DuckDB can read. That would cost a second set of
silver views over a deeply nested format, and every Phase 2 view would then be written twice.

**Decision needed before the Phase 2 silver views are written**, because it doubles or does not
double that work. Until then, nothing may quietly add a file-store path or quietly declare there will
never be one.
**Would settle it:** a pilot team that can run a collector but cannot run ClickHouse. If that team
never turns up, an ADR scoping the file store to the Admin API path is the honest outcome.

### 14. The collector-path views recompute everything on every read

**Threatens:** the reference stack's claim to serve a fleet
([ADR-0009](docs/adr/0009-storage-agnostic-clickhouse-reference.md)).

The silver and gold views over the collector's data are plain views. `silver_session` groups every
OTel log row, metric row and hook row by session; five of the other six silver views join back to
it; and each gold view joins several silver views to `silver_session_cohort`, which reads
`silver_session` again. A dashboard with thirteen panels therefore scans the bronze log table many
times per refresh.

On two real sessions that is milliseconds. At a thousand engineers and 90 days it is on the order
of tens of millions of log rows per scan, and nobody has measured it.

**Mitigation:** when a real deployment is slow, materialize silver: a daily table per view, written
by a scheduled job or a ClickHouse refreshable materialized view, with the gold views unchanged on
top. The same job is the fix for #10 on this path. Until then it is not built, because the views'
shape is still being learned and a materialized table is a migration each time it changes.
**Would falsify it:** a deployment of a few hundred engineers whose dashboards load in seconds on
the views as they are.

### 15. Any Grafana login can read every row the data source can

**Threatens:** INV-3, and [ADR-0032](docs/adr/0032-engineer-coach-by-shared-link.md).

Measured 2026-09-25
([note](docs/research/2026-09-25-grafana-access-and-shared-links.md)). A user with the `Viewer`
role sent a hand-written query to `/api/ds/query` and read the bronze log table. Dashboards run
their panels through that same endpoint, so panels do not limit what a login can read. The data
source's database user does.

The reference stack's data source connects as the main ClickHouse user, which reads bronze and
silver. Today only the operator has a login, so nothing is exposed. **The first login given to
anyone else can read every per-person row.** The CI check on dashboards (no pseudonym, no bronze
or silver) is real, but it only covers what a dashboard shows. It says nothing about what a login
can query.

**Mitigation:** before anyone else gets a login, the leadership Grafana reads through a ClickHouse
user granted the gold views only. Per-person pages move to a second Grafana that only the operator
logs in to (ADR-0032). **Would close it:** that grant in place, and a live test that the leadership
data source's user is refused a bronze or silver read.

### 16. Two of the new signals rest on unobserved or conditional telemetry

**Threatens:** [ADR-0035](docs/adr/0035-repository-identity-classified.md) and
[ADR-0037](docs/adr/0037-policy-evidence.md).

- **The repository attributes are documented, not observed.** Nobody knows yet whether they
  arrive on logs or only on metrics, or in what URL forms.
- **Other-organization sessions are visible only when managed settings are pushed as a file.**
  Settings delivered by Anthropic's server apply only after signing in to the organization.

**Would settle it:** the dogfood, for the first. The first point is a fact about Claude Code, so the
answer goes in a dated research note.

**Status 2026-10-06: the first is settled on one machine**
([note](docs/research/2026-10-06-http-hooks-wait.md)). On 2.1.289 to 2.1.291 the repository arrives
on every log record and every metric data point, never on the resource, and the collector stores
it classified, with no `vcs.*` key left. A team's URL forms are still to be seen.

### 17. A coach page can be demanded, or forwarded

**Threatens:** INV-3 in practice, and [ADR-0032](docs/adr/0032-engineer-coach-by-shared-link.md).

A manager can ask an engineer to show their page in a one-to-one, and no design prevents the
asking. A shared link works for whoever holds it, so a volunteer can forward their own page. During
a pilot the operator can open every page.

**Mitigation:** the page carries no score, no rank and no comparison with anyone else, so it is
useless as evidence. Volunteers opt in knowing who can open it, and the links are revoked when the
pilot ends. **Would show the mitigation failing:** the pilot's "do you feel more watched?" answer
getting worse, or any report of a manager asking to see a page.

### 18. Comparing tools fairly is harder than it looks

**Threatens:** [ADR-0030](docs/adr/0030-stakeholders-and-questions.md) decision 8.

Each vendor defines "accepted", "session" and "active" its own way, so their numbers side by side
are not a comparison. The git host measures every tool the same way, but only for a tool that
marks its commits or PRs. Claude Code's co-author line is a setting an organization or a person
can turn off, and other tools' marks have not been checked.

**Would settle it:** checking what each tool leaves in the git history when the first organization asks
for a comparison, and reporting the share of PRs whose tool is unknown beside every comparison.

### 19. Anyone who can reach the collector can write to it

**Threatens:** the credibility of every view, and
[ADR-0024](docs/adr/0024-bundle-configures-telemetry-only.md), carried into
[ADR-0039](docs/adr/0039-hooks-wait-at-most-one-second.md).

The collector accepts hooks and OTel from any machine that can reach ports 4318 and 8088. Nothing
authenticates the sender, by design: a token in the settings file would be readable by every
engineer who has the file, so it would not be a secret (ADR-0024). The collector serves nothing
back, so nothing can be read through it. But anyone on the network can post invented events: push
a home-grown artifact past five people, or add sessions that never happened.

**Mitigation:** the network overlay's runbook opens the two ports to the office and VPN ranges
only, and the dogfood is one team. **Would close it:** a decision before the adoption pilot,
between per-machine client certificates pushed by MDM (Claude Code supports
`CLAUDE_CODE_CLIENT_CERT` for its exporter), an address allowlist at the proxy, and accepting the
risk in writing.

### 20. A hook waits for the collector, up to a second

**Threatens:** [ADR-0039](docs/adr/0039-hooks-wait-at-most-one-second.md), and the promise that
installing Cardo changes nothing an engineer would notice.

Claude Code waits for every HTTP hook. A working collector answers in milliseconds. One that is
down, out of reach or stuck costs each hooked event up to the one-second timeout, and Claude Code
shows a hook error or a timeout notice under the message. An engineer off the VPN sees one on every
prompt. A hook that times out also loses its event, so a slow network costs data as well.

**Mitigation:** the collector's name resolves only inside the network where it can, so an
off-network lookup fails in about 0.2 s. **Would settle it:** the one-laptop check's round-trip
time over the VPN, and whether engineers in the dogfood report the notices. If healthy hooks come
near a second, the timeout is revisited with that measurement; if the notices annoy people, the
answer is a reachable collector, not a longer timeout.
