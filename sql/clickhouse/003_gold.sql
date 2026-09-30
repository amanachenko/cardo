-- Gold: the marts the dashboards read, ClickHouse dialect.
--
-- Mirrors sql/duckdb/030_gold.sql. INV-3 governs this layer: no per-person view is visible to
-- anyone except that person, and tier 1 is cohort-only. Every view here aggregates the pseudonym
-- away. If a view would let a manager rank their reports, it is the wrong view -- and none of
-- these may grow a pseudonym column, which is enforced by a test rather than left to discipline.
--
-- Dialect note, learned the hard way rather than read: ClickHouse rejects an aggregate aliased to
-- the name of the column it reads (`sum(sessions) AS sessions`) with ILLEGAL_AGGREGATION, which is
-- a shape the DuckDB file uses freely. Every view here therefore aggregates in a CTE with `_total`
-- suffixes and renames at the outer SELECT, so the published column names still match DuckDB's
-- exactly. Same names out, different names in -- a dashboard cannot tell which engine it is on.

-- Fleet adoption. How many people are actually using it, and how much.
CREATE OR REPLACE VIEW cardo.gold_fleet_adoption AS
WITH totals AS
(
    SELECT
        day,
        source,
        count(DISTINCT pseudonym) AS active_actors,
        sum(sessions)             AS sessions_total,
        sum(lines_added)          AS lines_added_total,
        sum(lines_removed)        AS lines_removed_total,
        sum(commits)              AS commits_total,
        sum(pull_requests)        AS pull_requests_total
    FROM cardo.silver_actor_day
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    sessions_total                                          AS sessions,
    lines_added_total                                       AS lines_added,
    lines_removed_total                                     AS lines_removed,
    commits_total                                           AS commits,
    pull_requests_total                                     AS pull_requests,
    round(sessions_total / nullIf(active_actors, 0), 2)     AS sessions_per_active_actor
FROM totals
ORDER BY day;

-- Tool acceptance.
--
-- ADR-0008 makes the volume denominator a product constraint rather than styling: an acceptance
-- rate without one rewards timidity, because a session that proposes nothing is never rejected.
-- `proposals` is therefore selected beside every rate, and a rate is NULL rather than 0 when
-- nothing was proposed -- an absent rate is honest, a zero is a lie.
CREATE OR REPLACE VIEW cardo.gold_tool_acceptance AS
WITH totals AS
(
    SELECT
        day,
        source,
        sum(edit_accepted + multi_edit_accepted + write_accepted + notebook_accepted) AS accepted_total,
        sum(edit_rejected + multi_edit_rejected + write_rejected + notebook_rejected) AS rejected_total,
        sum(edit_accepted)        AS edit_accepted_total,
        sum(edit_rejected)        AS edit_rejected_total,
        sum(write_accepted)       AS write_accepted_total,
        sum(write_rejected)       AS write_rejected_total,
        count(DISTINCT pseudonym) AS active_actors
    FROM cardo.silver_actor_day
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    accepted_total AS accepted,
    rejected_total AS rejected,
    accepted_total + rejected_total AS proposals,
    round(accepted_total / nullIf(accepted_total + rejected_total, 0), 4) AS acceptance_rate,
    round(edit_accepted_total / nullIf(edit_accepted_total + edit_rejected_total, 0), 4) AS edit_acceptance_rate,
    edit_accepted_total + edit_rejected_total AS edit_proposals,
    round(write_accepted_total / nullIf(write_accepted_total + write_rejected_total, 0), 4) AS write_acceptance_rate,
    write_accepted_total + write_rejected_total AS write_proposals
FROM totals
ORDER BY day;

-- Model mix and spend. Cost arrives in cents and is already dollars by the silver layer.
CREATE OR REPLACE VIEW cardo.gold_model_mix AS
WITH per_model AS
(
    SELECT
        day,
        source,
        model,
        count(DISTINCT pseudonym)  AS actors,
        sum(tokens_input)          AS tokens_input_total,
        sum(tokens_output)         AS tokens_output_total,
        sum(tokens_cache_read)     AS tokens_cache_read_total,
        sum(tokens_cache_creation) AS tokens_cache_creation_total,
        sum(cost_usd)              AS cost_usd_total
    FROM cardo.silver_actor_day_model
    GROUP BY day, source, model
)
SELECT
    day,
    source,
    model,
    actors,
    tokens_input_total          AS tokens_input,
    tokens_output_total         AS tokens_output,
    tokens_cache_read_total     AS tokens_cache_read,
    tokens_cache_creation_total AS tokens_cache_creation,
    round(cost_usd_total, 2)    AS cost_usd,
    round(cost_usd_total / nullIf(sum(cost_usd_total) OVER (PARTITION BY day, source), 0), 4) AS cost_share
FROM per_model
ORDER BY day ASC, cost_usd DESC;

-- Daily spend, for the capacity conversation.
CREATE OR REPLACE VIEW cardo.gold_cost_daily AS
WITH totals AS
(
    SELECT
        day,
        source,
        count(DISTINCT pseudonym) AS active_actors,
        sum(cost_usd)             AS cost_usd_total
    FROM cardo.silver_actor_day_model
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    round(cost_usd_total, 2)                               AS cost_usd,
    round(cost_usd_total / nullIf(active_actors, 0), 2)    AS cost_usd_per_active_actor
FROM totals
ORDER BY day;

-- Adapter drift, surfaced as data rather than only as a log line the operator may have missed.
-- ADR-0014: unmodelled fields are stored verbatim and reported, never dropped.
--
-- This is the view to check after a Claude Code or API change. A row here means the response grew
-- a field the adapter does not model; the payload kept it, and the canonical views need updating.
CREATE OR REPLACE VIEW cardo.gold_schema_drift AS
SELECT
    source,
    unmodelled_field,
    count()  AS rows_affected,
    min(day) AS first_seen,
    max(day) AS last_seen
FROM
(
    SELECT
        source,
        day,
        arrayJoin(unknown_fields) AS unmodelled_field
    FROM cardo.silver_actor_day
    WHERE length(unknown_fields) > 0
)
GROUP BY source, unmodelled_field
ORDER BY rows_affected DESC;
