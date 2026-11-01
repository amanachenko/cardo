-- 021_gold_both_paths.sql
--
-- The daily-aggregate family -- fleet adoption, daily spend, model mix, tool acceptance -- read
-- only the Admin API path (silver_actor_day over bronze_actor_day), while the store holds two
-- ingestion paths and says so on the overview itself. A stack without an admin key then shows
-- empty money and adoption charts beside real sessions in the store: "what does each cost" has
-- numbers in one dashboard and none in the other.
--
-- This migration derives the same daily aggregates from the sessions path (silver_session,
-- silver_api_request, silver_tool_call) and unions both into what the gold views read. Three
-- rules, each of them the point:
--
--   1. THE ADMIN ROW WINS THE DAY. When the ledger has a person's day, the derived rows for that
--      (day, person) are suppressed (LEFT ANTI JOIN). The ledger is the billing record; counting
--      a day twice is how "what does each cost" becomes a lie.
--   2. WHAT ONLY THE LEDGER KNOWS STAYS EMPTY. Lines, commits and pull requests exist on the
--      admin path alone. Derived rows carry zeros, and the charts show zeros -- inventing them
--      from sessions would answer a question nobody asked the data.
--   3. THE TWO PATHS STAY SEPARABLE. Derived rows carry source = 'sessions', so a chart can
--      still split what the ledger says from what the sessions say.
--
-- The floor is untouched: gold still exposes no per-person rows (INV-3), and the enablement
-- views' rollups below min_group_size are not in this file's scope.

-- One row per person and day from the sessions path. Service runs are not people and stay out.
CREATE OR REPLACE VIEW cardo.silver_actor_day_sessions AS
WITH
    sessions AS
    (
        SELECT
            day,
            pseudonym,
            uniqExact(session_id) AS n
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
          AND pseudonym IS NOT NULL
          AND pseudonym != ''
        GROUP BY day, pseudonym
    ),
    edits AS
    (
        SELECT
            day,
            pseudonym,
            countIf(tool_name = 'Edit' AND decision != 'reject')         AS edit_accepted,
            countIf(tool_name = 'Edit' AND decision = 'reject')          AS edit_rejected,
            countIf(tool_name = 'MultiEdit' AND decision != 'reject')    AS multi_edit_accepted,
            countIf(tool_name = 'MultiEdit' AND decision = 'reject')     AS multi_edit_rejected,
            countIf(tool_name = 'Write' AND decision != 'reject')        AS write_accepted,
            countIf(tool_name = 'Write' AND decision = 'reject')         AS write_rejected,
            countIf(tool_name = 'NotebookEdit' AND decision != 'reject') AS notebook_accepted,
            countIf(tool_name = 'NotebookEdit' AND decision = 'reject')  AS notebook_rejected
        FROM cardo.silver_tool_call
        WHERE is_edit
          AND pseudonym IS NOT NULL
          AND pseudonym != ''
        GROUP BY day, pseudonym
    )
SELECT
    'sessions'                                          AS source,
    s.day                                               AS day,
    s.pseudonym                                         AS pseudonym,
    toInt64(s.n)                                        AS sessions,
    toInt64(0)                                          AS lines_added,
    toInt64(0)                                          AS lines_removed,
    toInt64(0)                                          AS commits,
    toInt64(0)                                          AS pull_requests,
    toInt64(coalesce(e.edit_accepted, 0))               AS edit_accepted,
    toInt64(coalesce(e.edit_rejected, 0))               AS edit_rejected,
    toInt64(coalesce(e.multi_edit_accepted, 0))         AS multi_edit_accepted,
    toInt64(coalesce(e.multi_edit_rejected, 0))         AS multi_edit_rejected,
    toInt64(coalesce(e.write_accepted, 0))              AS write_accepted,
    toInt64(coalesce(e.write_rejected, 0))              AS write_rejected,
    toInt64(coalesce(e.notebook_accepted, 0))           AS notebook_accepted,
    toInt64(coalesce(e.notebook_rejected, 0))           AS notebook_rejected
FROM sessions AS s
LEFT JOIN edits AS e ON (e.day = s.day) AND (e.pseudonym = s.pseudonym);

-- One row per person, day and model from the sessions path.
CREATE OR REPLACE VIEW cardo.silver_actor_day_model_sessions AS
SELECT
    'sessions'                            AS source,
    day,
    pseudonym,
    model,
    toInt64(sum(input_tokens))            AS tokens_input,
    toInt64(sum(output_tokens))           AS tokens_output,
    toInt64(sum(cache_read_tokens))       AS tokens_cache_read,
    toInt64(sum(cache_creation_tokens))   AS tokens_cache_creation,
    round(sum(cost_usd), 6)               AS cost_usd
FROM cardo.silver_api_request
WHERE pseudonym IS NOT NULL
  AND pseudonym != ''
  AND model != ''
GROUP BY day, pseudonym, model;

-- Both paths, admin first: a (day, person) the ledger has is never counted twice.
CREATE OR REPLACE VIEW cardo.silver_actor_day_both AS
SELECT
    source, day, pseudonym, sessions,
    lines_added, lines_removed, commits, pull_requests,
    edit_accepted, edit_rejected, multi_edit_accepted, multi_edit_rejected,
    write_accepted, write_rejected, notebook_accepted, notebook_rejected
FROM cardo.silver_actor_day
UNION ALL
SELECT
    source, day, pseudonym, sessions,
    lines_added, lines_removed, commits, pull_requests,
    edit_accepted, edit_rejected, multi_edit_accepted, multi_edit_rejected,
    write_accepted, write_rejected, notebook_accepted, notebook_rejected
FROM cardo.silver_actor_day_sessions AS d
LEFT ANTI JOIN
    (SELECT DISTINCT day, pseudonym FROM cardo.silver_actor_day) AS a
    ON (a.day = d.day) AND (a.pseudonym = d.pseudonym);

CREATE OR REPLACE VIEW cardo.silver_actor_day_model_both AS
SELECT source, day, pseudonym, model,
       tokens_input, tokens_output, tokens_cache_read, tokens_cache_creation, cost_usd
FROM cardo.silver_actor_day_model
UNION ALL
SELECT source, day, pseudonym, model,
       tokens_input, tokens_output, tokens_cache_read, tokens_cache_creation, cost_usd
FROM cardo.silver_actor_day_model_sessions AS d
LEFT ANTI JOIN
    (SELECT DISTINCT day, pseudonym FROM cardo.silver_actor_day) AS a
    ON (a.day = d.day) AND (a.pseudonym = d.pseudonym);

-- The four gold views, their shapes unchanged, now over both paths. Each body is the last
-- definition's, with only the source view changed: 019's fleet adoption and cost daily, and
-- 003's tool acceptance and model mix.

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
    FROM cardo.silver_actor_day_both
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
    FROM cardo.silver_actor_day_model_both
    GROUP BY day, source
)
SELECT
    day,
    source,
    active_actors,
    round(cost_usd_total, 2) AS cost_usd
FROM totals
ORDER BY day;

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
    FROM cardo.silver_actor_day_both
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
    FROM cardo.silver_actor_day_model_both
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