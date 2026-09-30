-- Silver: the canonical layer.
--
-- ADR-0010 makes this views over raw rather than a storage format, so realigning with whatever
-- the ecosystem settles on is a rewrite here rather than a migration underneath.
--
-- A note on grain. docs/design/data-model.md describes session-grain canonical views, and those
-- are Phase 2 — they need the hook and OTel path. The analytics APIs report a daily aggregate per
-- actor and can never produce session-level facts (ADR-0017). So this source lands on its own
-- grain, actor-day, and the two coexist rather than one pretending to be the other.

CREATE OR REPLACE VIEW silver_actor_day AS
SELECT
    source,
    day,
    pseudonym,
    salt_version,
    tier,
    actor_type,
    org_id,
    customer_type,
    terminal_type,

    CAST(payload->>'$.core_metrics.num_sessions'                 AS BIGINT) AS sessions,
    CAST(payload->>'$.core_metrics.lines_of_code.added'          AS BIGINT) AS lines_added,
    CAST(payload->>'$.core_metrics.lines_of_code.removed'        AS BIGINT) AS lines_removed,
    CAST(payload->>'$.core_metrics.commits_by_claude_code'       AS BIGINT) AS commits,
    CAST(payload->>'$.core_metrics.pull_requests_by_claude_code' AS BIGINT) AS pull_requests,

    CAST(payload->>'$.tool_actions.edit_tool.accepted'          AS BIGINT) AS edit_accepted,
    CAST(payload->>'$.tool_actions.edit_tool.rejected'          AS BIGINT) AS edit_rejected,
    CAST(payload->>'$.tool_actions.multi_edit_tool.accepted'    AS BIGINT) AS multi_edit_accepted,
    CAST(payload->>'$.tool_actions.multi_edit_tool.rejected'    AS BIGINT) AS multi_edit_rejected,
    CAST(payload->>'$.tool_actions.write_tool.accepted'         AS BIGINT) AS write_accepted,
    CAST(payload->>'$.tool_actions.write_tool.rejected'         AS BIGINT) AS write_rejected,
    CAST(payload->>'$.tool_actions.notebook_edit_tool.accepted' AS BIGINT) AS notebook_accepted,
    CAST(payload->>'$.tool_actions.notebook_edit_tool.rejected' AS BIGINT) AS notebook_rejected,

    COALESCE(unknown_fields, []) AS unknown_fields
FROM bronze_actor_day;

-- One row per actor, day and model.
--
-- estimated_cost.amount is documented in CENTS. It is divided once, here, and the column is named
-- in dollars so that nothing downstream has to remember. Getting this wrong is a silent 100x on
-- every cost figure the product reports.
CREATE OR REPLACE VIEW silver_actor_day_model AS
SELECT
    b.source,
    b.day,
    b.pseudonym,
    b.org_id,
    m.value->>'$.model'                                 AS model,
    CAST(m.value->>'$.tokens.input'          AS BIGINT) AS tokens_input,
    CAST(m.value->>'$.tokens.output'         AS BIGINT) AS tokens_output,
    CAST(m.value->>'$.tokens.cache_read'     AS BIGINT) AS tokens_cache_read,
    CAST(m.value->>'$.tokens.cache_creation' AS BIGINT) AS tokens_cache_creation,
    CAST(m.value->>'$.estimated_cost.amount' AS BIGINT) AS cost_cents,
    CAST(m.value->>'$.estimated_cost.amount' AS DOUBLE) / 100.0 AS cost_usd,
    m.value->>'$.estimated_cost.currency'               AS currency
FROM bronze_actor_day AS b,
     LATERAL json_each(b.payload, '$.model_breakdown') AS m;
