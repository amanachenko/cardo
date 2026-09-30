-- 007 — gold for the collector path: the marts the enablement dashboard reads.
--
-- ClickHouse only (ADR-0028). INV-3 governs this layer as it governs 003_gold.sql: a pseudonym is
-- counted here and never selected, which a test enforces line by line. ADR-0029 adds the rule
-- this file exists to apply: a home-grown artifact name or a cohort label is shown only once at
-- least settings_effective.min_group_size people are in it (five unless raised), and is counted
-- as "other" below that. The organization's own names, Claude Code's built-in subagents and
-- commands, and fleet-wide totals are always shown.
--
-- A pseudonym that is NULL (a session that sent no OTel) is not a person to count(DISTINCT), so
-- it can never lift a label over the threshold.
--
-- Same dialect rule as 003: aggregate in a CTE under a `_total` name, rename outside.

-- The cohort each session is reported under: its cohort label if at least min_group_size people
-- had a session in that cohort that day, otherwise "other". A session with no cohort label is
-- "unassigned".
--
-- Session grain, and it lives beside silver_session for that reason: dashboards may not read it
-- (INV-3). The gold views below report a session's events on the day the session started.
CREATE OR REPLACE VIEW cardo.silver_session_cohort AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sizes AS
    (
        SELECT day, cohort, count(DISTINCT pseudonym) AS cohort_people
        FROM cardo.silver_session
        GROUP BY day, cohort
    )
SELECT
    s.session_id                                                                   AS session_id,
    s.day                                                                          AS day,
    if(z.cohort_people >= min_group, if(s.cohort = '', 'unassigned', s.cohort), 'other') AS cohort
FROM cardo.silver_session AS s
INNER JOIN sizes AS z ON z.day = s.day AND z.cohort = s.cohort;

-- Which artifacts are used, by how many people, each week. This is the question the product
-- exists to answer (ADR-0001): is what the platform team shipped being used, and what have
-- engineers built for themselves that several of them now use?
--
-- One row per week, kind and artifact. `artifact` is the name, or:
--   - "other" for a home-grown name fewer than min_group_size people used that week;
--   - empty for an instructions file that is not the organization's, which is counted by
--     file_kind and never named (ADR-0026).
--
-- people_share is the share of that week's active people (anyone with a session) who used it.
-- People cannot be added across weeks; sessions and uses can.
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
            count()                                                                AS uses_total
        FROM cardo.silver_artifact_load AS a
        INNER JOIN per_name AS n ON n.week = toMonday(a.day) AND n.kind = a.kind AND n.name = a.name
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
    f.active_people                                                AS active_people,
    round(u.people_total / nullIf(f.active_people, 0), 4)          AS people_share
FROM usage AS u
LEFT JOIN fleet AS f ON f.week = u.week
ORDER BY week ASC, kind ASC, people DESC;

-- Which version of each of the organization's instructions files people load (ADR-0026).
--
-- The organization puts a version in the names of what it ships (acme-security-v3.md); `family`
-- is the name without it. `current_version` is the highest version anyone loaded while the data
-- was retained. A version shipped and not yet loaded by anybody cannot be seen. A row that is not
-- current is people on a stale version. Someone who loaded two versions in a week counts under
-- both.
--
-- Only names matching CARDO_ORG_ARTIFACTS reach this view: the collector stores no other
-- instructions file's name. The group size does not apply (ADR-0029): the stragglers are the
-- point.
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
    latest AS
    (
        SELECT family, max(version) AS current_version
        FROM versions
        GROUP BY family
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
    v.loads_total                       AS loads
FROM versions AS v
INNER JOIN latest AS l ON l.family = v.family
ORDER BY week ASC, family ASC, version DESC;

-- The friction index's three components (ADR-0008), per day and cohort, each beside the volume
-- it is measured over. ADR-0008 makes that a product constraint: a rate with no denominator
-- rewards timidity, because a session that proposes nothing is never rejected.
--
--   edit_rejection_rate            rejected edits / edits proposed (Edit, MultiEdit, Write,
--                                  NotebookEdit), from OTel tool_decision
--   permission_wait_s_*            how long people took to answer permission prompts, both ends
--                                  on the laptop's clock (silver_policy_decision)
--   compactions_per_session        compactions / sessions
--
-- Events are reported on the day their session started. permission_prompts_two_clocks counts
-- waits that had to fall back to the collector's clock for their start; while it is 0, every
-- wait here is on one clock.
CREATE OR REPLACE VIEW cardo.gold_friction_daily AS
WITH
    sessions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count() AS sessions_total
        FROM cardo.silver_session AS s
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = s.session_id
        GROUP BY day, cohort
    ),
    turns AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               countIf(NOT t.is_builtin_command) AS prompts_total
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = t.session_id
        GROUP BY day, cohort
    ),
    edits AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               countIf(k.is_edit AND k.decision != '')        AS edit_proposals_total,
               countIf(k.is_edit AND k.decision = 'reject')   AS edit_rejections_total
        FROM cardo.silver_tool_call AS k
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = k.session_id
        GROUP BY day, cohort
    ),
    permissions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count()                                           AS prompts_total,
               countIf(p.answered)                               AS answered_total,
               countIf(p.clock = 'two_clocks')                   AS two_clocks_total,
               sumIf(p.wait_ms, p.answered)                      AS wait_ms_total,
               quantileExactIf(0.5)(p.wait_ms, p.answered)       AS wait_ms_p50,
               quantileExactIf(0.9)(p.wait_ms, p.answered)       AS wait_ms_p90
        FROM cardo.silver_policy_decision AS p
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = p.session_id
        GROUP BY day, cohort
    ),
    compactions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               countIf(e.kind = 'compaction')                                    AS compactions_total,
               countIf(e.kind = 'compaction' AND e.compaction_reason = 'auto')   AS auto_compactions_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = e.session_id
        GROUP BY day, cohort
    )
SELECT
    s.day                                                                  AS day,
    s.cohort                                                               AS cohort,
    s.people_total                                                         AS active_people,
    s.sessions_total                                                       AS sessions,
    t.prompts_total                                                        AS prompts,
    e.edit_proposals_total                                                 AS edit_proposals,
    e.edit_rejections_total                                                AS edit_rejections,
    round(e.edit_rejections_total / nullIf(e.edit_proposals_total, 0), 4)  AS edit_rejection_rate,
    p.prompts_total                                                        AS permission_prompts,
    p.answered_total                                                       AS permission_answered,
    p.two_clocks_total                                                     AS permission_prompts_two_clocks,
    round(p.wait_ms_total / 1000, 1)                                       AS permission_wait_s_total,
    round(p.wait_ms_p50 / 1000, 1)                                         AS permission_wait_s_p50,
    round(p.wait_ms_p90 / 1000, 1)                                         AS permission_wait_s_p90,
    round(p.prompts_total / nullIf(t.prompts_total, 0), 4)                 AS permission_prompts_per_prompt,
    m.compactions_total                                                    AS compactions,
    m.auto_compactions_total                                               AS auto_compactions,
    round(m.compactions_total / nullIf(s.sessions_total, 0), 4)            AS compactions_per_session
FROM sessions AS s
LEFT JOIN turns AS t ON t.day = s.day AND t.cohort = s.cohort
LEFT JOIN edits AS e ON e.day = s.day AND e.cohort = s.cohort
LEFT JOIN permissions AS p ON p.day = s.day AND p.cohort = s.cohort
LEFT JOIN compactions AS m ON m.day = s.day AND m.cohort = s.cohort
ORDER BY day ASC, cohort ASC;

-- Context and what it costs, per day and cohort.
--
--   session_start_context_p50   the context of each session's first main-thread request: Claude
--                               Code's fixed prefix (system prompt, tools, instructions files)
--                               plus the first prompt. What every session pays before any work.
--   turn_peak_context_*         per prompt, the largest context any of its main-thread requests
--                               sent: how close conversations get to the window. Claude Code
--                               does not report the window's size, so this is tokens, not a
--                               percentage.
--   switch_*                    model switches, and what they cost. The measured figures are the
--                               next main-thread request on the new model; the estimate is
--                               Claude Code's own, which assumes the whole context is rewritten
--                               and was 3.2 times the measured cost in the switch observed
--                               (ADR-0027). Chart the measured figures; show the estimate only
--                               beside them.
CREATE OR REPLACE VIEW cardo.gold_context_daily AS
WITH
    sessions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count() AS sessions_total
        FROM cardo.silver_session AS s
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = s.session_id
        GROUP BY day, cohort
    ),
    session_start AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count()                                  AS started_total,
               quantileExact(0.5)(f.start_context)      AS start_context_p50
        FROM
        (
            SELECT session_id, argMinIf(context_tokens, ts, is_main_thread) AS start_context
            FROM cardo.silver_api_request
            GROUP BY session_id
            HAVING countIf(is_main_thread) > 0
        ) AS f
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = f.session_id
        GROUP BY day, cohort
    ),
    turns AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               countIf(t.main_thread_requests > 0)                                   AS turns_total,
               quantileExactIf(0.5)(t.peak_context_tokens, t.main_thread_requests > 0) AS peak_p50,
               quantileExactIf(0.9)(t.peak_context_tokens, t.main_thread_requests > 0) AS peak_p90,
               max(t.peak_context_tokens)                                            AS peak_max
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = t.session_id
        GROUP BY day, cohort
    ),
    spend AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               sum(r.cost_usd)                          AS cost_usd_total,
               sumIf(r.cost_usd, r.is_main_thread)      AS main_thread_cost_usd_total
        FROM cardo.silver_api_request AS r
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = r.session_id
        GROUP BY day, cohort
    ),
    switches AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               countIf(e.kind = 'model_switch')                                              AS switches_total,
               countIf(e.kind = 'model_switch' AND e.next_request_cost_usd IS NOT NULL)      AS measured_total,
               countIf(e.kind = 'model_switch' AND e.estimated_cache_write_usd IS NOT NULL)  AS estimated_count_total,
               sumIf(e.next_request_cache_creation_tokens, e.kind = 'model_switch')          AS cache_write_tokens_total,
               sumIf(e.next_request_cost_usd, e.kind = 'model_switch')                       AS next_request_cost_total,
               sumIf(e.estimated_cache_write_usd, e.kind = 'model_switch')                   AS estimated_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = e.session_id
        GROUP BY day, cohort
    )
SELECT
    s.day                                             AS day,
    s.cohort                                          AS cohort,
    s.people_total                                    AS active_people,
    s.sessions_total                                  AS sessions,
    round(p.cost_usd_total, 4)                        AS cost_usd,
    round(p.main_thread_cost_usd_total, 4)            AS main_thread_cost_usd,
    b.started_total                                   AS sessions_with_requests,
    b.start_context_p50                               AS session_start_context_p50,
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
LEFT JOIN session_start AS b ON b.day = s.day AND b.cohort = s.cohort
LEFT JOIN turns AS t ON t.day = s.day AND t.cohort = s.cohort
LEFT JOIN spend AS p ON p.day = s.day AND p.cohort = s.cohort
LEFT JOIN switches AS w ON w.day = s.day AND w.cohort = s.cohort
ORDER BY day ASC, cohort ASC;
