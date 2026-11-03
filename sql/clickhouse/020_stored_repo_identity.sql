-- 020 — the repository identity silver views are stored, and rebuilt every 10 minutes (ADR-0052).
--
-- 015 defined silver_session_repo and silver_instruction_load as plain views over raw rows, so
-- every read of gold_repo_instructions and gold_repo_instruction_files worked them out again from
-- bronze -- the pattern 017 removed for the seven session-level silver views, and the one whose
-- cost put six of the made-up organization's 13 panels past Grafana's time limit (risks.md #14).
-- This migration stores the two, the way 017 stores the rest.
--
-- WHY THIS IS ITS OWN MIGRATION. On a fresh install the runner applies 015 before 017, and
-- ClickHouse resolves a view's sources when the view is created: a stored table built from
-- silver_session_live would fail at 015's place in the chain. 020 is applied after 017 in both
-- install orders, so its _live views can read the _live names and a rebuild starts from the raw
-- tables -- 017's header rule, "no stored table reads another", kept exactly.
--
-- The bodies are 015's definitions with the _live names: silver_session_live for the session list,
-- and silver_session_repo_live for the repository join. Silver_instruction_load's refresher runs
-- before silver_session_repo_refresh alphabetically, which is only safe because neither reads a
-- stored table.
--
-- A table takes its columns from its _live view (EMPTY AS SELECT creates it with no rows), so the
-- two match when this file runs. Rows are sorted by day, which every reader filters or groups on,
-- and each table expires its rows 90 days after their day as the raw tables do; internal/retention
-- moves that expiry with theirs. The first rebuild starts as soon as each refresher is created.
--
-- RE-RUNNABLE. The runner applies a migration again if a statement fails partway. DROP TABLE drops
-- the 015 plain view of the same name as readily as a table, and the refreshers go first, so a
-- second run of this file is a no-op over a working state.
--
-- The gold views are unchanged: they read the two names, and the names now sit on stored tables.

DROP TABLE IF EXISTS cardo.silver_session_repo_refresh;
DROP TABLE IF EXISTS cardo.silver_session_repo;
CREATE OR REPLACE VIEW cardo.silver_session_repo_live AS
SELECT
    s.session_id                                  AS session_id,
    s.day                                         AS day,
    if(empty(r.repo_class), 'none', r.repo_class) AS repo_class,
    r.repo                                        AS repo
FROM cardo.silver_session_live AS s
LEFT JOIN
(
    SELECT
        session_id,
        anyIf(e_repo, e_repo != '')             AS repo,
        anyIf(e_repo_class, e_repo_class != '') AS repo_class
    FROM
    (
        SELECT
            LogAttributes['session.id']                AS session_id,
            if(LogAttributes['cardo.repo'] != '', LogAttributes['cardo.repo'],
               ResourceAttributes['cardo.repo'])       AS e_repo,
            if(LogAttributes['cardo.repo.class'] != '', LogAttributes['cardo.repo.class'],
               ResourceAttributes['cardo.repo.class']) AS e_repo_class
        FROM cardo.bronze_otel_logs
        WHERE LogAttributes['session.id'] != ''
    )
    GROUP BY session_id
) AS r ON r.session_id = s.session_id;
CREATE TABLE cardo.silver_session_repo
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_session_repo_live;
CREATE MATERIALIZED VIEW cardo.silver_session_repo_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_session_repo
AS SELECT * FROM cardo.silver_session_repo_live;

DROP TABLE IF EXISTS cardo.silver_instruction_load_refresh;
DROP TABLE IF EXISTS cardo.silver_instruction_load;
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
    s.pseudonym                                           AS pseudonym,
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
) AS l
INNER JOIN cardo.silver_session_live AS s
    ON s.session_id = l.session_id AND s.actor_kind != 'service'
LEFT JOIN cardo.silver_session_repo_live AS r ON r.session_id = l.session_id;
CREATE TABLE cardo.silver_instruction_load
ENGINE = MergeTree ORDER BY (day, session_id)
TTL day + INTERVAL 90 DAY DELETE
EMPTY AS SELECT * FROM cardo.silver_instruction_load_live;
CREATE MATERIALIZED VIEW cardo.silver_instruction_load_refresh
REFRESH EVERY 10 MINUTE TO cardo.silver_instruction_load
AS SELECT * FROM cardo.silver_instruction_load_live;