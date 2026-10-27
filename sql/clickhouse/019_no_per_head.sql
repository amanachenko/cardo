-- No per-head figures: the fleet's sessions and spend are no longer divided by the people active.
--
-- 003 published sessions_per_active_actor in gold_fleet_adoption and cost_usd_per_active_actor in
-- gold_cost_daily, and the fleet dashboard charted both. Who Cardo serves (ADR-0030, its seventh
-- decision) rules them out: "Cardo computes and shows no per-head ratio". An average per head also
-- hides how the figure spreads: on the made-up organization a few people spend much of the money.
-- The counts stay, each in its own column: active_actors, sessions, cost_usd.
--
-- Each view starts from its last definition: gold_fleet_adoption from 012, which leaves a row
-- without a pseudonym out of the people count (ADR-0036, ADR-0054), and gold_cost_daily from 003.
--
-- The leadership database's copies, made by `cardo gold`, still list the two columns and refuse
-- SELECT * until `cardo gold` runs again. `cardo check` reads each of them whole and says so.

CREATE OR REPLACE VIEW cardo.gold_fleet_adoption AS
WITH totals AS
(
    SELECT
        day,
        source,
        countDistinctIf(pseudonym, pseudonym != '') AS active_actors,
        sum(sessions)                               AS sessions_total,
        sum(lines_added)                            AS lines_added_total,
        sum(lines_removed)                          AS lines_removed_total,
        sum(commits)                                AS commits_total,
        sum(pull_requests)                          AS pull_requests_total
    FROM cardo.silver_actor_day
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    sessions_total      AS sessions,
    lines_added_total   AS lines_added,
    lines_removed_total AS lines_removed,
    commits_total       AS commits,
    pull_requests_total AS pull_requests
FROM totals
ORDER BY day;

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
    round(cost_usd_total, 2) AS cost_usd
FROM totals
ORDER BY day;
