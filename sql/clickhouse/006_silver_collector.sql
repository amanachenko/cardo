-- 006 — silver for the collector path: the canonical events, one view each.
--
-- ClickHouse only (ADR-0028). These read the three bronze tables the collector writes
-- (004_bronze_collector.sql) and publish the session-grain model in docs/design/data-model.md.
-- They carry a pseudonym, as silver_actor_day does, so dashboards may not read them (INV-3); gold
-- is where the pseudonym is counted away.
--
-- FOUR THINGS EVERY VIEW HERE RELIES ON. Each was measured from a real Claude Code 2.1.281
-- session, not read from documentation (docs/research/2026-09-24-*.md).
--
--   1. A hook row carries no identity. It reaches a person only through its session: hook
--      `session_id` equals OTel `session.id`, and OTel rows carry `user.pseudonym`. Hook-derived
--      views join silver_session for pseudonym and cohort. If OTel is off, both are NULL.
--
--   2. Two clocks. OTel rows carry the laptop's time. Hook payloads carry none, so a hook row
--      carries the collector's receive time, measured 1 to 3 s behind the laptop and not steady.
--      A duration never has one end on each clock. Where a hook row's time is the only one
--      available, the row says so in a `clock` column.
--
--   3. Hook and OTel prompt ids are the same value: hook `prompt_id` = OTel `prompt.id`.
--
--   4. Every attribute value is a string. Numbers are cast here with *OrZero, so a value that is
--      not a number reads as 0 rather than failing the whole view.
--
-- DIALECT NOTE. An aggregate may not take the name of a column it reads (`any(x) AS x` is
-- ILLEGAL_AGGREGATION here, fine in DuckDB). The inner queries below name their columns with an
-- `e_` prefix, for event, so the grouped outer query can publish the plain names.

-- One row per session.
--
-- The SessionStart hook never runs in Claude Code 2.1.281, so a session starts at its first
-- event on the laptop's clock, and at its first hook row only when it sent no OTel at all.
-- claude_code.session.count contributes start_type and identity, never a time: a metric's
-- timestamp is its export interval's, not the event's.
CREATE OR REPLACE VIEW cardo.silver_session AS
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
    countIf(e_clock = 'collector') > 0                                         AS has_hooks
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
)
GROUP BY session_id;

-- One row per API request Claude Code made: the main thread, subagents, and its own helpers
-- (prompt suggestion, session titles, compaction), told apart by query_source.
--
-- context_tokens is what the request sent: input + cache read + cache creation. On a main-thread
-- request that is the size of the conversation at that moment, which is what PreModelSwitch's
-- context_tokens also reports (to the token, in the switch observed). Claude Code sends no
-- breakdown of what fills it, and nothing here guesses.
--
-- cost_usd is Claude Code's own figure, in dollars, per request.
CREATE OR REPLACE VIEW cardo.silver_api_request AS
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

-- One row per user prompt, including built-in commands such as /model, which Claude Code reports
-- as prompts with a command_name.
--
-- The hook adds permission_mode; OTel adds the command and the laptop's time. The two are joined
-- on the prompt id. Context is taken from the prompt's main-thread requests: the first says what
-- the prompt started from, the peak how large the conversation grew while answering it.
CREATE OR REPLACE VIEW cardo.silver_turn AS
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
    FROM cardo.silver_api_request
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
LEFT JOIN cardo.silver_session AS s ON s.session_id = p.session_id;

-- One row per tool call, from OTel's tool_decision and tool_result, joined on tool_use_id.
--
-- A rejected call has a decision and no result. decision_source says who decided: `config` for
-- a permission rule or mode (auto mode's approvals included, as observed), `user_temporary`,
-- `user_permanent` or `user_reject` for a person.
CREATE OR REPLACE VIEW cardo.silver_tool_call AS
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

-- One row per permission prompt: how long a person took to answer it.
--
-- BOTH ENDS ARE ON THE LAPTOP'S CLOCK. The prompt appears when Claude Code runs the
-- PermissionRequest hooks, which it reports itself as hook_execution_start with
-- hook_name = 'PermissionRequest:<tool>'. It fires on every permission prompt because the bundle
-- registers a PermissionRequest hook. The answer is the next tool_decision for the same session,
-- prompt and tool whose source is a person's. The hook carries no tool_use_id, hence the as-of
-- join on those three.
--
-- The PermissionRequest hook row's own time is NOT used for the start: it is the collector's
-- clock, and using it added about 1.3 s to each real wait. hook_execution_start is undocumented,
-- so if a session has none at all, its hook rows are the fallback, and those rows say
-- clock = 'two_clocks'. Treat their waits as approximate.
--
-- A prompt nobody answered (interrupted, or answered by a hook) has a NULL wait.
CREATE OR REPLACE VIEW cardo.silver_policy_decision AS
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
LEFT JOIN cardo.silver_session AS ses ON ses.session_id = st.session_id;

-- One row per compaction or model switch: the events that reshape a session's context.
--
-- A compaction's tokens and time come from OTel's claude_code.compaction. Its pre_tokens appears
-- to count the conversation without Claude Code's fixed prefix (6,505 against a 51,788-token
-- context, observed twice), so it is not comparable with context_tokens elsewhere. A session that
-- sent no OTel compaction event falls back to its PreCompact hook rows, with no tokens.
--
-- A model switch's facts come from the PreModelSwitch hook, and its time from Claude Code's own
-- hook_execution_start for it, on the laptop's clock. What the switch actually cost is on the
-- next main-thread request on the new model: its cache_creation_tokens and cost_usd.
-- estimated_cache_write_usd is Claude Code's estimate, which assumes the whole context is
-- rewritten; in the switch observed it was 3.2 times that next request's entire cost (ADR-0027).
CREATE OR REPLACE VIEW cardo.silver_context_event AS
WITH
switch_times AS
(
    SELECT
        LogAttributes['session.id']                                            AS session_id,
        LogAttributes['prompt.id']                                             AS prompt_id,
        substring(LogAttributes['hook_name'], length('PreModelSwitch:') + 1)   AS to_model,
        min(Timestamp)                                                         AS at
    FROM cardo.bronze_otel_logs
    WHERE EventName = 'claude_code.hook_execution_start'
      AND LogAttributes['hook_event'] = 'PreModelSwitch'
    GROUP BY session_id, prompt_id, to_model
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
        h.LogAttributes['requested_model']                                     AS requested_model,
        h.LogAttributes['model_switch_source']                                 AS switch_source,
        toUInt64OrZero(h.LogAttributes['context_tokens'])                      AS context_tokens,
        h.LogAttributes['prompt_cache_warm']                                   AS prompt_cache_warm,
        h.LogAttributes['cache_ttl']                                           AS cache_ttl,
        toFloat64OrNull(h.LogAttributes['estimated_cache_write_usd'])          AS estimated_cache_write_usd
    FROM cardo.bronze_hook_events AS h
    LEFT JOIN switch_times AS t
        ON t.session_id = h.LogAttributes['session_id']
       AND t.prompt_id = h.LogAttributes['prompt_id']
       AND t.to_model = h.LogAttributes['to_model']
    WHERE h.EventName = 'PreModelSwitch'
),
main_requests AS
(
    SELECT session_id, model, ts, cache_creation_tokens, cache_read_tokens, cost_usd
    FROM cardo.silver_api_request
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
       AND nx.model = sw.to_model
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
LEFT JOIN cardo.silver_session AS s ON s.session_id = e.session_id;

-- One row per use of an artifact: an instructions file loaded, a skill or command used in a
-- prompt, a subagent started, an MCP server called in a prompt.
--
--   instructions   InstructionsLoaded, one row per load. `name` is the file's own name only when
--                  it is the organization's (ADR-0026); otherwise empty, and file_kind says
--                  whether it was a CLAUDE.md, a rule or something else.
--   skill          UserPromptExpansion (a command the engineer typed) and skill.name on API
--                  requests (a skill, including one the model chose), one row per prompt and name.
--   subagent       SubagentStart, one row per start. Never SubagentStop, which Claude Code's
--                  own helpers fire with no start.
--   mcp            tool calls whose name is mcp__<server>__<tool>, one row per prompt and server.
--
-- is_org applies CARDO_ORG_ARTIFACTS as the views were given it (settings_effective). An empty
-- pattern names nothing; ClickHouse's match() would treat it as matching everything, so it is
-- tested for first. For MCP the pattern is tried on the server's name and on 'mcp__<server>__',
-- since the collector's strict mode matches it against the whole tool name. An instructions file
-- that has a name at all is the organization's: the collector stored the name only because it
-- matched the collector's pattern, so no disagreement between the two patterns can relabel it.
--
-- is_builtin marks Claude Code's own subagents and built-in commands. Like the organization's
-- names, they identify nobody, and gold shows them regardless of how many people use them
-- (ADR-0029). The subagent list is the one the collector's strict mode exempts.
CREATE OR REPLACE VIEW cardo.silver_artifact_load AS
WITH
    (SELECT org_artifacts FROM cardo.settings_effective) AS org_pattern
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
        OR (l.kind = 'skill' AND l.detail = 'builtin')                         AS is_builtin,
    l.name != '' AND (l.kind = 'instructions'
        OR (org_pattern != '' AND (match(l.name, org_pattern)
            OR (l.kind = 'mcp' AND match(concat('mcp__', l.name, '__'), org_pattern))))) AS is_org
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
LEFT JOIN cardo.silver_session AS s ON s.session_id = l.session_id;
