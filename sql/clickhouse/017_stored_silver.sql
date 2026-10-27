-- 017 — the seven session-level silver views are stored, and rebuilt every 10 minutes (ADR-0052).
--
-- Until now every silver view was a plain view, so each read of a gold view worked silver out
-- again from the raw tables. On the made-up organization (60 people over 12 weeks) one enablement
-- panel read about 100 million rows from a log table of 5.5 million, and six of the dashboard's
-- 13 panels reached Grafana's 60-second limit (risks.md #14).
--
-- Each of the seven now comes in three parts. For silver_session:
--
--   silver_session_live      the plain view, as it was. It reads the raw tables and other _live
--                            views, nothing else, so it is up to the second.
--   silver_session           a table: silver_session_live's rows as of the last rebuild. Gold
--                            reads it by this name, as it read the view.
--   silver_session_refresh   a refreshable view. Every 10 minutes it rebuilds silver_session from
--                            silver_session_live and swaps the new rows in at once, so a reader
--                            never sees half a table. A rebuild that fails leaves the last good
--                            table in place.
--
-- silver_artifact_load is the exception. Its table is silver_artifact_load_stored, and
-- silver_artifact_load stays a plain view over it that adds is_org when it is read. Narrowing
-- CARDO_ORG_ARTIFACTS therefore hides a name on the next read, not up to 10 minutes later
-- (ADR-0052, decision 3).
--
-- NO STORED TABLE READS ANOTHER. silver_turn_live reads silver_session_live, not the stored
-- silver_session, so every rebuild starts from the raw tables and the seven need no order. The
-- first prototype rebuilt the artifact table from the stored silver_session while that was still
-- empty: no row had a pseudonym, and every artifact counted 0 people, with no error. The price is
-- that each rebuild of a table that reads silver_session works it out again. On the made-up
-- organization a round of all seven took about 13 seconds in each 10 minutes.
--
-- WHO READS WHICH. Gold and silver_session_cohort read the stored tables, up to 10 minutes behind:
-- a command first used at 10:00 reaches the dashboards by about 10:10. What must show an event at
-- once reads the _live views: cardo check's read-back of a session just started, the console, and
-- the coach. Those read one person's rows, or a few counts, so reading live costs little there. A
-- test or check that inserts rows and then reads gold rebuilds first: SYSTEM REFRESH VIEW on each
-- refresher, then SYSTEM WAIT VIEW.
--
-- EXPIRY. While rebuilds succeed, a raw row that expires (ADR-0016) leaves silver at the next
-- rebuild. In case rebuilds stop, each table also expires its rows 90 days after their day, as the
-- raw tables do, and retention moves that expiry with theirs (internal/retention).
--
-- RE-RUNNABLE. The runner applies a migration again if a statement fails partway, so nothing here
-- assumes a first run. The _live views are created or replaced, and each table and refresher is
-- dropped if it exists and created again. Dropping a stored table loses nothing: it holds only
-- what the raw tables hold, and the next rebuild fills it.
--
-- The bodies below are the current definitions, unchanged but for reading the _live views.
-- silver_session is 006's, with 012's service-run columns and 013's probe filter, written out over
-- lines. silver_context_event is 009's, and the rest are 006's. Their comments there still hold.

-- 1. The plain views under their _live names, each after the views it reads.

CREATE OR REPLACE VIEW cardo.silver_session_live AS
SELECT
    session_id,
    nullIf(anyIf(e_pseudonym, e_pseudonym != ''), '')                          AS pseudonym,
    anyIf(e_salt_version, e_salt_version != '' AND e_clock != 'collector')     AS salt_version,
    anyIf(e_cohort, e_cohort != '')                                            AS cohort,
    anyIf(e_app_version, e_app_version != '')                                  AS app_version,
    anyIf(e_terminal_type, e_terminal_type != '')                              AS terminal_type,
    anyIf(e_start_type, e_start_type != '')                                    AS start_type,
    multiIf(countIf(e_clock = 'client') > 0, minIf(e_ts, e_clock = 'client'),
            countIf(e_clock = 'collector') > 0, minIf(e_ts, e_clock = 'collector'),
            min(e_ts))                                                         AS started_at,
    multiIf(countIf(e_clock = 'client') > 0, maxIf(e_ts, e_clock = 'client'),
            countIf(e_clock = 'collector') > 0, maxIf(e_ts, e_clock = 'collector'),
            max(e_ts))                                                         AS last_event_at,
    toDate(started_at)                                                         AS day,
    if(countIf(e_clock = 'client') > 0, 'client', 'collector')                 AS clock,
    countIf(e_event = 'SessionEnd') > 0                                        AS ended,
    anyIf(e_end_reason, e_end_reason != '')                                    AS end_reason,
    countIf(e_clock = 'client') > 0                                            AS has_otel,
    countIf(e_clock = 'collector') > 0                                         AS has_hooks,
    if(anyIf(svc_kind, svc_kind != '') = 'service', 'service', 'person')       AS actor_kind,
    anyIf(svc_workflow, svc_workflow != '')                                    AS workflow
FROM
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym'])                               AS e_pseudonym,
        ResourceAttributes['cardo.salt_version']                               AS e_salt_version,
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           LogAttributes['cardo.cohort'])                                      AS e_cohort,
        LogAttributes['app.version']                                           AS e_app_version,
        LogAttributes['terminal.type']                                         AS e_terminal_type,
        ''                                                                     AS e_start_type,
        Timestamp                                                              AS e_ts,
        'client'                                                               AS e_clock,
        EventName                                                              AS e_event,
        ''                                                                     AS e_end_reason
    FROM cardo.bronze_otel_logs
    WHERE LogAttributes['session.id'] != ''

    UNION ALL

    SELECT
        Attributes['session.id'],
        if(Attributes['user.pseudonym'] != '', Attributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym']),
        ResourceAttributes['cardo.salt_version'],
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           Attributes['cardo.cohort']),
        Attributes['app.version'],
        Attributes['terminal.type'],
        Attributes['start_type'],
        toDateTime64(TimeUnix, 9),
        'metric',
        MetricName,
        ''
    FROM cardo.bronze_otel_metrics_sum
    WHERE MetricName = 'claude_code.session.count' AND Attributes['session.id'] != ''

    UNION ALL

    SELECT
        LogAttributes['session_id'],
        '',
        ResourceAttributes['cardo.salt_version'],
        '',
        '',
        '',
        '',
        Timestamp,
        'collector',
        EventName,
        LogAttributes['session_end_reason']
    FROM cardo.bronze_hook_events
    WHERE LogAttributes['session_id'] != ''
) AS b
LEFT JOIN
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        'service'                                                              AS svc_kind,
        any(ResourceAttributes['cardo.workflow'])                              AS svc_workflow
    FROM cardo.bronze_otel_logs
    WHERE ResourceAttributes['cardo.actor'] = 'service' AND LogAttributes['session.id'] != ''
    GROUP BY session_id
) AS svc USING (session_id)
WHERE session_id NOT LIKE 'cardo-probe-%'
GROUP BY session_id;

CREATE OR REPLACE VIEW cardo.silver_api_request_live AS
SELECT
    LogAttributes['session.id']                                                AS session_id,
    LogAttributes['prompt.id']                                                 AS prompt_id,
    nullIf(if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
              ResourceAttributes['user.pseudonym']), '')                       AS pseudonym,
    if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
       LogAttributes['cardo.cohort'])                                          AS cohort,
    Timestamp                                                                  AS ts,
    toDate(Timestamp)                                                          AS day,
    LogAttributes['model']                                                     AS model,
    LogAttributes['query_source']                                              AS query_source,
    LogAttributes['query_source'] = 'repl_main_thread'                         AS is_main_thread,
    LogAttributes['effort']                                                    AS effort,
    LogAttributes['skill.name']                                                AS skill_name,
    LogAttributes['agent.name']                                                AS agent_name,
    toUInt64OrZero(LogAttributes['input_tokens'])                              AS input_tokens,
    toUInt64OrZero(LogAttributes['output_tokens'])                             AS output_tokens,
    toUInt64OrZero(LogAttributes['cache_read_tokens'])                         AS cache_read_tokens,
    toUInt64OrZero(LogAttributes['cache_creation_tokens'])                     AS cache_creation_tokens,
    input_tokens + cache_read_tokens + cache_creation_tokens                   AS context_tokens,
    toFloat64OrZero(LogAttributes['cost_usd'])                                 AS cost_usd,
    toUInt64OrZero(LogAttributes['duration_ms'])                               AS duration_ms
FROM cardo.bronze_otel_logs
WHERE EventName = 'claude_code.api_request';

CREATE OR REPLACE VIEW cardo.silver_tool_call_live AS
SELECT
    session_id,
    tool_use_id,
    anyIf(e_prompt_id, e_prompt_id != '')                                      AS prompt_id,
    nullIf(anyIf(e_pseudonym, e_pseudonym != ''), '')                          AS pseudonym,
    anyIf(e_cohort, e_cohort != '')                                            AS cohort,
    anyIf(e_tool_name, e_tool_name != '')                                      AS tool_name,
    minIf(e_ts, e_event = 'claude_code.tool_decision')                         AS decided_at,
    toDate(min(e_ts))                                                          AS day,
    anyIf(e_decision, e_event = 'claude_code.tool_decision')                   AS decision,
    anyIf(e_decision_source, e_event = 'claude_code.tool_decision')            AS decision_source,
    countIf(e_event = 'claude_code.tool_result') > 0                           AS ran,
    anyIf(e_success, e_event = 'claude_code.tool_result') = 'true'             AS succeeded,
    maxIf(e_duration_ms, e_event = 'claude_code.tool_result')                  AS duration_ms,
    tool_name IN ('Edit', 'MultiEdit', 'Write', 'NotebookEdit')                AS is_edit,
    if(startsWith(tool_name, 'mcp__'), splitByString('__', tool_name)[2], '')  AS mcp_server
FROM
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['tool_use_id']                                           AS tool_use_id,
        LogAttributes['prompt.id']                                             AS e_prompt_id,
        if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym'])                               AS e_pseudonym,
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           LogAttributes['cardo.cohort'])                                      AS e_cohort,
        LogAttributes['tool_name']                                             AS e_tool_name,
        Timestamp                                                              AS e_ts,
        EventName                                                              AS e_event,
        LogAttributes['decision']                                              AS e_decision,
        LogAttributes['source']                                                AS e_decision_source,
        LogAttributes['success']                                               AS e_success,
        toUInt64OrZero(LogAttributes['duration_ms'])                           AS e_duration_ms
    FROM cardo.bronze_otel_logs
    WHERE EventName IN ('claude_code.tool_decision', 'claude_code.tool_result')
      AND LogAttributes['session.id'] != ''
      AND LogAttributes['tool_use_id'] != ''
)
GROUP BY session_id, tool_use_id;

CREATE OR REPLACE VIEW cardo.silver_turn_live AS
WITH
prompts AS
(
    SELECT
        session_id,
        prompt_id,
        multiIf(countIf(e_clock = 'client') > 0, minIf(e_ts, e_clock = 'client'), min(e_ts)) AS started_at,
        if(countIf(e_clock = 'client') > 0, 'client', 'collector')                          AS clock,
        max(e_prompt_length)                                                                AS prompt_length,
        anyIf(e_permission_mode, e_permission_mode != '')                                   AS permission_mode,
        anyIf(e_command_name, e_command_name != '')                                         AS command_name,
        anyIf(e_command_source, e_command_source != '')                                     AS command_source
    FROM
    (
        SELECT
            LogAttributes['session.id']                        AS session_id,
            LogAttributes['prompt.id']                         AS prompt_id,
            Timestamp                                          AS e_ts,
            'client'                                           AS e_clock,
            toUInt64OrZero(LogAttributes['prompt_length'])     AS e_prompt_length,
            ''                                                 AS e_permission_mode,
            LogAttributes['command_name']                      AS e_command_name,
            LogAttributes['command_source']                    AS e_command_source
        FROM cardo.bronze_otel_logs
        WHERE EventName = 'claude_code.user_prompt'

        UNION ALL

        SELECT
            LogAttributes['session_id'],
            LogAttributes['prompt_id'],
            Timestamp,
            'collector',
            toUInt64OrZero(LogAttributes['prompt_length']),
            LogAttributes['permission_mode'],
            '',
            ''
        FROM cardo.bronze_hook_events
        WHERE EventName = 'UserPromptSubmit'
    )
    WHERE session_id != '' AND prompt_id != ''
    GROUP BY session_id, prompt_id
),
requests AS
(
    SELECT
        session_id,
        prompt_id,
        count()                                          AS api_requests,
        countIf(is_main_thread)                          AS main_thread_requests,
        argMinIf(context_tokens, ts, is_main_thread)     AS first_context_tokens,
        maxIf(context_tokens, is_main_thread)            AS peak_context_tokens,
        sum(output_tokens)                               AS output_tokens_total,
        sum(cost_usd)                                    AS cost_usd_total
    FROM cardo.silver_api_request_live
    GROUP BY session_id, prompt_id
)
SELECT
    p.session_id                          AS session_id,
    p.prompt_id                           AS prompt_id,
    s.pseudonym                           AS pseudonym,
    s.cohort                              AS cohort,
    p.started_at                          AS started_at,
    toDate(p.started_at)                  AS day,
    p.clock                               AS clock,
    p.prompt_length                       AS prompt_length,
    p.permission_mode                     AS permission_mode,
    p.command_name                        AS command_name,
    p.command_source                      AS command_source,
    p.command_source = 'builtin'          AS is_builtin_command,
    r.api_requests                        AS api_requests,
    r.main_thread_requests                AS main_thread_requests,
    r.first_context_tokens                AS first_context_tokens,
    r.peak_context_tokens                 AS peak_context_tokens,
    r.output_tokens_total                 AS output_tokens,
    r.cost_usd_total                      AS cost_usd
FROM prompts AS p
LEFT JOIN requests AS r ON r.session_id = p.session_id AND r.prompt_id = p.prompt_id
LEFT JOIN cardo.silver_session_live AS s ON s.session_id = p.session_id;

CREATE OR REPLACE VIEW cardo.silver_policy_decision_live AS
WITH
starts AS
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['prompt.id']                                             AS prompt_id,
        substring(LogAttributes['hook_name'], length('PermissionRequest:') + 1) AS tool_name,
        Timestamp                                                              AS requested_at,
        'client'                                                               AS clock
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.hook_execution_start'
      AND LogAttributes['hook_event'] = 'PermissionRequest'

    UNION ALL

    SELECT
        LogAttributes['session_id'],
        LogAttributes['prompt_id'],
        LogAttributes['tool_name'],
        Timestamp,
        'two_clocks'
    FROM cardo.bronze_hook_events
    WHERE EventName = 'PermissionRequest'
      AND LogAttributes['session_id'] NOT IN
      (
          SELECT LogAttributes['session.id']
          FROM cardo.bronze_otel_logs
          WHERE EventName = 'claude_code.hook_execution_start'
      )
),
answers AS
(
    SELECT
        LogAttributes['session.id']        AS session_id,
        LogAttributes['prompt.id']         AS prompt_id,
        LogAttributes['tool_name']         AS tool_name,
        LogAttributes['tool_use_id']       AS tool_use_id,
        LogAttributes['decision']          AS decision,
        LogAttributes['source']            AS decision_source,
        Timestamp                          AS decided_at
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.tool_decision'
      AND startsWith(LogAttributes['source'], 'user')
),
asking AS
(
    SELECT
        LogAttributes['session_id']                     AS session_id,
        LogAttributes['prompt_id']                      AS prompt_id,
        LogAttributes['tool_name']                      AS tool_name,
        any(LogAttributes['permission_mode'])           AS permission_mode,
        any(LogAttributes['agent_type'])                AS agent_type
    FROM cardo.bronze_hook_events
    WHERE EventName = 'PermissionRequest'
    GROUP BY session_id, prompt_id, tool_name
)
SELECT
    st.session_id                                                              AS session_id,
    st.prompt_id                                                               AS prompt_id,
    ses.pseudonym                                                              AS pseudonym,
    ses.cohort                                                                 AS cohort,
    st.tool_name                                                               AS tool_name,
    ask.permission_mode                                                        AS permission_mode,
    ask.agent_type                                                             AS agent_type,
    st.requested_at                                                            AS requested_at,
    toDate(st.requested_at)                                                    AS day,
    st.clock                                                                   AS clock,
    ans.tool_use_id != ''                                                      AS answered,
    ans.tool_use_id                                                            AS tool_use_id,
    ans.decision                                                               AS decision,
    ans.decision_source                                                        AS decision_source,
    if(ans.tool_use_id != '', ans.decided_at, NULL)                            AS decided_at,
    if(ans.tool_use_id != '', dateDiff('millisecond', st.requested_at, ans.decided_at), NULL) AS wait_ms
FROM starts AS st
ASOF LEFT JOIN answers AS ans
    ON ans.session_id = st.session_id
   AND ans.prompt_id = st.prompt_id
   AND ans.tool_name = st.tool_name
   AND st.requested_at <= ans.decided_at
LEFT JOIN asking AS ask
    ON ask.session_id = st.session_id
   AND ask.prompt_id = st.prompt_id
   AND ask.tool_name = st.tool_name
LEFT JOIN cardo.silver_session_live AS ses ON ses.session_id = st.session_id;

CREATE OR REPLACE VIEW cardo.silver_context_event_live AS
WITH
switch_times AS
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['hook_event']                                            AS hook_event,
        if(hook_event = 'PreModelSwitch', LogAttributes['prompt.id'], '')      AS prompt_key,
        replaceRegexpOne(
            substring(LogAttributes['hook_name'], position(LogAttributes['hook_name'], ':') + 1),
            '(-[0-9]{8})?(\\[1m\\])?$', '')                                    AS model_key,
        min(Timestamp)                                                         AS at
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.hook_execution_start'
      AND LogAttributes['hook_event'] IN ('PreModelSwitch', 'PostModelSwitch')
    GROUP BY session_id, hook_event, prompt_key, model_key
),
switches AS
(
    SELECT
        h.LogAttributes['session_id']                                          AS session_id,
        h.LogAttributes['prompt_id']                                           AS prompt_id,
        if(t.session_id != '', t.at, h.Timestamp)                              AS at,
        if(t.session_id != '', 'client', 'collector')                          AS clock,
        h.LogAttributes['from_model']                                          AS from_model,
        h.LogAttributes['to_model']                                            AS to_model,
        replaceRegexpOne(h.LogAttributes['to_model'], '(-[0-9]{8})?(\\[1m\\])?$', '') AS to_model_key,
        h.LogAttributes['requested_model']                                     AS requested_model,
        h.LogAttributes['model_switch_source']                                 AS switch_source,
        toUInt64OrZero(h.LogAttributes['context_tokens'])                      AS context_tokens,
        h.LogAttributes['prompt_cache_warm']                                   AS prompt_cache_warm,
        h.LogAttributes['cache_ttl']                                           AS cache_ttl,
        toFloat64OrNull(h.LogAttributes['estimated_cache_write_usd'])          AS estimated_cache_write_usd
    FROM cardo.bronze_hook_events AS h
    LEFT JOIN switch_times AS t
        ON t.session_id = h.LogAttributes['session_id']
       AND t.hook_event = h.EventName
       AND t.prompt_key = if(h.EventName = 'PreModelSwitch', h.LogAttributes['prompt_id'], '')
       AND t.model_key = replaceRegexpOne(h.LogAttributes['to_model'], '(-[0-9]{8})?(\\[1m\\])?$', '')
    WHERE h.EventName = 'PreModelSwitch'
       OR (h.EventName = 'PostModelSwitch'
           AND h.LogAttributes['model_switch_source'] IN ('command', 'picker', 'sdk'))
),
main_requests AS
(
    SELECT
        session_id,
        replaceRegexpOne(model, '(-[0-9]{8})?(\\[1m\\])?$', '')            AS model_key,
        ts,
        cache_creation_tokens,
        cache_read_tokens,
        cost_usd
    FROM cardo.silver_api_request_live
    WHERE is_main_thread
),
events AS
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['prompt.id']                                             AS prompt_id,
        'compaction'                                                           AS kind,
        Timestamp                                                              AS at,
        'client'                                                               AS clock,
        LogAttributes['trigger']                                               AS compaction_reason,
        toUInt64OrZero(LogAttributes['pre_tokens'])                            AS tokens_before,
        toUInt64OrZero(LogAttributes['post_tokens'])                           AS tokens_after,
        toUInt64OrZero(LogAttributes['duration_ms'])                           AS duration_ms,
        ''                                                                     AS from_model,
        ''                                                                     AS to_model,
        ''                                                                     AS requested_model,
        ''                                                                     AS switch_source,
        toUInt64(0)                                                            AS context_tokens,
        ''                                                                     AS prompt_cache_warm,
        ''                                                                     AS cache_ttl,
        CAST(NULL, 'Nullable(Float64)')                                        AS estimated_cache_write_usd,
        CAST(NULL, 'Nullable(UInt64)')                                         AS next_request_cache_creation_tokens,
        CAST(NULL, 'Nullable(UInt64)')                                         AS next_request_cache_read_tokens,
        CAST(NULL, 'Nullable(Float64)')                                        AS next_request_cost_usd
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.compaction'

    UNION ALL

    SELECT
        LogAttributes['session_id'],
        LogAttributes['prompt_id'],
        'compaction',
        Timestamp,
        'collector',
        LogAttributes['compaction_reason'],
        0, 0, 0,
        '', '', '', '', 0, '', '',
        NULL, NULL, NULL, NULL
    FROM cardo.bronze_hook_events
    WHERE EventName = 'PreCompact'
      AND LogAttributes['session_id'] NOT IN
      (
          SELECT LogAttributes['session.id']
          FROM cardo.bronze_otel_logs
          WHERE EventName = 'claude_code.compaction'
      )

    UNION ALL

    SELECT
        sw.session_id,
        sw.prompt_id,
        'model_switch',
        sw.at,
        sw.clock,
        '',
        0, 0, 0,
        sw.from_model,
        sw.to_model,
        sw.requested_model,
        sw.switch_source,
        sw.context_tokens,
        sw.prompt_cache_warm,
        sw.cache_ttl,
        sw.estimated_cache_write_usd,
        if(nx.session_id != '', nx.cache_creation_tokens, NULL),
        if(nx.session_id != '', nx.cache_read_tokens, NULL),
        if(nx.session_id != '', nx.cost_usd, NULL)
    FROM switches AS sw
    ASOF LEFT JOIN main_requests AS nx
        ON nx.session_id = sw.session_id
       AND nx.model_key = sw.to_model_key
       AND sw.at <= nx.ts
)
SELECT
    e.session_id                              AS session_id,
    e.prompt_id                               AS prompt_id,
    s.pseudonym                               AS pseudonym,
    s.cohort                                  AS cohort,
    e.kind                                    AS kind,
    e.at                                      AS at,
    toDate(e.at)                              AS day,
    e.clock                                   AS clock,
    e.compaction_reason                       AS compaction_reason,
    e.tokens_before                           AS tokens_before,
    e.tokens_after                            AS tokens_after,
    e.duration_ms                             AS duration_ms,
    e.from_model                              AS from_model,
    e.to_model                                AS to_model,
    e.requested_model                         AS requested_model,
    e.switch_source                           AS switch_source,
    e.context_tokens                          AS context_tokens,
    e.prompt_cache_warm                       AS prompt_cache_warm,
    e.cache_ttl                               AS cache_ttl,
    e.estimated_cache_write_usd               AS estimated_cache_write_usd,
    e.next_request_cache_creation_tokens      AS next_request_cache_creation_tokens,
    e.next_request_cache_read_tokens          AS next_request_cache_read_tokens,
    e.next_request_cost_usd                   AS next_request_cost_usd
FROM events AS e
LEFT JOIN cardo.silver_session_live AS s ON s.session_id = e.session_id;

-- Without is_org, which silver_artifact_load adds when it is read (section 3).
CREATE OR REPLACE VIEW cardo.silver_artifact_load_live AS
SELECT
    l.session_id                                                               AS session_id,
    l.prompt_id                                                                AS prompt_id,
    s.pseudonym                                                                AS pseudonym,
    s.cohort                                                                   AS cohort,
    l.at                                                                       AS at,
    toDate(l.at)                                                               AS day,
    l.kind                                                                     AS kind,
    l.name                                                                     AS name,
    l.file_kind                                                                AS file_kind,
    l.detail                                                                   AS detail,
    (l.kind = 'subagent' AND l.name IN ('general-purpose', 'Explore', 'Plan', 'statusline-setup', 'claude-code-guide'))
        OR (l.kind = 'skill' AND l.detail = 'builtin')                         AS is_builtin
FROM
(
    SELECT
        LogAttributes['session_id']            AS session_id,
        LogAttributes['prompt_id']             AS prompt_id,
        Timestamp                              AS at,
        'instructions'                         AS kind,
        LogAttributes['instructions_name']     AS name,
        LogAttributes['instructions_file']     AS file_kind,
        LogAttributes['load_reason']           AS detail
    FROM cardo.bronze_hook_events
    WHERE EventName = 'InstructionsLoaded'

    UNION ALL

    SELECT
        LogAttributes['session_id'],
        LogAttributes['prompt_id'],
        Timestamp,
        'subagent',
        LogAttributes['agent_type'],
        '',
        ''
    FROM cardo.bronze_hook_events
    WHERE EventName = 'SubagentStart'

    UNION ALL

    SELECT session_id, prompt_id, min(e_at), 'skill', name, '', anyIf(e_detail, e_detail != '')
    FROM
    (
        SELECT
            LogAttributes['session_id']        AS session_id,
            LogAttributes['prompt_id']         AS prompt_id,
            LogAttributes['command_name']      AS name,
            Timestamp                          AS e_at,
            LogAttributes['command_source']    AS e_detail
        FROM cardo.bronze_hook_events
        WHERE EventName = 'UserPromptExpansion'

        UNION ALL

        SELECT
            LogAttributes['session.id'],
            LogAttributes['prompt.id'],
            LogAttributes['skill.name'],
            Timestamp,
            ''
        FROM cardo.bronze_otel_logs
        WHERE EventName = 'claude_code.api_request'
    )
    WHERE session_id != '' AND name != ''
    GROUP BY session_id, prompt_id, name

    UNION ALL

    SELECT session_id, prompt_id, min(e_at), 'mcp', name, '', ''
    FROM
    (
        SELECT
            LogAttributes['session.id']                            AS session_id,
            LogAttributes['prompt.id']                             AS prompt_id,
            splitByString('__', LogAttributes['tool_name'])[2]     AS name,
            Timestamp                                              AS e_at
        FROM cardo.bronze_otel_logs
        WHERE EventName = 'claude_code.tool_decision'
          AND startsWith(LogAttributes['tool_name'], 'mcp__')

        UNION ALL

        SELECT
            LogAttributes['session_id'],
            LogAttributes['prompt_id'],
            splitByString('__', LogAttributes['tool_name'])[2],
            Timestamp
        FROM cardo.bronze_hook_events
        WHERE EventName = 'PermissionRequest'
          AND startsWith(LogAttributes['tool_name'], 'mcp__')
    )
    WHERE session_id != '' AND name != ''
    GROUP BY session_id, prompt_id, name
) AS l
LEFT JOIN cardo.silver_session_live AS s ON s.session_id = l.session_id;

-- 2. The stored tables, and the refreshers that rebuild them.
--
-- A table takes its columns from its _live view (EMPTY AS SELECT creates it with no rows), so the
-- two match when this file runs. A later migration that changes a _live view's columns has to
-- change its table's as well. Rows are sorted by day, which every reader filters or groups on.
-- The first rebuild starts as soon as each refresher is created.

DROP TABLE IF EXISTS cardo.silver_session_refresh;
DROP TABLE IF EXISTS cardo.silver_session;
CREATE TABLE cardo.silver_session
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_session_live;
CREATE MATERIALIZED VIEW cardo.silver_session_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_session
AS SELECT * FROM cardo.silver_session_live;

DROP TABLE IF EXISTS cardo.silver_api_request_refresh;
DROP TABLE IF EXISTS cardo.silver_api_request;
CREATE TABLE cardo.silver_api_request
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_api_request_live;
CREATE MATERIALIZED VIEW cardo.silver_api_request_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_api_request
AS SELECT * FROM cardo.silver_api_request_live;

DROP TABLE IF EXISTS cardo.silver_tool_call_refresh;
DROP TABLE IF EXISTS cardo.silver_tool_call;
CREATE TABLE cardo.silver_tool_call
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_tool_call_live;
CREATE MATERIALIZED VIEW cardo.silver_tool_call_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_tool_call
AS SELECT * FROM cardo.silver_tool_call_live;

DROP TABLE IF EXISTS cardo.silver_turn_refresh;
DROP TABLE IF EXISTS cardo.silver_turn;
CREATE TABLE cardo.silver_turn
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_turn_live;
CREATE MATERIALIZED VIEW cardo.silver_turn_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_turn
AS SELECT * FROM cardo.silver_turn_live;

DROP TABLE IF EXISTS cardo.silver_policy_decision_refresh;
DROP TABLE IF EXISTS cardo.silver_policy_decision;
CREATE TABLE cardo.silver_policy_decision
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_policy_decision_live;
CREATE MATERIALIZED VIEW cardo.silver_policy_decision_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_policy_decision
AS SELECT * FROM cardo.silver_policy_decision_live;

DROP TABLE IF EXISTS cardo.silver_context_event_refresh;
DROP TABLE IF EXISTS cardo.silver_context_event;
CREATE TABLE cardo.silver_context_event
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_context_event_live;
CREATE MATERIALIZED VIEW cardo.silver_context_event_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_context_event
AS SELECT * FROM cardo.silver_context_event_live;

DROP TABLE IF EXISTS cardo.silver_artifact_load_refresh;
DROP TABLE IF EXISTS cardo.silver_artifact_load_stored;
CREATE TABLE cardo.silver_artifact_load_stored
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_artifact_load_live;
CREATE MATERIALIZED VIEW cardo.silver_artifact_load_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_artifact_load_stored
AS SELECT * FROM cardo.silver_artifact_load_live;

-- 3. silver_artifact_load: the stored rows, and is_org worked out as they are read.
--
-- is_org applies CARDO_ORG_ARTIFACTS as the views were given it (settings_effective), as 006
-- describes: an empty pattern names nothing, MCP is tried on the server's name and on
-- 'mcp__<server>__', and an instructions file with a name at all is the organization's.
CREATE OR REPLACE VIEW cardo.silver_artifact_load AS
WITH
    (SELECT org_artifacts FROM cardo.settings_effective) AS org_pattern
SELECT
    l.*,
    l.name != '' AND (l.kind = 'instructions'
        OR (org_pattern != '' AND (match(l.name, org_pattern)
            OR (l.kind = 'mcp' AND match(concat('mcp__', l.name, '__'), org_pattern))))) AS is_org
FROM cardo.silver_artifact_load_stored AS l;

-- 4. When silver was last rebuilt, for the dashboards to show (ADR-0052, decision 6). A table that
-- stopped rebuilding then shows as old data, rather than as a quiet week.
--
-- rebuilt_at is the oldest of the refreshers' last good rebuilds, so one that keeps failing holds
-- it back, and it is NULL until every one has succeeded once. failing counts the refreshers whose
-- last try failed.
CREATE OR REPLACE VIEW cardo.gold_data_as_of AS
SELECT
    if(countIf(last_success_time IS NULL) > 0, NULL, min(last_success_time))    AS rebuilt_at,
    countIf(exception != '')                                                    AS failing,
    count()                                                                     AS refreshers
FROM system.view_refreshes
WHERE database = 'cardo' AND startsWith(view, 'silver_') AND endsWith(view, '_refresh');
