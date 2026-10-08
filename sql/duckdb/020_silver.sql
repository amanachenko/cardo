-- Silver: the canonical layer.
--
-- ADR-0010 makes this views over raw rather than a storage format, so realigning with whatever
-- the ecosystem settles on is a rewrite here rather than a migration underneath.
--
-- A note on grain. docs/design/data-model.md describes session-grain canonical views, and those
-- are Phase 2 — they need the hook and OTel path. The analytics APIs report a daily aggregate per
-- actor and can never produce session-level facts (ADR-0017). So this source lands on its own
-- grain, actor-day, and the two coexist rather than one pretending to be the other.
--
-- An absent field reads as 0, or '' for text, as ClickHouse's JSONExtract functions make it. DuckDB's
-- `->>` yields NULL, and a NULL counter makes edit_accepted + multi_edit_accepted + ... NULL for
-- the whole row, which SUM then skips without a word. Before the COALESCE, a source that left out
-- one tool undercounted the fleet's acceptance here and not in ClickHouse.

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

    COALESCE(CAST(payload->>'$.core_metrics.num_sessions'                 AS BIGINT), 0) AS sessions,
    COALESCE(CAST(payload->>'$.core_metrics.lines_of_code.added'          AS BIGINT), 0) AS lines_added,
    COALESCE(CAST(payload->>'$.core_metrics.lines_of_code.removed'        AS BIGINT), 0) AS lines_removed,
    COALESCE(CAST(payload->>'$.core_metrics.commits_by_claude_code'       AS BIGINT), 0) AS commits,
    COALESCE(CAST(payload->>'$.core_metrics.pull_requests_by_claude_code' AS BIGINT), 0) AS pull_requests,

    COALESCE(CAST(payload->>'$.tool_actions.edit_tool.accepted'          AS BIGINT), 0) AS edit_accepted,
    COALESCE(CAST(payload->>'$.tool_actions.edit_tool.rejected'          AS BIGINT), 0) AS edit_rejected,
    COALESCE(CAST(payload->>'$.tool_actions.multi_edit_tool.accepted'    AS BIGINT), 0) AS multi_edit_accepted,
    COALESCE(CAST(payload->>'$.tool_actions.multi_edit_tool.rejected'    AS BIGINT), 0) AS multi_edit_rejected,
    COALESCE(CAST(payload->>'$.tool_actions.write_tool.accepted'         AS BIGINT), 0) AS write_accepted,
    COALESCE(CAST(payload->>'$.tool_actions.write_tool.rejected'         AS BIGINT), 0) AS write_rejected,
    COALESCE(CAST(payload->>'$.tool_actions.notebook_edit_tool.accepted' AS BIGINT), 0) AS notebook_accepted,
    COALESCE(CAST(payload->>'$.tool_actions.notebook_edit_tool.rejected' AS BIGINT), 0) AS notebook_rejected,

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
    COALESCE(m.value->>'$.model', '')                                        AS model,
    COALESCE(CAST(m.value->>'$.tokens.input'          AS BIGINT), 0)         AS tokens_input,
    COALESCE(CAST(m.value->>'$.tokens.output'         AS BIGINT), 0)         AS tokens_output,
    COALESCE(CAST(m.value->>'$.tokens.cache_read'     AS BIGINT), 0)         AS tokens_cache_read,
    COALESCE(CAST(m.value->>'$.tokens.cache_creation' AS BIGINT), 0)         AS tokens_cache_creation,
    COALESCE(CAST(m.value->>'$.estimated_cost.amount' AS BIGINT), 0)         AS cost_cents,
    COALESCE(CAST(m.value->>'$.estimated_cost.amount' AS DOUBLE) / 100.0, 0) AS cost_usd,
    COALESCE(m.value->>'$.estimated_cost.currency', '')                      AS currency
FROM bronze_actor_day AS b,
     LATERAL json_each(b.payload, '$.model_breakdown') AS m;
