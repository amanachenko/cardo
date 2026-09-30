-- Silver: the canonical layer, ClickHouse dialect.
--
-- Mirrors sql/duckdb/020_silver.sql. ADR-0010 makes this views over raw rather than a storage
-- format, so realigning with whatever the ecosystem settles on is a rewrite here, not a migration
-- underneath.
--
-- Grain note, same as the DuckDB file: docs/design/data-model.md describes session-grain canonical
-- views and those are Phase 2, needing the hook and OTel path. The analytics APIs report a daily
-- aggregate per actor and can never produce session-level facts (ADR-0017). This source lands on
-- its own grain, actor-day, and the two coexist rather than one pretending to be the other.
--
-- One dialect difference worth knowing before comparing outputs: DuckDB's `payload->>'$.x'` yields
-- NULL for an absent field, while ClickHouse's JSONExtractInt yields 0. Sums agree; "was this
-- field present at all" does not. If that distinction ever becomes load-bearing, read it from
-- unknown_fields, which is explicit, rather than from a zero, which is ambiguous.

CREATE OR REPLACE VIEW cardo.silver_actor_day AS
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

    JSONExtractInt(payload, 'core_metrics', 'num_sessions')                 AS sessions,
    JSONExtractInt(payload, 'core_metrics', 'lines_of_code', 'added')       AS lines_added,
    JSONExtractInt(payload, 'core_metrics', 'lines_of_code', 'removed')     AS lines_removed,
    JSONExtractInt(payload, 'core_metrics', 'commits_by_claude_code')       AS commits,
    JSONExtractInt(payload, 'core_metrics', 'pull_requests_by_claude_code') AS pull_requests,

    JSONExtractInt(payload, 'tool_actions', 'edit_tool', 'accepted')          AS edit_accepted,
    JSONExtractInt(payload, 'tool_actions', 'edit_tool', 'rejected')          AS edit_rejected,
    JSONExtractInt(payload, 'tool_actions', 'multi_edit_tool', 'accepted')    AS multi_edit_accepted,
    JSONExtractInt(payload, 'tool_actions', 'multi_edit_tool', 'rejected')    AS multi_edit_rejected,
    JSONExtractInt(payload, 'tool_actions', 'write_tool', 'accepted')         AS write_accepted,
    JSONExtractInt(payload, 'tool_actions', 'write_tool', 'rejected')         AS write_rejected,
    JSONExtractInt(payload, 'tool_actions', 'notebook_edit_tool', 'accepted') AS notebook_accepted,
    JSONExtractInt(payload, 'tool_actions', 'notebook_edit_tool', 'rejected') AS notebook_rejected,

    unknown_fields
FROM cardo.bronze_actor_day;

-- One row per actor, day and model.
--
-- estimated_cost.amount is documented in CENTS. It is divided once, here, and the column is named
-- in dollars so that nothing downstream has to remember. Getting this wrong is a silent 100x on
-- every cost figure the product reports.
--
-- model_breakdown is a JSON array inside the payload. JSONExtractArrayRaw returns its elements as
-- raw JSON strings and arrayJoin explodes them into rows -- the ClickHouse equivalent of DuckDB's
-- LATERAL json_each. An actor with no model activity contributes no rows, which is why this view
-- cannot be used to count active actors; use silver_actor_day for that.
CREATE OR REPLACE VIEW cardo.silver_actor_day_model AS
SELECT
    source,
    day,
    pseudonym,
    org_id,
    JSONExtractString(m, 'model')                        AS model,
    JSONExtractInt(m, 'tokens', 'input')                 AS tokens_input,
    JSONExtractInt(m, 'tokens', 'output')                AS tokens_output,
    JSONExtractInt(m, 'tokens', 'cache_read')            AS tokens_cache_read,
    JSONExtractInt(m, 'tokens', 'cache_creation')        AS tokens_cache_creation,
    JSONExtractInt(m, 'estimated_cost', 'amount')        AS cost_cents,
    JSONExtractInt(m, 'estimated_cost', 'amount') / 100.0 AS cost_usd,
    JSONExtractString(m, 'estimated_cost', 'currency')   AS currency
FROM
(
    SELECT
        source,
        day,
        pseudonym,
        org_id,
        arrayJoin(JSONExtractArrayRaw(payload, 'model_breakdown')) AS m
    FROM cardo.bronze_actor_day
);
