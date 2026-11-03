-- Team reach, against the whole organization (migration 014).
--
-- ADR-0030 decides that teams are compared on reach, never ranked: "adoption and version reach are
-- shown by team, against the fleet as a whole, and never sorted into a league table". This view is
-- that reach. It answers "how far did this reach inside this team", with the organization's own
-- numbers beside it, so a team is read against the whole and never against another team.
--
-- Four rules shape it, and each one is a rule somewhere else:
--
--   - Nothing is divided by headcount. ADR-0030's seventh decision says Cardo computes and shows no
--     per-head ratio, so this view carries counts and shares of people, and no "per engineer".
--   - A team below the floor is not rendered at all. ADR-0046 says a team with fewer than
--     min_group_size people is not rendered, because "1 person" is a person. The floor is the
--     team's ACTIVE people that week -- the people with a session, not counting service runs --
--     not everyone the directory lists: a team of six where one person had a session is one
--     person's week, and a leader can read every gold view. The same floor applies to the
--     artifact names inside a team, as it does everywhere else (ADR-0029).
--   - A service run is not a person. It stays out of every people count (ADR-0036, ADR-0054).
--   - One meaning of "team". ADR-0034 decision 3 has teams come from a directory file, falling
--     back to the bundle label. silver_session_team is that rule, per session, in one place: the
--     directory's team when the person is listed for the salt in force, the session's own label
--     otherwise. Without it a person can sit in two different teams on two pages, because the
--     weekly dashboards read the label and the directory was read here.
--
-- There is no ordering that ranks teams: rows come out by week and then by artifact, and the
-- dashboard sorts. Nothing here compares one team to another.
--
-- The three views below are one thing split in layers, because of INV-3. Gold is the layer a
-- dashboard reads, and a pseudonym may be counted there but never selected or compared -- a
-- per-person row is a ranking with one rename to go. Joining a session to its team needs the
-- pseudonym on both sides, so that join lives in the silver layer, where a pseudonym is what the
-- layer is for. The gold view then counts and publishes, and publishes no person.

-- Which team a session belongs to: the directory's team when the person is listed, the session's
-- own label otherwise (ADR-0034 decision 3).
--
-- The join is on pseudonym and salt version: a directory loaded with one salt does not attribute
-- sessions hashed with another (ADR-0046). Team names are organization structure, so they are
-- visible; the counts under a small team are not. A service run has no row, so no view counting
-- people reads it (ADR-0036). A session with no label at all is `unassigned`, the word the weekly
-- views use for it.
CREATE OR REPLACE VIEW cardo.silver_session_team AS
WITH
    current_batch AS
    (
        SELECT batch
        FROM cardo.settings_teams_batches
        ORDER BY loaded_at DESC
        LIMIT 1
    ),
    members AS
    (
        SELECT t.team AS team, t.pseudonym AS pseudonym, t.salt_version AS salt_version
        FROM cardo.settings_teams AS t
        INNER JOIN current_batch AS b ON t.batch = b.batch
    )
SELECT
    s.session_id AS session_id,
    toMonday(s.day) AS week,
    if(m.team != '', m.team, if(s.cohort != '', s.cohort, 'unassigned')) AS team
FROM cardo.silver_session AS s
LEFT JOIN members AS m
    ON m.pseudonym = s.pseudonym AND m.salt_version = s.salt_version
WHERE s.actor_kind != 'service';

-- The per-person layer: one row per artifact load, with the team it belongs to.
--
-- The team comes from silver_session_team, so the directory's team and the bundle label mean the
-- same thing here as everywhere else. The name of the artifact is carried as the collector wrote
-- it: the gold view applies the floor to the name at read time, so a corrected pattern relabels
-- the past.
CREATE OR REPLACE VIEW cardo.silver_team_artifact_use AS
SELECT
    t.week                   AS week,
    t.team                   AS team,
    a.kind                   AS kind,
    a.name                   AS name,
    a.is_org                 AS is_org,
    a.is_builtin             AS is_builtin,
    a.pseudonym              AS pseudonym,
    a.session_id             AS session_id,
    a.day                    AS day
FROM cardo.silver_artifact_load AS a
INNER JOIN cardo.silver_session_team AS t
    ON t.session_id = a.session_id;

-- What a dashboard reads: one row per week, team and artifact.
--
-- `people` is the distinct people in that team who used the artifact that week; `uses` is the
-- number of loads. `team_reach` is the share of the team's active people that week who used it,
-- and `org_reach` is the same share for the whole organization -- the same formula as
-- gold_artifact_usage.people_share, so the two read alike. The share inside the team is what
-- "reach inside this team" means: against the organization as a denominator, a team of five where
-- everyone uses something reads as half of a team of fifty where ten do. Both denominators are
-- published beside the shares, so a rate never stands without its volume.
CREATE OR REPLACE VIEW cardo.gold_team_reach AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    fleet AS
    (
        SELECT toMonday(day) AS week, count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY week
    ),
    team_week AS
    (
        SELECT
            t.week AS week,
            t.team AS team,
            count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session_team AS t
        INNER JOIN cardo.silver_session AS s
            ON s.session_id = t.session_id
        GROUP BY week, team
    ),
    -- The artifact name obeys the same floor as everywhere else: a name fewer than
    -- min_group_size people used that week is "other".
    per_name AS
    (
        SELECT week, kind, name, count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_team_artifact_use
        GROUP BY week, kind, name
    ),
    usage AS
    (
        SELECT
            u.week                                                       AS week,
            u.team                                                       AS team,
            u.kind                                                       AS kind,
            if(u.is_org OR u.is_builtin OR u.name = '' OR n.name_people >= min_group,
               u.name, 'other')                                          AS artifact,
            count(DISTINCT pseudonym)                                    AS people_total,
            count()                                                      AS uses_total
        FROM cardo.silver_team_artifact_use AS u
        INNER JOIN per_name AS n
            ON n.week = u.week AND n.kind = u.kind AND n.name = u.name
        GROUP BY week, team, kind, artifact
    ),
    org_usage AS
    (
        SELECT
            toMonday(a.day)                                              AS week,
            a.kind                                                       AS kind,
            if(a.is_org OR a.is_builtin OR a.name = '' OR n.name_people >= min_group,
               a.name, 'other')                                          AS artifact,
            count(DISTINCT pseudonym)                                    AS people_total
        FROM cardo.silver_artifact_load AS a
        INNER JOIN per_name AS n
            ON n.week = toMonday(a.day) AND n.kind = a.kind AND n.name = a.name
        WHERE a.session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service')
        GROUP BY week, kind, artifact
    )
SELECT
    u.week                                                         AS week,
    u.team                                                         AS team,
    u.kind                                                         AS kind,
    u.artifact                                                     AS artifact,
    u.people_total                                                 AS people,
    u.uses_total                                                   AS uses,
    t.active_people                                                AS team_active_people,
    f.active_people                                                AS fleet_people,
    round(u.people_total / nullIf(t.active_people, 0), 4)          AS team_reach,
    round(o.people_total / nullIf(f.active_people, 0), 4)          AS org_reach
FROM usage AS u
INNER JOIN team_week AS t
    ON t.week = u.week AND t.team = u.team
LEFT JOIN fleet AS f
    ON f.week = u.week
LEFT JOIN org_usage AS o
    ON o.week = u.week AND o.kind = u.kind AND o.artifact = u.artifact
WHERE t.active_people >= min_group
ORDER BY u.week ASC, u.artifact ASC, u.team ASC;