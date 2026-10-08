-- 009 — a model switch's next request is matched on the model with its date and [1m] removed.
--
-- 008 times a switch by comparing to_model with the hook name after removing a date and a [1m]
-- suffix from both, and then finds what the switch cost by comparing to_model with the next
-- main-thread api_request's model as written. A dated to_model (claude-haiku-4-5-20251001, seen
-- on a 2.1.289 resume) or one with [1m] then misses a request that names the model another way,
-- and the switch shows no cost at all. Which spelling api_request uses beside a dated to_model has
-- not been observed, so both sides are now compared with the date and [1m] removed, as the hook
-- name already is. to_model itself is kept as sent.
--
-- 008 is immutable once shipped, so silver_context_event is redefined here. Everything but
-- to_model_key, model_key and the ASOF join's model condition is 008's.

-- One row per compaction or model switch: the events that reshape a session's context.
--
-- A compaction's tokens and time come from OTel's claude_code.compaction. Its pre_tokens appears
-- to count the conversation without Claude Code's fixed prefix (6,505 against a 51,788-token
-- context, observed twice), so it is not comparable with context_tokens elsewhere. A session that
-- sent no OTel compaction event falls back to its PreCompact hook rows, with no tokens.
--
-- A model switch's facts come from its hook, and its time from Claude Code's own
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
LEFT JOIN cardo.silver_session AS s ON s.session_id = e.session_id;
