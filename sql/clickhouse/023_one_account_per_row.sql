-- 023 — a session can carry more than one account, and each row belongs to the account on it
-- (ADR-0059).
--
-- WHAT WENT WRONG. When someone switches Claude accounts in the middle of a session, Claude Code
-- keeps the session id and sends the new account's email from then on, so one session's rows
-- carry two code names. silver_session_live picked one with anyIf, which returns whichever row
-- ClickHouse reads first, and the other silver views took the session's pick. The whole session,
-- its prompts and its dollars, moved between the two accounts from one read to the next, and the
-- coach showed different figures on each reload. The same pick sized teams for the five-person
-- minimum, counted fleet adoption and decided whose artifact loads they were.
--
-- WHAT CHANGES, section by section below:
--   1. silver_session_account_live says which account was active in each session at each moment.
--   2. The silver views take the account from each row, and silver_session has one row per
--      session and account. Their columns are unchanged, so the stored tables stay as they are.
--   3. The views that give a session its team label keep one row per session, and the team from
--      the organization's directory gives each account its own.
--   4. Every reader counts a session once, and the daily views by team leave service runs out.
--   5. The trend rows of the days that hold a switched session are cleared, to be recomputed.
--
-- For example, a session starts on account A, which sends 12 prompts costing $4.10, and the person
-- then switches to account B, which sends 5 prompts costing $1.30. A's coach shows 1 session, 12
-- prompts and $4.10, B's shows 1 session, 5 prompts and $1.30, and the fleet shows 1 session, 17
-- prompts and $5.40, on every read.
--
-- WHAT IT COSTS. Every view that credits a row reads the lookup in section 1, which reads every
-- OTel event once. On 5 million made-up OTel rows, building all nine stored tables took 23 seconds
-- against 10 before, inside the 10-minute rebuild (ADR-0052). The gold views read the stored tables
-- and are not slower.
--
-- RE-RUNNABLE. Every view is created or replaced, and section 5's scratch table is emptied before
-- it is filled. A second run clears the same trend days again, and `cardo trend` recomputes them
-- again: nothing is lost.
--
-- 1. THE ACCOUNT ACTIVE AT EACH MOMENT. A hook row carries no identity (ADR-0023), and an OTel
-- event can arrive without an email. Such a row belongs to the account active when it was sent:
-- the last code name the session showed before it. silver_session_account_live lists, for each
-- session, each moment the code name changed, and a view credits an unnamed row with an ASOF join
-- on it:
--
--     ASOF LEFT JOIN cardo.silver_session_account_live AS a
--         ON a.session_id = r.session_id AND r.at >= a.active_from
--
-- For example, a session whose events carry account A from 10:00, B from 10:03 and A again from
-- 10:06 has three rows here: A from the beginning of time, B from 10:03, A from 10:06. A hook row
-- at 10:05 is B's, and the session's end at 10:07 is A's. The session's first account is active
-- from the beginning of time, so a hook row sent before any OTel event, such as the session's
-- start, takes the first account. A session with no code name at all has no row here, and its rows
-- stay unattributed.
--
-- A code name is read from every OTel event, on the record or on the resource, wherever the
-- collector put it, and from the session-count metric. The metric is placed at the start of its
-- interval, which is never later than the session's start: its own time is when it was exported,
-- up to a minute later, which would have looked like a switch back to the first account.
--
-- The hook row's time is the collector's, and an OTel event's is the laptop's. A hook row sent
-- within a few seconds of a switch can land on the other account when the clocks disagree, and it
-- lands there on every read (ADR-0059, consequences).
--
-- This is a plain view, read only by other _live views and the deletion procedure: a rebuild
-- still starts from the raw tables (017, "no stored table reads another").

CREATE OR REPLACE VIEW cardo.silver_session_account_live AS
SELECT
    session_id,
    pseudonym,
    if(n = 1, toDateTime64(0, 9), seen_at)                                     AS active_from,
    cohort
FROM
(
    SELECT
        session_id,
        pseudonym,
        seen_at,
        row_number() OVER w                                                    AS n,
        lagInFrame(pseudonym, 1, '') OVER w                                    AS before,
        argMinIf(e_cohort, seen_at, e_cohort != '')
            OVER (PARTITION BY session_id, pseudonym)                          AS cohort
    FROM
    (
        SELECT
            LogAttributes['session.id']                                        AS session_id,
            Timestamp                                                          AS seen_at,
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])                           AS pseudonym,
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               LogAttributes['cardo.cohort'])                                  AS e_cohort
        FROM cardo.bronze_otel_logs
        WHERE session_id != '' AND pseudonym != ''

        UNION ALL

        SELECT
            Attributes['session.id'],
            toDateTime64(StartTimeUnix, 9),
            if(Attributes['user.pseudonym'] != '', Attributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym']),
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               Attributes['cardo.cohort'])
        FROM cardo.bronze_otel_metrics_sum
        WHERE MetricName = 'claude_code.session.count'
          AND Attributes['session.id'] != ''
          AND if(Attributes['user.pseudonym'] != '', Attributes['user.pseudonym'],
                 ResourceAttributes['user.pseudonym']) != ''
    )
    WINDOW w AS (PARTITION BY session_id ORDER BY seen_at, pseudonym
                 ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
)
WHERE pseudonym != before;

-- 2. THE SILVER VIEWS TAKE THE ACCOUNT FROM THE ROW. Each starts from its last definition (017,
-- and 020 for the repository views) and changes only where the account comes from, and any pick
-- that was `any`: every such pick is now the first value seen, by time. The columns, their order
-- and their types are unchanged, so the stored tables keep working as they are and their next
-- rebuild reads these definitions. In each view a row's own code name wins, and a row without one
-- takes the account active at its moment from section 1, with that account's team label.
--
-- silver_session_live has one row per session and account. Each row describes that account's part
-- of the session: when it started and last showed an event, its clock and paths, its release and
-- terminal, whether the session ended in it. The session's day, the day its first account started,
-- is on every row, so a session is counted on the day it started whichever account was on it.
-- Whether it is a service run, and its workflow, belong to the session.

CREATE OR REPLACE VIEW cardo.silver_session_live AS
SELECT
    session_id,
    pseudonym,
    salt_version,
    cohort,
    app_version,
    terminal_type,
    start_type,
    started_at,
    last_event_at,
    toDate(min(started_at) OVER (PARTITION BY session_id))                     AS day,
    clock,
    ended,
    end_reason,
    has_otel,
    has_hooks,
    actor_kind,
    workflow
FROM
(
    SELECT
        session_id,
        nullIf(account, '')                                                    AS pseudonym,
        argMinIf(e_salt_version, e_ts, e_salt_version != '' AND e_clock != 'collector') AS salt_version,
        argMinIf(e_cohort, e_ts, e_cohort != '')                               AS cohort,
        argMinIf(e_app_version, e_ts, e_app_version != '')                     AS app_version,
        argMinIf(e_terminal_type, e_ts, e_terminal_type != '')                 AS terminal_type,
        argMinIf(e_start_type, e_ts, e_start_type != '')                       AS start_type,
        multiIf(countIf(e_clock = 'client') > 0, minIf(e_ts, e_clock = 'client'),
                countIf(e_clock = 'collector') > 0, minIf(e_ts, e_clock = 'collector'),
                min(e_ts))                                                     AS started_at,
        multiIf(countIf(e_clock = 'client') > 0, maxIf(e_ts, e_clock = 'client'),
                countIf(e_clock = 'collector') > 0, maxIf(e_ts, e_clock = 'collector'),
                max(e_ts))                                                     AS last_event_at,
        if(countIf(e_clock = 'client') > 0, 'client', 'collector')             AS clock,
        countIf(e_event = 'SessionEnd') > 0                                    AS ended,
        argMinIf(e_end_reason, e_ts, e_end_reason != '')                       AS end_reason,
        countIf(e_clock = 'client') > 0                                        AS has_otel,
        countIf(e_clock = 'collector') > 0                                     AS has_hooks,
        if(max(svc_kind) = 'service', 'service', 'person')                     AS actor_kind,
        max(svc_workflow)                                                      AS workflow
    FROM
    (
        SELECT
            b.*,
            if(b.e_pseudonym != '', b.e_pseudonym, acct.pseudonym)             AS account
        FROM
        (
            SELECT
                LogAttributes['session.id']                                    AS session_id,
                if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
                   ResourceAttributes['user.pseudonym'])                       AS e_pseudonym,
                ResourceAttributes['cardo.salt_version']                       AS e_salt_version,
                if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
                   LogAttributes['cardo.cohort'])                              AS e_cohort,
                LogAttributes['app.version']                                   AS e_app_version,
                LogAttributes['terminal.type']                                 AS e_terminal_type,
                ''                                                             AS e_start_type,
                Timestamp                                                      AS e_ts,
                'client'                                                       AS e_clock,
                EventName                                                      AS e_event,
                ''                                                             AS e_end_reason
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
        ASOF LEFT JOIN cardo.silver_session_account_live AS acct
            ON acct.session_id = b.session_id AND b.e_ts >= acct.active_from
    ) AS e
    LEFT JOIN
    (
        SELECT
            LogAttributes['session.id']                                        AS session_id,
            'service'                                                          AS svc_kind,
            argMin(ResourceAttributes['cardo.workflow'], Timestamp)            AS svc_workflow
        FROM cardo.bronze_otel_logs
        WHERE ResourceAttributes['cardo.actor'] = 'service' AND LogAttributes['session.id'] != ''
        GROUP BY session_id
    ) AS svc USING (session_id)
    WHERE session_id NOT LIKE 'cardo-probe-%'
    GROUP BY session_id, account
);

CREATE OR REPLACE VIEW cardo.silver_api_request_live AS
SELECT
    r.session_id                                                               AS session_id,
    r.prompt_id                                                                AS prompt_id,
    nullIf(if(r.own_pseudonym != '', r.own_pseudonym, acct.pseudonym), '')     AS pseudonym,
    if(r.own_cohort != '', r.own_cohort, acct.cohort)                          AS cohort,
    r.ts                                                                       AS ts,
    toDate(r.ts)                                                               AS day,
    r.model                                                                    AS model,
    r.query_source                                                             AS query_source,
    r.query_source = 'repl_main_thread'                                        AS is_main_thread,
    r.effort                                                                   AS effort,
    r.skill_name                                                               AS skill_name,
    r.agent_name                                                               AS agent_name,
    r.input_tokens                                                             AS input_tokens,
    r.output_tokens                                                            AS output_tokens,
    r.cache_read_tokens                                                        AS cache_read_tokens,
    r.cache_creation_tokens                                                    AS cache_creation_tokens,
    r.input_tokens + r.cache_read_tokens + r.cache_creation_tokens             AS context_tokens,
    r.cost_usd                                                                 AS cost_usd,
    r.duration_ms                                                              AS duration_ms
FROM
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['prompt.id']                                             AS prompt_id,
        if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym'])                               AS own_pseudonym,
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           LogAttributes['cardo.cohort'])                                      AS own_cohort,
        Timestamp                                                              AS ts,
        LogAttributes['model']                                                 AS model,
        LogAttributes['query_source']                                          AS query_source,
        LogAttributes['effort']                                                AS effort,
        LogAttributes['skill.name']                                            AS skill_name,
        LogAttributes['agent.name']                                            AS agent_name,
        toUInt64OrZero(LogAttributes['input_tokens'])                          AS input_tokens,
        toUInt64OrZero(LogAttributes['output_tokens'])                         AS output_tokens,
        toUInt64OrZero(LogAttributes['cache_read_tokens'])                     AS cache_read_tokens,
        toUInt64OrZero(LogAttributes['cache_creation_tokens'])                 AS cache_creation_tokens,
        toFloat64OrZero(LogAttributes['cost_usd'])                             AS cost_usd,
        toUInt64OrZero(LogAttributes['duration_ms'])                           AS duration_ms
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.api_request'
) AS r
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = r.session_id AND r.ts >= acct.active_from;

CREATE OR REPLACE VIEW cardo.silver_tool_call_live AS
SELECT
    k.session_id                                                               AS session_id,
    k.tool_use_id                                                              AS tool_use_id,
    k.prompt_id                                                                AS prompt_id,
    nullIf(if(k.own_pseudonym != '', k.own_pseudonym, acct.pseudonym), '')     AS pseudonym,
    if(k.own_cohort != '', k.own_cohort, acct.cohort)                          AS cohort,
    k.tool_name                                                                AS tool_name,
    k.decided_at                                                               AS decided_at,
    toDate(k.first_at)                                                         AS day,
    k.decision                                                                 AS decision,
    k.decision_source                                                          AS decision_source,
    k.ran                                                                      AS ran,
    k.succeeded                                                                AS succeeded,
    k.duration_ms                                                              AS duration_ms,
    k.tool_name IN ('Edit', 'MultiEdit', 'Write', 'NotebookEdit')              AS is_edit,
    if(startsWith(k.tool_name, 'mcp__'), splitByString('__', k.tool_name)[2], '') AS mcp_server
FROM
(
    SELECT
        session_id,
        tool_use_id,
        argMinIf(e_prompt_id, e_ts, e_prompt_id != '')                         AS prompt_id,
        argMinIf(e_pseudonym, e_ts, e_pseudonym != '')                         AS own_pseudonym,
        argMinIf(e_cohort, e_ts, e_cohort != '')                               AS own_cohort,
        argMinIf(e_tool_name, e_ts, e_tool_name != '')                         AS tool_name,
        minIf(e_ts, e_event = 'claude_code.tool_decision')                     AS decided_at,
        min(e_ts)                                                              AS first_at,
        argMinIf(e_decision, e_ts, e_event = 'claude_code.tool_decision')      AS decision,
        argMinIf(e_decision_source, e_ts, e_event = 'claude_code.tool_decision') AS decision_source,
        countIf(e_event = 'claude_code.tool_result') > 0                       AS ran,
        argMinIf(e_success, e_ts, e_event = 'claude_code.tool_result') = 'true' AS succeeded,
        maxIf(e_duration_ms, e_event = 'claude_code.tool_result')              AS duration_ms
    FROM
    (
        SELECT
            LogAttributes['session.id']                                        AS session_id,
            LogAttributes['tool_use_id']                                       AS tool_use_id,
            LogAttributes['prompt.id']                                         AS e_prompt_id,
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])                           AS e_pseudonym,
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               LogAttributes['cardo.cohort'])                                  AS e_cohort,
            LogAttributes['tool_name']                                         AS e_tool_name,
            Timestamp                                                          AS e_ts,
            EventName                                                          AS e_event,
            LogAttributes['decision']                                          AS e_decision,
            LogAttributes['source']                                            AS e_decision_source,
            LogAttributes['success']                                           AS e_success,
            toUInt64OrZero(LogAttributes['duration_ms'])                       AS e_duration_ms
        FROM cardo.bronze_otel_logs
        WHERE EventName IN ('claude_code.tool_decision', 'claude_code.tool_result')
          AND LogAttributes['session.id'] != ''
          AND LogAttributes['tool_use_id'] != ''
    )
    GROUP BY session_id, tool_use_id
) AS k
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = k.session_id AND k.first_at >= acct.active_from;

-- A prompt belongs to the account on its OTel event, or, when only its hook arrived, to the
-- account active when it started. Its requests' cost goes with it.
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
        argMinIf(e_permission_mode, e_ts, e_permission_mode != '')                          AS permission_mode,
        argMinIf(e_command_name, e_ts, e_command_name != '')                                AS command_name,
        argMinIf(e_command_source, e_ts, e_command_source != '')                            AS command_source,
        argMinIf(e_pseudonym, e_ts, e_pseudonym != '')                                      AS own_pseudonym,
        argMinIf(e_cohort, e_ts, e_cohort != '')                                            AS own_cohort
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
            LogAttributes['command_source']                    AS e_command_source,
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])           AS e_pseudonym,
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               LogAttributes['cardo.cohort'])                  AS e_cohort
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
            '',
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
    p.session_id                                                               AS session_id,
    p.prompt_id                                                                AS prompt_id,
    nullIf(if(p.own_pseudonym != '', p.own_pseudonym, acct.pseudonym), '')     AS pseudonym,
    if(p.own_cohort != '', p.own_cohort, acct.cohort)                          AS cohort,
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
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = p.session_id AND p.started_at >= acct.active_from;

-- A permission wait belongs to the account active when it was asked: the OTel event's own code
-- name, or the account active when the hook arrived.
CREATE OR REPLACE VIEW cardo.silver_policy_decision_live AS
WITH
starts AS
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['prompt.id']                                             AS prompt_id,
        substring(LogAttributes['hook_name'], length('PermissionRequest:') + 1) AS tool_name,
        Timestamp                                                              AS requested_at,
        'client'                                                               AS clock,
        if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym'])                               AS own_pseudonym,
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           LogAttributes['cardo.cohort'])                                      AS own_cohort
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.hook_execution_start'
      AND LogAttributes['hook_event'] = 'PermissionRequest'

    UNION ALL

    SELECT
        LogAttributes['session_id'],
        LogAttributes['prompt_id'],
        LogAttributes['tool_name'],
        Timestamp,
        'two_clocks',
        '',
        ''
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
        LogAttributes['session_id']                         AS session_id,
        LogAttributes['prompt_id']                          AS prompt_id,
        LogAttributes['tool_name']                          AS tool_name,
        argMin(LogAttributes['permission_mode'], Timestamp) AS permission_mode,
        argMin(LogAttributes['agent_type'], Timestamp)      AS agent_type
    FROM cardo.bronze_hook_events
    WHERE EventName = 'PermissionRequest'
    GROUP BY session_id, prompt_id, tool_name
)
SELECT
    st.session_id                                                              AS session_id,
    st.prompt_id                                                               AS prompt_id,
    nullIf(if(st.own_pseudonym != '', st.own_pseudonym, acct.pseudonym), '')   AS pseudonym,
    if(st.own_cohort != '', st.own_cohort, acct.cohort)                        AS cohort,
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
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = st.session_id AND st.requested_at >= acct.active_from;

-- A compaction or a model switch belongs to the account active when it happened.
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
        CAST(NULL, 'Nullable(Float64)')                                        AS next_request_cost_usd,
        if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
           ResourceAttributes['user.pseudonym'])                               AS own_pseudonym,
        if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
           LogAttributes['cardo.cohort'])                                      AS own_cohort
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
        NULL, NULL, NULL, NULL,
        '', ''
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
        if(nx.session_id != '', nx.cost_usd, NULL),
        '',
        ''
    FROM switches AS sw
    ASOF LEFT JOIN main_requests AS nx
        ON nx.session_id = sw.session_id
       AND nx.model_key = sw.to_model_key
       AND sw.at <= nx.ts
)
SELECT
    e.session_id                                                               AS session_id,
    e.prompt_id                                                                AS prompt_id,
    nullIf(if(e.own_pseudonym != '', e.own_pseudonym, acct.pseudonym), '')     AS pseudonym,
    if(e.own_cohort != '', e.own_cohort, acct.cohort)                          AS cohort,
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
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = e.session_id AND e.at >= acct.active_from;

-- An artifact load belongs to the account active when it loaded. Most loads are hook rows; a
-- skill or an MCP server seen on an OTel event keeps that event's code name. Without is_org, which
-- silver_artifact_load adds when it is read (017, section 3).
CREATE OR REPLACE VIEW cardo.silver_artifact_load_live AS
SELECT
    l.session_id                                                               AS session_id,
    l.prompt_id                                                                AS prompt_id,
    nullIf(if(l.own_pseudonym != '', l.own_pseudonym, acct.pseudonym), '')     AS pseudonym,
    if(l.own_cohort != '', l.own_cohort, acct.cohort)                          AS cohort,
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
        LogAttributes['load_reason']           AS detail,
        ''                                     AS own_pseudonym,
        ''                                     AS own_cohort
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
        '',
        '',
        ''
    FROM cardo.bronze_hook_events
    WHERE EventName = 'SubagentStart'

    UNION ALL

    SELECT session_id, prompt_id, min(e_at), 'skill', name, '', argMinIf(e_detail, e_at, e_detail != ''),
           argMinIf(e_pseudonym, e_at, e_pseudonym != ''), argMinIf(e_cohort, e_at, e_cohort != '')
    FROM
    (
        SELECT
            LogAttributes['session_id']        AS session_id,
            LogAttributes['prompt_id']         AS prompt_id,
            LogAttributes['command_name']      AS name,
            Timestamp                          AS e_at,
            LogAttributes['command_source']    AS e_detail,
            ''                                 AS e_pseudonym,
            ''                                 AS e_cohort
        FROM cardo.bronze_hook_events
        WHERE EventName = 'UserPromptExpansion'

        UNION ALL

        SELECT
            LogAttributes['session.id'],
            LogAttributes['prompt.id'],
            LogAttributes['skill.name'],
            Timestamp,
            '',
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym']),
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               LogAttributes['cardo.cohort'])
        FROM cardo.bronze_otel_logs
        WHERE EventName = 'claude_code.api_request'
    )
    WHERE session_id != '' AND name != ''
    GROUP BY session_id, prompt_id, name

    UNION ALL

    SELECT session_id, prompt_id, min(e_at), 'mcp', name, '', '',
           argMinIf(e_pseudonym, e_at, e_pseudonym != ''), argMinIf(e_cohort, e_at, e_cohort != '')
    FROM
    (
        SELECT
            LogAttributes['session.id']                            AS session_id,
            LogAttributes['prompt.id']                             AS prompt_id,
            splitByString('__', LogAttributes['tool_name'])[2]     AS name,
            Timestamp                                              AS e_at,
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])               AS e_pseudonym,
            if(ResourceAttributes['cardo.cohort'] != '', ResourceAttributes['cardo.cohort'],
               LogAttributes['cardo.cohort'])                      AS e_cohort
        FROM cardo.bronze_otel_logs
        WHERE EventName = 'claude_code.tool_decision'
          AND startsWith(LogAttributes['tool_name'], 'mcp__')

        UNION ALL

        SELECT
            LogAttributes['session_id'],
            LogAttributes['prompt_id'],
            splitByString('__', LogAttributes['tool_name'])[2],
            Timestamp,
            '',
            ''
        FROM cardo.bronze_hook_events
        WHERE EventName = 'PermissionRequest'
          AND startsWith(LogAttributes['tool_name'], 'mcp__')
    )
    WHERE session_id != '' AND name != ''
    GROUP BY session_id, prompt_id, name
) AS l
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = l.session_id AND l.at >= acct.active_from;

-- One row per session, as 020 had it: a session's repository is the session's, whichever account
-- was on it.
CREATE OR REPLACE VIEW cardo.silver_session_repo_live AS
SELECT
    s.session_id                                  AS session_id,
    s.day                                         AS day,
    if(empty(r.repo_class), 'none', r.repo_class) AS repo_class,
    r.repo                                        AS repo
FROM
(
    SELECT session_id, min(day) AS day
    FROM cardo.silver_session_live
    GROUP BY session_id
) AS s
LEFT JOIN
(
    SELECT
        session_id,
        argMinIf(e_repo, e_ts, e_repo != '')             AS repo,
        argMinIf(e_repo_class, e_ts, e_repo_class != '') AS repo_class
    FROM
    (
        SELECT
            LogAttributes['session.id']                AS session_id,
            Timestamp                                  AS e_ts,
            if(LogAttributes['cardo.repo'] != '', LogAttributes['cardo.repo'],
               ResourceAttributes['cardo.repo'])       AS e_repo,
            if(LogAttributes['cardo.repo.class'] != '', LogAttributes['cardo.repo.class'],
               ResourceAttributes['cardo.repo.class']) AS e_repo_class
        FROM cardo.bronze_otel_logs
        WHERE LogAttributes['session.id'] != ''
    )
    GROUP BY session_id
) AS r ON r.session_id = s.session_id;

-- An instructions file load is a hook row, so it belongs to the account active when it loaded.
-- Service runs stay out, as 020 kept them out.
CREATE OR REPLACE VIEW cardo.silver_instruction_load_live AS
SELECT
    l.session_id                                          AS session_id,
    l.prompt_id                                           AS prompt_id,
    l.day                                                 AS day,
    l.at                                                  AS at,
    l.file_kind                                           AS file_kind,
    l.org_name                                            AS org_name,
    l.name                                                AS name,
    l.scope                                               AS scope,
    l.load_reason                                         AS load_reason,
    nullIf(acct.pseudonym, '')                            AS pseudonym,
    if(empty(r.repo_class), 'none', r.repo_class)         AS repo_class,
    r.repo                                                AS repo
FROM
(
    SELECT
        LogAttributes['session_id']                        AS session_id,
        LogAttributes['prompt_id']                         AS prompt_id,
        toDate(Timestamp)                                  AS day,
        Timestamp                                          AS at,
        LogAttributes['instructions_file']                 AS file_kind,
        LogAttributes['instructions_name']                 AS org_name,
        LogAttributes['instructions_project_name']         AS name,
        multiIf(
            ifNull(LogAttributes['memory_type'], '') = 'Project', 'project',
            ifNull(LogAttributes['memory_type'], '') = 'User', 'user',
            ifNull(LogAttributes['memory_type'], '') = 'Managed', 'managed',
            LogAttributes['instructions_scope'] = 'managed', 'managed',
            '')                                            AS scope,
        LogAttributes['load_reason']                       AS load_reason
    FROM cardo.bronze_hook_events
    WHERE EventName = 'InstructionsLoaded'
      AND LogAttributes['session_id'] IN
      (
          SELECT session_id FROM cardo.silver_session_live WHERE actor_kind != 'service'
      )
) AS l
ASOF LEFT JOIN cardo.silver_session_account_live AS acct
    ON acct.session_id = l.session_id AND l.at >= acct.active_from
LEFT JOIN cardo.silver_session_repo_live AS r ON r.session_id = l.session_id;

-- 3. THE VIEWS THAT GIVE A SESSION ITS TEAM. silver_session_cohort (007) and
-- silver_session_cohort_week (018) keep one row per session, so every gold view still joins them on
-- the session alone, and a gold view never names a code name except to count people (INV-3, the
-- check in test/invariants_test.go). A session's rows go under the team label of the account it
-- started on. The label comes from the machine's settings (OTEL_RESOURCE_ATTRIBUTES), which do not
-- change inside one Claude Code process, so both accounts of a switched session carry the same one.
-- The size that decides whether a team is named still counts each account under its own label.
--
-- silver_session_cohort now leaves service runs out, as silver_session_cohort_week always has: the
-- daily views counted a CI job as a person (ADR-0036), and the check that every view from 012 on
-- leaves them out reaches the daily views as soon as they are redefined here.
--
-- silver_session_team (014) is different: its team comes from the directory, by code name, so each
-- account has its own. It gains the code name as its last column, and the one view that joins it
-- to rows, silver_team_artifact_use, joins on the session and the code name:
--
--     ON t.session_id = a.session_id AND t.pseudonym <=> a.pseudonym
--
-- `<=>` matches two NULLs, so a session with no code name at all still finds its one row.

CREATE OR REPLACE VIEW cardo.silver_session_cohort AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sizes AS
    (
        SELECT day, cohort, count(DISTINCT pseudonym) AS cohort_people
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY day, cohort
    ),
    sessions AS
    (
        SELECT session_id, min(day) AS day, argMin(cohort, started_at) AS cohort
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY session_id
    )
SELECT
    s.session_id                                                                   AS session_id,
    s.day                                                                          AS day,
    if(z.cohort_people >= min_group, if(s.cohort = '', 'unassigned', s.cohort), 'other') AS cohort
FROM sessions AS s
INNER JOIN sizes AS z ON z.day = s.day AND z.cohort = s.cohort;

CREATE OR REPLACE VIEW cardo.silver_session_cohort_week AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sizes AS
    (
        SELECT toMonday(day) AS week, cohort, count(DISTINCT pseudonym) AS cohort_people
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY week, cohort
    ),
    sessions AS
    (
        SELECT session_id, toMonday(min(day)) AS week, argMin(cohort, started_at) AS cohort
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY session_id
    )
SELECT
    s.session_id                                                                   AS session_id,
    s.week                                                                         AS week,
    if(z.cohort_people >= min_group, if(s.cohort = '', 'unassigned', s.cohort), 'other') AS cohort
FROM sessions AS s
INNER JOIN sizes AS z ON z.week = s.week AND z.cohort = s.cohort;

CREATE OR REPLACE VIEW cardo.silver_session_team AS
WITH
    current_batch AS
    (
        SELECT batch
        FROM cardo.settings_teams_batches
        ORDER BY loaded_at DESC
        LIMIT 1
    ),
    members AS
    (
        SELECT t.team AS team, t.pseudonym AS pseudonym, t.salt_version AS salt_version
        FROM cardo.settings_teams AS t
        INNER JOIN current_batch AS b ON t.batch = b.batch
    )
SELECT
    s.session_id AS session_id,
    toMonday(s.day) AS week,
    if(m.team != '', m.team, if(s.cohort != '', s.cohort, 'unassigned')) AS team,
    s.pseudonym AS pseudonym
FROM cardo.silver_session AS s
LEFT JOIN members AS m
    ON m.pseudonym = s.pseudonym AND m.salt_version = s.salt_version
WHERE s.actor_kind != 'service';

CREATE OR REPLACE VIEW cardo.silver_team_artifact_use AS
SELECT
    t.week                   AS week,
    t.team                   AS team,
    a.kind                   AS kind,
    a.name                   AS name,
    a.is_org                 AS is_org,
    a.is_builtin             AS is_builtin,
    a.pseudonym              AS pseudonym,
    a.session_id             AS session_id,
    a.day                    AS day
FROM cardo.silver_artifact_load AS a
INNER JOIN cardo.silver_session_team AS t
    ON t.session_id = a.session_id AND t.pseudonym <=> a.pseudonym;

-- 4. EVERY READER COUNTS A SESSION ONCE. silver_session has a row per account now, so a view that
-- counted its rows as sessions counts distinct session ids, and a view that joined it to row-level
-- rows by the session joins a list of sessions with one row each. Each view below is its last
-- definition with that change and no other, except where a line says so. The weekly views that
-- join only silver_session_cohort_week need nothing: it still has one row per session.

-- The daily views by team now leave service runs out (section 3).
CREATE OR REPLACE VIEW cardo.gold_friction_daily AS
WITH
    sessions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count(DISTINCT s.session_id) AS sessions_total
        FROM cardo.silver_session AS s
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = s.session_id
        WHERE s.actor_kind != 'service'
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

CREATE OR REPLACE VIEW cardo.gold_context_daily AS
WITH
    sessions AS
    (
        SELECT c.day AS day, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count(DISTINCT s.session_id) AS sessions_total
        FROM cardo.silver_session AS s
        INNER JOIN cardo.silver_session_cohort AS c ON c.session_id = s.session_id
        WHERE s.actor_kind != 'service'
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

CREATE OR REPLACE VIEW cardo.gold_context_weekly AS
WITH
    sessions AS
    (
        SELECT c.week AS week, c.cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count(DISTINCT s.session_id) AS sessions_total
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

-- Each team's people come from silver_session_team itself, which carries the code name, so the
-- team's week is not joined to silver_session row by row.
CREATE OR REPLACE VIEW cardo.gold_team_reach AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    fleet AS
    (
        SELECT toMonday(day) AS week, count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY week
    ),
    team_week AS
    (
        SELECT
            t.week AS week,
            t.team AS team,
            count(DISTINCT t.pseudonym) AS active_people
        FROM cardo.silver_session_team AS t
        GROUP BY week, team
    ),
    per_name AS
    (
        SELECT week, kind, name, count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_team_artifact_use
        GROUP BY week, kind, name
    ),
    usage AS
    (
        SELECT
            u.week                                                       AS week,
            u.team                                                       AS team,
            u.kind                                                       AS kind,
            if(u.is_org OR u.is_builtin OR u.name = '' OR n.name_people >= min_group,
               u.name, 'other')                                          AS artifact,
            count(DISTINCT pseudonym)                                    AS people_total,
            count()                                                      AS uses_total
        FROM cardo.silver_team_artifact_use AS u
        INNER JOIN per_name AS n
            ON n.week = u.week AND n.kind = u.kind AND n.name = u.name
        GROUP BY week, team, kind, artifact
    ),
    org_usage AS
    (
        SELECT
            toMonday(a.day)                                              AS week,
            a.kind                                                       AS kind,
            if(a.is_org OR a.is_builtin OR a.name = '' OR n.name_people >= min_group,
               a.name, 'other')                                          AS artifact,
            count(DISTINCT pseudonym)                                    AS people_total
        FROM cardo.silver_artifact_load AS a
        INNER JOIN per_name AS n
            ON n.week = toMonday(a.day) AND n.kind = a.kind AND n.name = a.name
        WHERE a.session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service')
        GROUP BY week, kind, artifact
    )
SELECT
    u.week                                                         AS week,
    u.team                                                         AS team,
    u.kind                                                         AS kind,
    u.artifact                                                     AS artifact,
    u.people_total                                                 AS people,
    u.uses_total                                                   AS uses,
    t.active_people                                                AS team_active_people,
    f.active_people                                                AS fleet_people,
    round(u.people_total / nullIf(t.active_people, 0), 4)          AS team_reach,
    round(o.people_total / nullIf(f.active_people, 0), 4)          AS org_reach
FROM usage AS u
INNER JOIN team_week AS t
    ON t.week = u.week AND t.team = u.team
LEFT JOIN fleet AS f
    ON f.week = u.week
LEFT JOIN org_usage AS o
    ON o.week = u.week AND o.kind = u.kind AND o.artifact = u.artifact
WHERE t.active_people >= min_group
ORDER BY u.week ASC, u.artifact ASC, u.team ASC;

CREATE OR REPLACE VIEW cardo.trend_day_source AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sessions AS
    (
        SELECT day AS day,
               count(DISTINCT pseudonym)  AS people_total,
               count(DISTINCT session_id) AS sessions_total
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY day
    ),
    turns AS
    (
        SELECT s.day                            AS day,
               countIf(NOT t.is_builtin_command) AS prompts_total
        FROM cardo.silver_turn AS t
        INNER JOIN (SELECT DISTINCT session_id, day FROM cardo.silver_session
                    WHERE actor_kind != 'service') AS s
            ON s.session_id = t.session_id
        GROUP BY day
    ),
    compactions AS
    (
        SELECT s.day                                                 AS day,
               countIf(e.kind = 'compaction')                        AS compactions_total,
               countIf(e.kind = 'compaction' AND e.compaction_reason = 'auto') AS auto_compactions_total
        FROM cardo.silver_context_event AS e
        INNER JOIN (SELECT DISTINCT session_id, day FROM cardo.silver_session
                    WHERE actor_kind != 'service') AS s
            ON s.session_id = e.session_id
        GROUP BY day
    )
SELECT
    s.day                                       AS day,
    s.people_total                              AS active_people,
    s.sessions_total                            AS sessions,
    t.prompts_total                             AS prompts,
    m.compactions_total                         AS compactions,
    m.auto_compactions_total                    AS auto_compactions
FROM sessions AS s
LEFT JOIN turns AS t ON t.day = s.day
LEFT JOIN compactions AS m ON m.day = s.day
WHERE s.people_total >= min_group;

-- A session runs one Claude Code release, so each session is joined to its week and release once.
CREATE OR REPLACE VIEW cardo.gold_release_cache_weekly AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    per_release AS
    (
        SELECT s.week AS week, s.app_version AS app_version, count(DISTINCT pseudonym) AS release_people
        FROM cardo.silver_api_request AS r
        INNER JOIN (SELECT session_id, toMonday(min(day)) AS week, argMin(app_version, started_at) AS app_version
                    FROM cardo.silver_session WHERE actor_kind != 'service' GROUP BY session_id) AS s
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
        INNER JOIN (SELECT session_id, toMonday(min(day)) AS week, argMin(app_version, started_at) AS app_version
                    FROM cardo.silver_session WHERE actor_kind != 'service' GROUP BY session_id) AS s
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
        INNER JOIN (SELECT DISTINCT session_id, toMonday(day) AS week FROM cardo.silver_session
                    WHERE actor_kind != 'service') AS s
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

-- The fleet adoption page adds up each person's sessions (021). A switched session is counted for
-- the account it started on, so the sum counts it once; the account that joined it later still
-- has its row, and still counts as active that day.
CREATE OR REPLACE VIEW cardo.silver_actor_day_sessions AS
SELECT
    'sessions'                                     AS source,
    day,
    pseudonym,
    toInt64(uniqExactIf(session_id, opened))       AS sessions,
    toInt64(0)                                     AS lines_added,
    toInt64(0)                                     AS lines_removed,
    toInt64(0)                                     AS commits,
    toInt64(0)                                     AS pull_requests,
    toInt64(0)                                     AS edit_accepted,
    toInt64(0)                                     AS edit_rejected,
    toInt64(0)                                     AS multi_edit_accepted,
    toInt64(0)                                     AS multi_edit_rejected,
    toInt64(0)                                     AS write_accepted,
    toInt64(0)                                     AS write_rejected,
    toInt64(0)                                     AS notebook_accepted,
    toInt64(0)                                     AS notebook_rejected
FROM
(
    SELECT day, pseudonym, session_id,
           started_at = min(started_at) OVER (PARTITION BY session_id) AS opened
    FROM cardo.silver_session
    WHERE actor_kind != 'service'
)
WHERE pseudonym IS NOT NULL
  AND pseudonym != ''
GROUP BY day, pseudonym;

-- 5. THE TREND ROWS WRITTEN FROM THE OLD PICK ARE RECOMPUTED. A day or week that holds a session
-- with two accounts was written while the session counted for one of them, so the other was
-- missing from its people counts. Those rows are cleared, and `cardo trend` recomputes them on its
-- next pass: it fills every day in the window that has no row, rebuilding stored silver first
-- (the trend rows that outlive their rows, ADR-0055). As in 022, only days inside the retention
-- window are cleared, with a day's margin, because `cardo trend` refuses an older day and a row
-- cleared there would be lost for ever. The days go through a scratch table, read once, and the
-- statements are safe to run again from the first.

CREATE TABLE IF NOT EXISTS cardo.repair_023_days (day Date) ENGINE = MergeTree ORDER BY day;

TRUNCATE TABLE cardo.repair_023_days;

INSERT INTO cardo.repair_023_days
SELECT DISTINCT day
FROM cardo.silver_session_live
WHERE session_id IN
(
    SELECT session_id FROM cardo.silver_session_live
    GROUP BY session_id
    HAVING count() > 1
);

ALTER TABLE cardo.trend_day DELETE
WHERE day IN (SELECT day FROM cardo.repair_023_days)
  AND ((SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
        FROM cardo.settings WHERE key = 'retention_days') = 0
       OR day >= today() - (SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
                            FROM cardo.settings WHERE key = 'retention_days') + 2)
SETTINGS mutations_sync = 2;

ALTER TABLE cardo.trend_week_artifact DELETE
WHERE week IN (SELECT toMonday(day) FROM cardo.repair_023_days)
  AND ((SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
        FROM cardo.settings WHERE key = 'retention_days') = 0
       OR week >= today() - (SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
                             FROM cardo.settings WHERE key = 'retention_days') + 2)
SETTINGS mutations_sync = 2;

DROP TABLE IF EXISTS cardo.repair_023_days;
