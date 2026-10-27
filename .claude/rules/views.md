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

Seven silver views are stored (`sql/clickhouse/017_stored_silver.sql`,
[ADR-0052](../../docs/adr/0052-silver-is-stored-and-rebuilt-every-10-minutes.md)). `silver_session` is a table that
`silver_session_refresh` rebuilds every 10 minutes from `silver_session_live`, the plain view that
holds the query, and so on for the other six. `silver_artifact_load` is the exception: a plain view
over `silver_artifact_load_stored` that adds `is_org`. Gold and `silver_session_cohort` read the
stored tables; `cardo check`, the console and the coach read the `_live` views.

## Things that look like good ideas and are not

- A `_live` view that reads a stored table. Each reads the raw tables and other `_live` views only,
  so no rebuild depends on another having finished. The first prototype rebuilt the artifact table
  from `silver_session` while that was still empty, and every artifact counted 0 people, with no
  error.
- Changing a stored silver view through its refresher. The query is in its `_live` view: replace
  that, and the next rebuild uses it. A change to its columns has to change the stored table's too;
  017 drops the table and creates it again from the view.
- Storing what a setting decides. `is_org` and the five-person minimum are worked out as gold is
  read, so narrowing `CARDO_ORG_ARTIFACTS` hides a name on the next read rather than up to 10
  minutes later.
- Writing rows in a test and reading silver or gold straight after. The stored tables hold what the
  last rebuild saw: call `clickhouse.RebuildSilver` in between, as the seeded view tests do. Without
  it, `TestCollectorSilverViews` read 0 sessions.
- Reading the stored tables from something that must show an event at once. `cardo check` looks for
  the session started a minute ago, so it reads `silver_session_live`.
- Reading `estimated_cost.amount` as dollars. **It is cents**, on both analytics APIs. Silver divides
  once and names the column `cost_usd`; nothing downstream should divide again.
- `sum(x) AS x` in a ClickHouse view, or in a dashboard panel. Legal in DuckDB, an error here as
  soon as `x` is used again in the same query: a panel computing
  `sum(r) / nullIf(sum(sessions), 0)` beside `sum(sessions) AS sessions` fails with "aggregate
  function found inside another". Aggregate in a subquery with a `_total` suffix and rename
  outside, so the published names still match. Run a new panel's query through Grafana, not only
  through ClickHouse directly: the macros are Grafana's.
- Checking a view's rounded figure against `math.Round` in Go. ClickHouse's `round()` takes a
  float's half to the even neighbour, so 1,014.65 s of permission waits is 1014.6 in
  `gold_friction_daily`, and Go's `math.Round` makes it 1014.7. The made-up organization's check
  failed on 2 of 330 days for that alone. Use `math.RoundToEven`, or compare the unrounded column.
- Adding up `people` or `active_people` across weeks or days. They are distinct counts; the same
  engineer on five days is one person, not five. A panel over a range shows the busiest week or
  day. Sessions, uses and prompts add up.
- Sizing a cohort per day in a view that reports per week. `silver_session_cohort` counts a team's
  people per day, so at weekends teams seldom reached five and every team's line broke: on the
  made-up organization the data team, eight people, was named on 39 of 84 days and on no weekend.
  Count people over the view's own period
  ([ADR-0029](../../docs/adr/0029-minimum-group-size.md)): a weekly view reads
  `silver_session_cohort_week`. The same goes for a model, a release or a tool folded below five:
  count its people in the view's week.
- Joining two subqueries without an alias. ClickHouse refuses it (`ALIAS_REQUIRED`: "no alias for
  subquery"), and a panel such as the rollout table, which sets each artifact beside the fleet's
  total, needs `(...) AS u CROSS JOIN (...) AS f`.
- Giving an annotation its data source only beside its target. Grafana's query endpoint refuses a
  query with no data source in it ("Query does not contain a valid data source identifier"), and
  `TestMadeUpDashboardsAnswer` sends each target as the dashboard stores it. Put the `datasource`
  inside `target`, beside its `rawSql`, as a panel's targets carry theirs.
- Charting a new share or a figure per request and leaving `rateVolumes` in `test/deploy_test.go`
  alone. The test checks only the rates it lists, so a new name goes unchecked. Add its pair: a
  share of sessions with `sessions`, `cache_read_share` and anything `_per_request` with
  `requests`, an artifact's `people_share` with `active_people`.
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
- Matching a model switch's `to_model` to OTel's `hook_execution_start`, or to the next
  `api_request`'s `model`, as written. `to_model` may be dated while the OTel `hook_name` uses the
  canonical model name, so match them with the date and `[1m]` removed. `008` did that for the time
  and not for the cost, and a switch to a dated model showed no cost until `009`.
  `PostModelSwitch` also fires on fallback (`auto`) and resume, which the views leave out.
