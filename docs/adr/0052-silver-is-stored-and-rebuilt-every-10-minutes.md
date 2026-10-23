# ADR-0052 — The collector path's silver views are stored as tables ClickHouse rebuilds every 10 minutes

**Status:** Proposed
**Date:** 2026-10-23
**Evidence:** Measured on the made-up organization ([ADR-0051](0051-a-made-up-organization.md)) on
2026-10-23: 60 people over 12 weeks, on ClickHouse 26.6.8, with the seven views stored by hand.
These are timings of Cardo's own queries on invented data, not findings about how anyone works.
The figures for 500 people are estimates scaled from those, not measurements. The version that
was measured differed from decisions 1 and 3 in two small ways: ClickHouse named the stored
tables itself, and `is_org` was stored. Neither should change the timings.

Extends [the layered schema (ADR-0010)](0010-layered-schema.md) and
[plain SQL files (ADR-0019)](0019-sql-tooling.md) without replacing either. It is the mitigation
[risks.md](../../risks.md) #14 called for.

## Context

The silver and gold views over the collector's data are plain views. Nothing is stored, so every
panel that reads them works its answer out again from the raw (bronze) tables. Three things make
that slow:

- **The views nest.** `silver_session` groups every raw log, metric and hook row by session. Four
  other silver views join it. `silver_session_cohort` reads it twice, and the two daily gold views
  join `silver_session_cohort` ten times between them. One panel ends up reading about 100 million
  rows from a log table of 5.5 million.
- **Narrowing the dates does not help.** A session's day is the day of its first event, so
  ClickHouse has to group every row before it knows which sessions fall in the last 30 days. On
  the made-up organization, a 30-day range took as long as the whole history.
- **The work multiplies:** people, times days kept, times panels, times each time someone opens a
  dashboard.

On the made-up organization at 60 people, the enablement dashboard took about 85 seconds to draw.
Three of its panels hit Grafana's 60-second limit and showed nothing.

ClickHouse has a feature for exactly this, the **refreshable materialized view**: a query that
ClickHouse runs on a schedule, writing the whole result into a table and swapping it in at once.

## Decision

**1. Seven silver views become stored tables, rebuilt every 10 minutes.** They are the ones with a
row per session or per event:
- `silver_session`;
- `silver_api_request`;
- `silver_turn`;
- `silver_tool_call`;
- `silver_policy_decision`;
- `silver_context_event`;
- `silver_artifact_load`.

How they are built:
- **Each stored table has a `silver_` name of our choosing.** For example, `silver_session` is a
  table, and a refreshable view named `silver_session_refresh` writes into it (`TO
  cardo.silver_session`). Gold goes on reading `silver_session` by name. A stored table never
  goes by the name ClickHouse would pick (`.inner_id.` and a number), because the dashboard check
  that no panel reads silver looks for `silver_` and would not know it.
- **Each rebuild recomputes the whole table from the raw tables** and swaps the result in at once.
  A dashboard never sees half a table. A rebuild that fails leaves the last good table in place.
- **The query is the view's own query, unchanged.** Only its wrapper changes.
- **Why 10 minutes:** a weekly adoption chart does not notice, and "did my rollout land?" waits a
  few minutes. At 60 people a round of rebuilds takes 4 to 10 seconds of each 10 minutes.

**2. A stored table that reads another waits for it.**
- `silver_turn` and `silver_context_event` read `silver_session` and `silver_api_request`.
- `silver_policy_decision` and `silver_artifact_load` read `silver_session`.

Their refreshers name those in `DEPENDS ON`, so each rebuild starts after its sources' rebuild of
the same time. All seven share the one 10-minute schedule. The first try had no `DEPENDS ON`. It
read `silver_session` while that was still empty, so no artifact row had a pseudonym, and the
artifact views counted 0 people everywhere, with no error.

**3. What the organization's settings decide is worked out at read time, never stored.**
- **The five-person minimum** ([ADR-0029](0029-minimum-group-size.md)). `silver_session_cohort`
  and every gold view stay plain views, so raising the minimum takes effect on the next read, not
  up to 10 minutes later.
- **Which names are the organization's own** (`CARDO_ORG_ARTIFACTS`,
  [ADR-0025](0025-artifact-names-kept-with-guardrails.md)). Gold shows such a name however few
  people use it. So `is_org` is not stored: the stored table is `silver_artifact_load_stored`, and
  `silver_artifact_load` stays a plain view over it that adds `is_org`. Suppose `is_org` were
  stored, and the organization narrowed its pattern because the pattern caught a name only one
  person uses. That name would stay on the dashboards for up to 10 minutes, unhidden.

**4. Stored rows expire with the raw rows.**
- **While rebuilds succeed,** the stored tables hold nothing the raw tables do not. A raw row that
  expires after 90 days ([ADR-0016](0016-retention.md)), or is deleted, leaves silver at the next
  rebuild.
- **If rebuilds stop,** the last good table stays. So each stored table also carries the raw
  tables' expiry, and whatever changes the raw tables' expiry changes theirs. The expiry was
  checked to survive a rebuild.

**5. Anything that reads silver straight after writing rows forces a rebuild first.** It runs
`SYSTEM REFRESH VIEW` on each refresher, then `SYSTEM WAIT VIEW`, and only then reads. That
covers:
- the ClickHouse tests that insert rows;
- the made-up organization's check;
- the preview's comparison;
- any check that a session has just arrived. Such a check can read the raw tables instead.

**6. A rebuild that keeps failing shows on the dashboards.** ClickHouse records every rebuild in
`system.view_refreshes`. A gold view reads the time of the oldest last good rebuild there, and each
dashboard shows it, for example "data as of 10:20". A table that stopped rebuilding then reads as
old, rather than as a quiet week.

## Consequences

- **The dashboards draw in seconds.** On the made-up organization the enablement dashboard took
  1.7 seconds as opened, and 1.4 seconds over the whole history, down from 85. Its slowest panel
  took 1.5 seconds, and the fleet dashboard's panels under 0.2 seconds each. A panel now reads 4
  to 7 million rows, not about 100 million. Every gold view matched the answer key.
- **The dashboards run up to about 10 minutes behind the raw tables.** For example, a command
  first used at 10:00 shows from about 10:10.
- **ClickHouse works in the background, whether anyone looks or not.**
  - **At 60 people:** a round of all seven rebuilds took 4 to 10 seconds. The heaviest,
    `silver_tool_call` (1.7 million rows from 5.5 million raw ones), took about 5 seconds and up to
    2 GiB of memory.
  - **At 500 people, an estimate:** about eight times the rows, so a round of about a minute in
    every 10. The heaviest rebuild would want about 15 GiB, more than the 12 GiB or so ClickHouse
    was allowed in the measurement. ClickHouse then moves the grouping to disk, as it does by
    default past half the memory it may use, and the rebuild runs slower.
  - **That need is not new.** The plain views run the same grouping on every panel that reads
    them. Storing silver meets it once every 10 minutes instead of on every read.
- **Stored silver takes about as much disk as the raw tables.** At 60 people it was 320 MiB,
  beside 364 MiB of raw rows, so roughly 2.5 GiB at 500 (an estimate). A sort order on the stored
  tables may shrink that, but it was not measured.
- **Changing a silver view is still one migration.** The migration replaces the refresher's query,
  and the next rebuild recomputes the whole table from the raw tables, with no backfill code. That
  is why [ADR-0019](0019-sql-tooling.md)'s reason against materialized views does not apply here.
  It rejected the kind that runs on each insert, which never rewrites the rows it already wrote
  when its definition changes.
- **It keeps no trend past 90 days** ([risks.md](../../risks.md) #10). Silver holds only what the
  raw tables hold. It does give the collector path the scheduler it lacked: a later cohort-only
  rollup could use the same kind of view, adding each day and keeping it.
- **Rebuilding everything does not scale forever.** At a few thousand people a rebuild could take
  most of the 10 minutes. The fix then is to rebuild only the last few days and keep older ones.
  That needs a superseding record, because silver would then need its own expiry and deletion:
  decision 4 holds only because every rebuild starts from the raw tables.
- **The ClickHouse must have refreshable materialized views.** The reference stack pins 26.6,
  where they are on by default. An organization that runs its own ClickHouse needs a version
  where they are on too, and the migration's database user needs to create them.
  `docs/design/architecture.md` and the stack's README say so when this ships.
- **Who can read what does not change.** The stored tables sit in the same database under `silver_`
  names, so the dashboard check still refuses a panel that reads them. Any Grafana login still
  reads whatever the data source's database user can ([risks.md](../../risks.md) #15).
- **The views stay ClickHouse-only,** as the collector path already is
  ([ADR-0028](0028-file-store-is-admin-api-only.md)).

## Rejected alternatives and why

- **Pushing the date range down into the raw tables, so a 30-day panel reads 30 days.** At best a
  30-day panel reads 30 of the 84 days, so 85 seconds becomes about 30. The whole-history range
  stays slow, and the repeated reads stay.
- **ClickHouse's classic materialized views, which run on each insert.** They are always fresh,
  but a session's rows arrive over hours, across three tables, and each insert sees only its own
  batch. So:
  - every view would have to store partial results and merge them later;
  - a deleted raw row would stay counted;
  - a changed definition would need a hand-written backfill (ADR-0019's reason).

  The views would stop being the plain SQL they are now.
- **A scheduled `cardo` command that rewrites each day, as the Admin API poller does.** This is
  the daily table risk #14 first sketched. It gives the most control, but nothing on the collector
  path runs on a schedule today, so it is a new long-running process. Silver would also keep days
  the raw tables have lost, and would need its own expiry and deletion.
- **Rebuilding only the last few days.** It is cheaper at scale, but it loses the same two things
  as the daily command: silver no longer holds only what the raw tables hold, and every view change
  needs the old days rebuilt by hand. It is the later step if full rebuilds get too slow (see
  Consequences).
- **A bigger machine, or ClickHouse tuning.** The work grows with people, history, panels and
  opens, so this moves the wall without removing it.
- **Grafana's query cache.** It is in Grafana's paid editions only, not the open-source one the
  stack ships, and the first open would still be slow.
- **Storing gold instead of silver.** Gold is what the review of the dashboards will change most,
  and every chart change would become a migration and a rebuild. Silver is both the stable part
  and the expensive part.
- **Storing `silver_session_cohort`, and `is_org`, as well.** A setting change, such as a raised
  minimum or a narrowed pattern, would then wait up to 10 minutes, and a name the minimum should
  hide would show for that long.
- **Rebuilding every minute, or every day.** Every minute: at 500 people a rebuild could take most
  of the minute. Every day: "did my rollout land?" would wait until tomorrow. Ten minutes sits
  between the two, and changing it is one migration once the 500-person size is measured.
- **Letting ClickHouse name the stored tables,** as the first try did. It works, but the
  dashboard check looks for `silver_`, and a panel could read `.inner_id.` and a number without
  it noticing.
