-- 018 — the enablement dashboard's views, as its review on the made-up organization left them.
--
-- Numbered after stored silver (017): the views here read the stored silver tables by name, as
-- 007's do, and silver_session_cohort_week stays a plain view like silver_session_cohort.
--
-- ClickHouse only (ADR-0028). INV-3 and the five-person minimum (ADR-0029) govern every view here
-- as they govern 007: a pseudonym is counted, never selected, and a name or team label is shown
-- only once at least settings_effective.min_group_size people are in it.
--
-- What the review changed, and why:
--
--   - Teams are sized per week. 007 sizes a cohort per day, so on a weekend no team reaches five
--     people and every team's line breaks, and a small team is named only on its busier days. A
--     weekly view now counts its teams over the week, its own period (ADR-0029).
--   - "Stale" is judged against the newest version that existed that week. 007 compared every week
--     with the newest version ever loaded, so the weeks before v3 shipped read as all stale.
--   - Spend is shown by model and by team, and cost by Claude Code release, so that a rise in
--     spend arrives with its reason.
--   - Failing calls repeated in a row are counted: the cost of a flaky command was in silver and
--     no view read it.
--
-- 007 is immutable once shipped, so gold_artifact_usage and gold_instructions_versions are
-- redefined here whole; everything not named above is 007's. gold_friction_daily and
-- gold_context_daily are unchanged, and no panel reads them now.
--
-- Same dialect rule as 003 and 007: aggregate in a CTE under a `_total` name, rename outside.

-- The cohort each session is reported under in the weekly views: its cohort label if at least
-- min_group_size people had a session in that cohort that week, otherwise "other". A session
-- with no cohort label is "unassigned".
--
-- Session grain, beside silver_session_cohort for the same reason: dashboards may not read it
-- (INV-3). The weekly views report a session's events in the week the session started.
CREATE OR REPLACE VIEW cardo.silver_session_cohort_week AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sizes AS
    (
        SELECT toMonday(day) AS week, cohort, count(DISTINCT pseudonym) AS cohort_people
        FROM cardo.silver_session
        GROUP BY week, cohort
    )
SELECT
    s.session_id                                                                   AS session_id,
    toMonday(s.day)                                                                AS week,
    if(z.cohort_people >= min_group, if(s.cohort = '', 'unassigned', s.cohort), 'other') AS cohort
FROM cardo.silver_session AS s
INNER JOIN sizes AS z ON z.week = toMonday(s.day) AND z.cohort = s.cohort;

-- 007's view, with two columns more:
--
--   teams        how many team labels the people using it carry that week. A count, never which
--                teams: spread across teams is the vote for what teams chose (ADR-0040). Every
--                team counts, including one too small to be named.
--   first_week   the first week anyone used the name, while the data was retained, including
--                weeks it was still "other". Empty on the "other" row itself.
CREATE OR REPLACE VIEW cardo.gold_artifact_usage AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    fleet AS
    (
        SELECT toMonday(day) AS week, count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session
        GROUP BY week
    ),
    per_name AS
    (
        SELECT toMonday(day) AS week, kind, name, count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_artifact_load
        GROUP BY week, kind, name
    ),
    first_seen AS
    (
        SELECT kind, name, min(toMonday(day)) AS first_week
        FROM cardo.silver_artifact_load
        GROUP BY kind, name
    ),
    usage AS
    (
        SELECT
            toMonday(a.day)                                                        AS week,
            a.kind                                                                 AS kind,
            if(a.is_org OR a.is_builtin OR a.name = '' OR n.name_people >= min_group,
               a.name, 'other')                                                    AS artifact,
            a.file_kind                                                            AS file_kind,
            a.is_org                                                               AS org,
            a.is_builtin                                                           AS builtin,
            count(DISTINCT pseudonym)                                              AS people_total,
            count(DISTINCT a.session_id)                                           AS sessions_total,
            count()                                                                AS uses_total,
            uniqExactIf(a.cohort, a.cohort != '')                                  AS teams_total,
            min(f.first_week)                                                      AS first_week_total
        FROM cardo.silver_artifact_load AS a
        INNER JOIN per_name AS n ON n.week = toMonday(a.day) AND n.kind = a.kind AND n.name = a.name
        INNER JOIN first_seen AS f ON f.kind = a.kind AND f.name = a.name
        GROUP BY week, kind, artifact, file_kind, org, builtin
    )
SELECT
    u.week                                                         AS week,
    u.kind                                                         AS kind,
    u.artifact                                                     AS artifact,
    u.file_kind                                                    AS file_kind,
    u.org                                                          AS is_org,
    u.builtin                                                      AS is_builtin,
    u.people_total                                                 AS people,
    u.sessions_total                                               AS sessions,
    u.uses_total                                                   AS uses,
    u.teams_total                                                  AS teams,
    if(u.artifact = 'other', NULL, u.first_week_total)             AS first_week,
    f.active_people                                                AS active_people,
    round(u.people_total / nullIf(f.active_people, 0), 4)          AS people_share
FROM usage AS u
LEFT JOIN fleet AS f ON f.week = u.week
ORDER BY week ASC, kind ASC, people DESC;

-- 007's view, except for what "current" means. `current_version` is now the highest version
-- anyone had loaded by the end of that week, so a week before v3 shipped judges v2 current. A
-- version shipped and not yet loaded by anybody still cannot be seen. `first_week` is the first
-- week anyone loaded that file.
CREATE OR REPLACE VIEW cardo.gold_instructions_versions AS
WITH
    versions AS
    (
        SELECT
            toMonday(day)                                                          AS week,
            replaceRegexpOne(name, '-v[0-9]+([.][A-Za-z0-9]+)?$', '')              AS family,
            toUInt32OrZero(extract(name, '-v([0-9]+)(?:[.][A-Za-z0-9]+)?$'))       AS version,
            name                                                                   AS file_name,
            count(DISTINCT pseudonym)                                              AS people_total,
            count(DISTINCT session_id)                                             AS sessions_total,
            count()                                                                AS loads_total
        FROM cardo.silver_artifact_load
        WHERE kind = 'instructions' AND name != ''
        GROUP BY week, family, version, file_name
    ),
    weekly_top AS
    (
        SELECT week, family, max(version) AS top_version
        FROM versions
        GROUP BY week, family
    ),
    latest AS
    (
        SELECT week, family,
               max(top_version) OVER (PARTITION BY family ORDER BY week ASC
                                      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS current_version
        FROM weekly_top
    ),
    firsts AS
    (
        SELECT file_name, min(week) AS first_week
        FROM versions
        GROUP BY file_name
    )
SELECT
    v.week                              AS week,
    v.family                            AS family,
    v.file_name                         AS instructions_name,
    v.version                           AS version,
    v.version > 0                       AS is_versioned,
    l.current_version                   AS current_version,
    v.version = l.current_version       AS is_current,
    v.people_total                      AS people,
    v.sessions_total                    AS sessions,
    v.loads_total                       AS loads,
    f.first_week                        AS first_week
FROM versions AS v
INNER JOIN latest AS l ON l.family = v.family AND l.week = v.week
INNER JOIN firsts AS f ON f.file_name = v.file_name
ORDER BY week ASC, family ASC, version DESC;

-- Per week, team and instructions family: how many sessions loaded the family, and how many of
-- them loaded its current version: the highest anyone had loaded by the end of that week, as in
-- gold_instructions_versions. Version reach
-- by team is shown against the fleet, never ranked (ADR-0030); the teams' rows add up to the
-- fleet, because each session is in one team. A session that loaded two versions counts once in
-- `sessions`, and in `sessions_current` if either was current.
CREATE OR REPLACE VIEW cardo.gold_instructions_by_team_weekly AS
WITH
    loads AS
    (
        SELECT
            a.session_id                                                           AS session_id,
            c.week                                                                 AS week,
            c.cohort                                                               AS cohort,
            replaceRegexpOne(a.name, '-v[0-9]+([.][A-Za-z0-9]+)?$', '')            AS family,
            toUInt32OrZero(extract(a.name, '-v([0-9]+)(?:[.][A-Za-z0-9]+)?$'))     AS version
        FROM cardo.silver_artifact_load AS a
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = a.session_id
        WHERE a.kind = 'instructions' AND a.name != ''
    ),
    currents AS
    (
        SELECT week, family,
               max(top_version) OVER (PARTITION BY family ORDER BY week ASC
                                      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS current_version
        FROM (SELECT week, family, max(version) AS top_version FROM loads GROUP BY week, family)
    ),
    team AS
    (
        SELECT
            o.week                                                                 AS week,
            o.cohort                                                               AS cohort,
            o.family                                                               AS family,
            count(DISTINCT o.session_id)                                           AS sessions_total,
            uniqExactIf(o.session_id, o.version = k.current_version)               AS current_total
        FROM loads AS o
        INNER JOIN currents AS k ON k.week = o.week AND k.family = o.family
        GROUP BY week, cohort, family
    )
SELECT
    week,
    cohort,
    family,
    sessions_total                                         AS sessions,
    current_total                                          AS sessions_current,
    round(current_total / nullIf(sessions_total, 0), 4)    AS current_share
FROM team
ORDER BY week ASC, family ASC, cohort ASC;

-- Spend per week and team. Teams are sized per week, listed and never ranked (ADR-0030), and the
-- spend is never divided by headcount: a large team spends more, and that is all it says.
-- cost_usd is Claude Code's own per-request cost, main thread, subagents and helpers together.
CREATE OR REPLACE VIEW cardo.gold_spend_weekly AS
WITH
    spend AS
    (
        SELECT
            c.week                              AS week,
            c.cohort                            AS cohort,
            count(DISTINCT r.session_id)        AS sessions_total,
            count()                             AS requests_total,
            sum(r.cost_usd)                     AS cost_usd_total
        FROM cardo.silver_api_request AS r
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = r.session_id
        GROUP BY week, cohort
    )
SELECT
    week,
    cohort,
    sessions_total                  AS sessions,
    requests_total                  AS requests,
    round(cost_usd_total, 4)        AS cost_usd
FROM spend
ORDER BY week ASC, cohort ASC;

-- Spend per week and model, for the whole fleet. A model fewer than min_group_size people used
-- that week is "other models": what one person spent on a model is that person's spend.
CREATE OR REPLACE VIEW cardo.gold_model_spend_weekly AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    per_model AS
    (
        SELECT c.week AS week, r.model AS model, count(DISTINCT pseudonym) AS model_people
        FROM cardo.silver_api_request AS r
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = r.session_id
        GROUP BY week, model
    ),
    spend AS
    (
        SELECT
            c.week                                                                 AS week,
            if(m.model_people >= min_group, r.model, 'other models')               AS model,
            count(DISTINCT pseudonym)                                              AS people_total,
            count()                                                                AS requests_total,
            sum(r.cost_usd)                                                        AS cost_usd_total
        FROM cardo.silver_api_request AS r
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = r.session_id
        INNER JOIN per_model AS m ON m.week = c.week AND m.model = r.model
        GROUP BY week, model
    )
SELECT
    week,
    model,
    people_total                    AS people,
    requests_total                  AS requests,
    round(cost_usd_total, 4)        AS cost_usd
FROM spend
ORDER BY week ASC, cost_usd DESC;

-- How much of its context each Claude Code release reads from the cache, per week. A release that
-- stops reusing the cache writes it again on every request and costs more for the same work; the
-- operator's answer is the minimum version (INV-6: a minimum, never a maximum). Cost per request
-- alone cannot tell that apart from a change of model, so the cache figures lead.
--
--   cache_read_share          cache-read tokens / all input tokens (input + cache read + write)
--   cache_write_per_request   cache-write tokens per request
--
-- A release fewer than min_group_size people used that week is "other releases". The rates come
-- with their volume, requests (ADR-0008); a panel over several weeks recomputes them from the
-- token sums.
CREATE OR REPLACE VIEW cardo.gold_release_cache_weekly AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    per_release AS
    (
        SELECT s.week AS week, s.app_version AS app_version, count(DISTINCT pseudonym) AS release_people
        FROM cardo.silver_api_request AS r
        INNER JOIN (SELECT session_id, toMonday(day) AS week, app_version FROM cardo.silver_session) AS s
            ON s.session_id = r.session_id
        GROUP BY week, app_version
    ),
    cache AS
    (
        SELECT
            s.week                                                                 AS week,
            if(p.release_people >= min_group, s.app_version, 'other releases')     AS app_version,
            count(DISTINCT pseudonym)                                              AS people_total,
            count(DISTINCT r.session_id)                                           AS sessions_total,
            count()                                                                AS requests_total,
            sum(r.input_tokens)                                                    AS input_total,
            sum(r.cache_read_tokens)                                               AS cache_read_total,
            sum(r.cache_creation_tokens)                                           AS cache_write_total,
            sum(r.cost_usd)                                                        AS cost_usd_total
        FROM cardo.silver_api_request AS r
        INNER JOIN (SELECT session_id, toMonday(day) AS week, app_version FROM cardo.silver_session) AS s
            ON s.session_id = r.session_id
        INNER JOIN per_release AS p ON p.week = s.week AND p.app_version = s.app_version
        GROUP BY week, app_version
    )
SELECT
    week,
    app_version,
    people_total                                                                       AS people,
    sessions_total                                                                     AS sessions,
    requests_total                                                                     AS requests,
    input_total                                                                        AS input_tokens,
    cache_read_total                                                                   AS cache_read_tokens,
    cache_write_total                                                                  AS cache_write_tokens,
    round(cost_usd_total, 4)                                                           AS cost_usd,
    round(cache_read_total / nullIf(input_total + cache_read_total + cache_write_total, 0), 4) AS cache_read_share,
    round(cache_write_total / nullIf(requests_total, 0))                               AS cache_write_per_request,
    round(cost_usd_total / nullIf(requests_total, 0), 4)                               AS cost_per_request
FROM cache
ORDER BY week ASC, app_version ASC;

-- The context every session starts with, for the whole fleet, per week: Claude Code's fixed
-- prefix (system prompt, connectors' tools, instructions files) plus the first prompt, which every
-- request of the session pays again from the cache. Its steps are what a rollout added to every
-- session. Tokens, not dollars: what a token costs depends on the model and on the cache.
CREATE OR REPLACE VIEW cardo.gold_setup_context_weekly AS
WITH
    starts AS
    (
        SELECT session_id, argMinIf(context_tokens, ts, is_main_thread) AS start_context
        FROM cardo.silver_api_request
        GROUP BY session_id
        HAVING countIf(is_main_thread) > 0
    ),
    weekly AS
    (
        SELECT
            s.week                                        AS week,
            count()                                       AS started_total,
            quantileExact(0.5)(f.start_context)           AS start_p50,
            quantileExact(0.9)(f.start_context)           AS start_p90
        FROM starts AS f
        INNER JOIN (SELECT session_id, toMonday(day) AS week FROM cardo.silver_session) AS s
            ON s.session_id = f.session_id
        GROUP BY week
    )
SELECT
    week,
    started_total       AS sessions_with_requests,
    start_p50           AS session_start_context_p50,
    start_p90           AS session_start_context_p90
FROM weekly
ORDER BY week ASC;

-- gold_context_daily's prompt and model-switch columns, per week and team, with teams sized per
-- week. The fleet's starting context is gold_setup_context_weekly, and spend gold_spend_weekly.
CREATE OR REPLACE VIEW cardo.gold_context_weekly AS
WITH
    sessions AS
    (
        SELECT c.week AS week, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count() AS sessions_total
        FROM cardo.silver_session AS s
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = s.session_id
        GROUP BY week, cohort
    ),
    turns AS
    (
        SELECT c.week AS week, c.cohort AS cohort,
               countIf(t.main_thread_requests > 0)                                   AS turns_total,
               quantileExactIf(0.5)(t.peak_context_tokens, t.main_thread_requests > 0) AS peak_p50,
               quantileExactIf(0.9)(t.peak_context_tokens, t.main_thread_requests > 0) AS peak_p90,
               max(t.peak_context_tokens)                                            AS peak_max
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = t.session_id
        GROUP BY week, cohort
    ),
    switches AS
    (
        SELECT c.week AS week, c.cohort AS cohort,
               countIf(e.kind = 'model_switch')                                              AS switches_total,
               countIf(e.kind = 'model_switch' AND e.next_request_cost_usd IS NOT NULL)      AS measured_total,
               countIf(e.kind = 'model_switch' AND e.estimated_cache_write_usd IS NOT NULL)  AS estimated_count_total,
               sumIf(e.next_request_cache_creation_tokens, e.kind = 'model_switch')          AS cache_write_tokens_total,
               sumIf(e.next_request_cost_usd, e.kind = 'model_switch')                       AS next_request_cost_total,
               sumIf(e.estimated_cache_write_usd, e.kind = 'model_switch')                   AS estimated_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = e.session_id
        GROUP BY week, cohort
    )
SELECT
    s.week                                            AS week,
    s.cohort                                          AS cohort,
    s.people_total                                    AS active_people,
    s.sessions_total                                  AS sessions,
    t.turns_total                                     AS turns,
    t.peak_p50                                        AS turn_peak_context_p50,
    t.peak_p90                                        AS turn_peak_context_p90,
    t.peak_max                                        AS turn_peak_context_max,
    w.switches_total                                  AS model_switches,
    w.measured_total                                  AS model_switches_measured,
    w.cache_write_tokens_total                        AS switch_cache_write_tokens,
    round(w.next_request_cost_total, 4)               AS switch_next_request_cost_usd,
    w.estimated_count_total                           AS model_switches_estimated,
    round(w.estimated_total, 4)                       AS switch_estimated_cache_write_usd
FROM sessions AS s
LEFT JOIN turns AS t ON t.week = s.week AND t.cohort = s.cohort
LEFT JOIN switches AS w ON w.week = s.week AND w.cohort = s.cohort
ORDER BY week ASC, cohort ASC;

-- Failing calls repeated in a row, per week and tool: a command that fails, is run again, and
-- fails again, each time with the model reading the failure. Claude Code does not send the
-- command (INV-5), so the tool is as close as the view gets; the repository waits until real
-- sessions have confirmed how it arrives (ADR-0035).
--
--   loops            runs of three or more failed calls of one tool, one after another in a
--                    session, with no other call that ran in between. Two in a row can be bad
--                    luck; three rarely are.
--   failed_in_loops  the failed calls inside those runs
--   calls            every call of the tool that ran that week: the volume the loops come from
--
-- Calls are ordered by when Claude Code decided to run them. An MCP tool fewer than
-- min_group_size people called that week is "other tools"; Claude Code's own tools are always
-- named.
CREATE OR REPLACE VIEW cardo.gold_tool_loops_weekly AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    calls AS
    (
        SELECT k.session_id AS session_id, k.tool_use_id AS tool_use_id, k.tool_name AS tool_name,
               k.decided_at AS decided_at, toUInt8(NOT k.succeeded) AS failed, c.week AS week
        FROM cardo.silver_tool_call AS k
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = k.session_id
        WHERE k.ran
    ),
    per_tool AS
    (
        SELECT c.week AS week, k.tool_name AS tool_name, count(DISTINCT pseudonym) AS tool_people
        FROM cardo.silver_tool_call AS k
        INNER JOIN cardo.silver_session_cohort_week AS c ON c.session_id = k.session_id
        WHERE k.ran
        GROUP BY week, tool_name
    ),
    ordered AS
    (
        SELECT session_id, tool_use_id, tool_name, decided_at, failed, week,
               lagInFrame(tool_name, 1, '') OVER w      AS prev_tool,
               lagInFrame(failed, 1, 0) OVER w          AS prev_failed
        FROM calls
        WINDOW w AS (PARTITION BY session_id ORDER BY decided_at ASC, tool_use_id ASC
                     ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
    ),
    runs AS
    (
        SELECT session_id, tool_name, failed, week,
               sum(toUInt8(NOT (failed = 1 AND prev_failed = 1 AND prev_tool = tool_name))) OVER w AS run
        FROM ordered
        WINDOW w AS (PARTITION BY session_id ORDER BY decided_at ASC, tool_use_id ASC
                     ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
    ),
    loops AS
    (
        SELECT session_id, run, any(week) AS week, any(tool_name) AS tool_name, count() AS fails
        FROM runs
        WHERE failed = 1
        GROUP BY session_id, run
        HAVING fails >= 3
    ),
    named AS
    (
        SELECT week, tool_name,
               if(startsWith(tool_name, 'mcp__') AND tool_people < min_group, 'other tools', tool_name) AS tool
        FROM per_tool
    ),
    volume AS
    (
        SELECT c.week AS week, n.tool AS tool, count() AS calls_total
        FROM calls AS c
        INNER JOIN named AS n ON n.week = c.week AND n.tool_name = c.tool_name
        GROUP BY week, tool
    ),
    found AS
    (
        SELECT l.week AS week, n.tool AS tool,
               count()                          AS loops_total,
               sum(l.fails)                     AS failed_total,
               uniqExact(l.session_id)          AS sessions_total
        FROM loops AS l
        INNER JOIN named AS n ON n.week = l.week AND n.tool_name = l.tool_name
        GROUP BY week, tool
    )
SELECT
    v.week                          AS week,
    v.tool                          AS tool_name,
    v.calls_total                   AS calls,
    f.loops_total                   AS loops,
    f.failed_total                  AS failed_in_loops,
    f.sessions_total                AS sessions_with_loops
FROM volume AS v
LEFT JOIN found AS f ON f.week = v.week AND f.tool = v.tool
ORDER BY week ASC, loops DESC;
