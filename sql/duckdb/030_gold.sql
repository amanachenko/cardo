-- Gold: the marts the dashboards read.
--
-- INV-3 governs this layer: no per-person view is visible to anyone except that person, and
-- tier 1 is cohort-only. Every view here aggregates the pseudonym away. If a view would let a
-- manager rank their reports, it is the wrong view — and none of these may grow a pseudonym
-- column, which is enforced by a test rather than left to discipline.

-- Fleet adoption. How many people are actually using it, and how much.
CREATE OR REPLACE VIEW gold_fleet_adoption AS
SELECT
    day,
    source,
    COUNT(DISTINCT pseudonym)                         AS active_actors,
    SUM(sessions)                                     AS sessions,
    SUM(lines_added)                                  AS lines_added,
    SUM(lines_removed)                                AS lines_removed,
    SUM(commits)                                      AS commits,
    SUM(pull_requests)                                AS pull_requests,
    ROUND(SUM(sessions) / NULLIF(COUNT(DISTINCT pseudonym), 0), 2) AS sessions_per_active_actor
FROM silver_actor_day
GROUP BY day, source
ORDER BY day;

-- Tool acceptance.
--
-- ADR-0008 makes the volume denominator a product constraint rather than styling: an acceptance
-- rate without one rewards timidity, because a session that proposes nothing is never rejected.
-- `proposals` is therefore selected beside every rate, and a rate is NULL rather than 0 when
-- nothing was proposed — an absent rate is honest, a zero is a lie.
CREATE OR REPLACE VIEW gold_tool_acceptance AS
WITH totals AS (
    SELECT
        day,
        source,
        SUM(edit_accepted + multi_edit_accepted + write_accepted + notebook_accepted) AS accepted,
        SUM(edit_rejected + multi_edit_rejected + write_rejected + notebook_rejected) AS rejected,
        SUM(edit_accepted) AS edit_accepted, SUM(edit_rejected) AS edit_rejected,
        SUM(write_accepted) AS write_accepted, SUM(write_rejected) AS write_rejected,
        COUNT(DISTINCT pseudonym) AS active_actors
    FROM silver_actor_day
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    accepted,
    rejected,
    accepted + rejected AS proposals,
    ROUND(accepted::DOUBLE / NULLIF(accepted + rejected, 0), 4) AS acceptance_rate,
    ROUND(edit_accepted::DOUBLE / NULLIF(edit_accepted + edit_rejected, 0), 4) AS edit_acceptance_rate,
    edit_accepted + edit_rejected AS edit_proposals,
    ROUND(write_accepted::DOUBLE / NULLIF(write_accepted + write_rejected, 0), 4) AS write_acceptance_rate,
    write_accepted + write_rejected AS write_proposals
FROM totals
ORDER BY day;

-- Model mix and spend. Cost arrives in cents and is already dollars by the silver layer.
CREATE OR REPLACE VIEW gold_model_mix AS
SELECT
    day,
    source,
    model,
    COUNT(DISTINCT pseudonym)     AS actors,
    SUM(tokens_input)             AS tokens_input,
    SUM(tokens_output)            AS tokens_output,
    SUM(tokens_cache_read)        AS tokens_cache_read,
    SUM(tokens_cache_creation)    AS tokens_cache_creation,
    ROUND(SUM(cost_usd), 2)       AS cost_usd,
    ROUND(SUM(cost_usd) / NULLIF(SUM(SUM(cost_usd)) OVER (PARTITION BY day, source), 0), 4) AS cost_share
FROM silver_actor_day_model
GROUP BY day, source, model
ORDER BY day, cost_usd DESC;

-- Daily spend, for the capacity conversation.
CREATE OR REPLACE VIEW gold_cost_daily AS
SELECT
    day,
    source,
    COUNT(DISTINCT pseudonym)                                   AS active_actors,
    ROUND(SUM(cost_usd), 2)                                     AS cost_usd,
    ROUND(SUM(cost_usd) / NULLIF(COUNT(DISTINCT pseudonym), 0), 2) AS cost_usd_per_active_actor
FROM silver_actor_day_model
GROUP BY day, source
ORDER BY day;

-- Adapter drift, surfaced as data rather than only as a log line the operator may have missed.
-- ADR-0014: unmodelled fields are stored verbatim and reported, never dropped.
CREATE OR REPLACE VIEW gold_schema_drift AS
WITH exploded AS (
    SELECT
        source,
        day,
        unnest(unknown_fields) AS unmodelled_field
    FROM silver_actor_day
    WHERE len(unknown_fields) > 0
)
SELECT
    source,
    unmodelled_field,
    COUNT(*) AS rows_affected,
    MIN(day) AS first_seen,
    MAX(day) AS last_seen
FROM exploded
GROUP BY source, unmodelled_field
ORDER BY rows_affected DESC;
