---
paths:
  - "sql/**"
  - "dashboards/**"
  - "internal/source/**"
  - "internal/store/**"
---

# Views, marts and dashboards

The views read the organization's artifact pattern and the minimum group size from
`cardo.settings`, which `cardo migrate` writes from `CARDO_ORG_ARTIFACTS` and
`CARDO_MIN_GROUP_SIZE` ([ADR-0029](../../docs/adr/0029-minimum-group-size.md)). Service rows
(`actor_kind=service`) stay out of people counts, the five-person rule, budget lists and the coach
([ADR-0036](../../docs/adr/0036-service-runs-are-not-people.md)).

## Things that look like good ideas and are not

- Reading `estimated_cost.amount` as dollars. **It is cents**, on both analytics APIs. Silver divides
  once and names the column `cost_usd`; nothing downstream should divide again.
- `sum(x) AS x` in a ClickHouse view, or in a dashboard panel. Legal in DuckDB, an error here as
  soon as `x` is used again in the same query: a panel computing
  `sum(r) / nullIf(sum(sessions), 0)` beside `sum(sessions) AS sessions` fails with "aggregate
  function found inside another". Aggregate in a subquery with a `_total` suffix and rename
  outside, so the published names still match. Run a new panel's query through Grafana, not only
  through ClickHouse directly: the macros are Grafana's.
- Adding up `people` or `active_people` across weeks or days. They are distinct counts; the same
  engineer on five days is one person, not five. A panel over a range shows the busiest week or
  day. Sessions, uses and prompts add up.
- Reading a dashboard's queries from its top-level `panels`. A collapsed row keeps its panels
  inside it, and query variables and annotations send SQL through the same data source. The
  dashboard tests read every query through `dashboardQueries` in `test/deploy_test.go`, and a new
  test reads them the same way.
- Checking that a panel charts a rate's volume by looking for the volume's name in the SQL. Each
  rate is computed from its volume, so the name is always there. The test requires the volume as
  an output column.
- Ordering rows by a server timestamp to decide which write is newest. The reference stack's
  Docker VM clock steps backwards, and a settings write 5 ms after another lost to it one run in
  eight. `cardo.settings` orders by a version the server computes as one above the highest.
- Counting subagents from `SubagentStop`. Claude Code's own helpers, prompt suggestion and
  compaction, fire it with no `SubagentStart`. Pair the two on `agent_id`.
- Subtracting a hook row's time from an OTel row's. A hook row carries the collector's receive
  time; OTel carries the laptop's. On the reference stack they were 1 to 3 s apart, and not steady,
  which added about 1.3 s to each real permission wait. Both ends of a duration come from one
  clock: a permission wait starts at OTel's `hook_execution_start` for `PermissionRequest:<tool>`.
- Reporting a model switch's `estimated_cache_write_usd` as what the switch cost. It assumes the
  whole context is rewritten. In the switch observed, most of it was still cached, and the estimate
  was 3.2 times the next request's entire `cost_usd`. The next main-thread `api_request` is the cost,
  except after a switch made before the session's first one (`context_tokens` 0): that request is
  the session starting, and `silver_context_event` still reports it as the switch's cost.
- Matching a model switch's `to_model` to OTel's `hook_execution_start` as written. `to_model` may be
  dated while the OTel `hook_name` uses the canonical model name, so match the two with the date and
  `[1m]` removed. `PostModelSwitch` also fires on fallback (`auto`) and resume, which the views
  leave out.
