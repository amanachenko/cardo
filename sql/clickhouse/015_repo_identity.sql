-- 015 — repository identity, read (ADR-0035).
--
-- The collector already classifies every repository before storing it
-- (deploy/collector/config.yaml, transform/otel_repo): an organization repository keeps its owner
-- and name, any other becomes `external` plus its host, and a session outside any repository has
-- neither key. The classification was computed, stored, and never read -- the words cardo.repo
-- appeared in no file under sql/ before this migration.
--
-- Hook rows do not carry the repository. The hook receiver never sees the client's git remote
-- (ADR-0035's context), so the repository is a property of the session, taken from the session's
-- OTel rows, and the instruction loads are joined to it through the session id.
--
-- The full URL is never stored, so nothing here can show one: an organization repository is
-- host/owner/name (github.com/acme/widgets) and an external one is its host alone (gitlab.com).
--
-- One row per session: which repository the session worked in, and in what class.
--
-- A session whose repository nobody recorded is `none`: it worked outside any repository, or the
-- client was too old to say (docs/design/data-model.md). The two are not told apart, because the
-- collector cannot tell them apart either -- the key is absent in both cases.
--
-- A repository the collector could not reduce to a host keeps its class and an empty name: it is
-- still external work, and calling it `none` would hide it.
CREATE OR REPLACE VIEW cardo.silver_session_repo AS
SELECT
    s.session_id                                  AS session_id,
    if(empty(r.repo_class), 'none', r.repo_class) AS repo_class,
    r.repo                                        AS repo
FROM
(
    SELECT session_id
    FROM
    (
        SELECT LogAttributes['session.id'] AS session_id
        FROM cardo.bronze_otel_logs
        WHERE LogAttributes['session.id'] != ''

        UNION ALL

        SELECT LogAttributes['session_id'] AS session_id
        FROM cardo.bronze_hook_events
        WHERE LogAttributes['session_id'] != ''
    )
    GROUP BY session_id
) AS s
LEFT JOIN
(
    SELECT
        session_id,
        anyIf(e_repo, e_repo != '')             AS repo,
        anyIf(e_repo_class, e_repo_class != '') AS repo_class
    FROM
    (
        SELECT
            LogAttributes['session.id']            AS session_id,
            ResourceAttributes['cardo.repo']       AS e_repo,
            ResourceAttributes['cardo.repo.class'] AS e_repo_class
        FROM cardo.bronze_otel_logs
        WHERE LogAttributes['session.id'] != ''
    )
    GROUP BY session_id
) AS r ON r.session_id = s.session_id;

-- One row per instructions file that was loaded.
--
-- Two names live here and they mean different things.
--
-- `org_name` is the organization's own artifact (ADR-0026): a version in the name is how a stale
-- copy shows up, so the artifact views read it and show it whatever its count (ADR-0025,
-- ADR-0029). The collector keeps it under CARDO_ORG_ARTIFACTS.
--
-- `name` is a file of a repository: a team's own CLAUDE.md, its rules, its imports. The collector
-- keeps it only when the organization asked for it (CARDO_INSTRUCTIONS_NAMES=project, ADR-0035)
-- and only at project scope, so a person's own files are never named whatever the setting says.
-- It is a separate field on purpose: a team's file is not the organization's artifact, and one
-- field for the two would show a team's file names in the artifact views without the five-person
-- rule.
--
-- The scope comes from what Claude Code sent (memory_type: User, Project, Managed), not from the
-- path: a file name is part of a path, and the path is what INV-5 keeps out of tier 1. An event
-- that says nothing about its scope is `unknown` rather than `project`, because a file whose scope
-- nobody stated is not evidence that a project loaded it.
--
-- A service run is not an engineer (ADR-0036, ADR-0054): its rows are left out here, so the people
-- counts below count people.
--
-- The pseudonym comes from the session, because a hook row reaches a person only through its
-- session's OTel rows (ADR-0029).
CREATE OR REPLACE VIEW cardo.silver_instruction_load AS
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
    r.repo_class                                          AS repo_class,
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
INNER JOIN cardo.silver_session AS s
    ON s.session_id = l.session_id AND s.actor_kind != 'service'
LEFT JOIN cardo.silver_session_repo AS r ON r.session_id = l.session_id;

-- The deployment choice ADR-0035 names: whether the names of project-level instruction files are
-- kept. It lives beside the other two (ADR-0029), so the views read it the same way.
--
-- The view is replaced rather than extended, so the column order changes here and every reader
-- names the columns it wants. Nothing selects these positionally.
CREATE OR REPLACE VIEW cardo.settings_effective AS
SELECT
    greatest(toUInt32(5), toUInt32OrDefault(argMaxIf(value, version, key = 'min_group_size'), toUInt32(5))) AS min_group_size,
    argMaxIf(value, version, key = 'org_artifacts') AS org_artifacts,
    argMaxIf(value, version, key = 'instructions_names') AS instructions_names
FROM cardo.settings;

-- Which repositories have instructions files, and how many.
--
-- Cohort-only (INV-3): people are counted, never selected. The repository label is the
-- organization's own name for its own repositories, and a host for everything else -- which is
-- what ADR-0035 recorded, and the full URL was never stored.
--
-- `instruction_files` is the number of distinct files, which needs the names to tell one file from
-- another. Without them the number is absent rather than zero (ADR-0008's rule: an absent number is
-- honest, a zero is a lie), so it is NULL unless the names are kept. The number of loads is there
-- either way: without the names that is what "how many" can be counted from.
--
-- The names are counted over the rows that carry one, so a mismatch between the setting and what
-- the collector kept shows as an absent number rather than a wrong one.
CREATE OR REPLACE VIEW cardo.gold_repo_instructions AS
WITH
    (SELECT instructions_names FROM cardo.settings_effective) AS names_mode,
    per_repo AS
    (
        SELECT
            toMonday(day)              AS week,
            repo_class,
            repo,
            count(DISTINCT session_id) AS sessions,
            count(DISTINCT pseudonym)  AS people,
            count()                    AS instruction_loads,
            countDistinctIf(
                if(org_name != '', org_name, name),
                if(org_name != '', org_name, name) != '')  AS files
        FROM cardo.silver_instruction_load
        WHERE scope = 'project'
        GROUP BY week, repo_class, repo
    )
SELECT
    week,
    repo_class,
    if(repo_class = 'none', 'none', repo) AS repo,
    sessions,
    people,
    instruction_loads,
    if(names_mode = 'project' AND files > 0, files, CAST(NULL AS Nullable(UInt64))) AS instruction_files
FROM per_repo
ORDER BY week ASC, repo_class ASC, repo ASC;

-- Which instruction files a repository's sessions load, and who loads them.
--
-- Only organization repositories: ADR-0035 keeps the names for every repository but shows them
-- for organization repositories alone.
--
-- Two names meet here. The organization's own artifact name is always shown: the organization
-- chose it, so it identifies nobody (ADR-0029). A team's own file name is shown only once enough
-- people of that repository load it, because a file name one person uses identifies them.
--
-- The rows exist only when the names are kept. Without the setting the collector keeps no
-- project-level file names at all, so a list of a repository's files would omit every one of
-- them: saying nothing is the honest answer.
CREATE OR REPLACE VIEW cardo.gold_repo_instruction_files AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    (SELECT instructions_names FROM cardo.settings_effective) AS names_mode,
    per_file AS
    (
        SELECT
            toMonday(day)       AS week,
            repo,
            if(org_name != '', org_name, name) AS file_key,
            max(org_name != '') AS is_org,
            count(DISTINCT pseudonym) AS file_people
        FROM cardo.silver_instruction_load
        WHERE scope = 'project' AND repo_class = 'org' AND (org_name != '' OR name != '')
        GROUP BY week, repo, file_key
    ),
    usage AS
    (
        SELECT
            toMonday(l.day)                                                     AS week,
            l.repo                                                              AS repo,
            if(n.is_org OR n.file_people >= min_group, n.file_key, 'other')    AS file_name,
            count(DISTINCT pseudonym)                                           AS people,
            count(DISTINCT l.session_id)                                        AS sessions,
            count()                                                             AS loads
        FROM cardo.silver_instruction_load AS l
        INNER JOIN per_file AS n
            ON n.week = toMonday(l.day)
           AND n.repo = l.repo
           AND n.file_key = if(l.org_name != '', l.org_name, l.name)
        WHERE l.scope = 'project' AND l.repo_class = 'org' AND (l.org_name != '' OR l.name != '')
        GROUP BY week, repo, file_name
    )
SELECT
    week,
    repo,
    file_name,
    people,
    sessions,
    loads
FROM usage
WHERE names_mode = 'project'
ORDER BY week ASC, repo ASC, file_name ASC;
