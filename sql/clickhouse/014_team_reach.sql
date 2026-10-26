-- Team reach, against the whole organization (migration 014).
--
-- ADR-0030 decides that teams are compared on reach, never ranked: "adoption and version reach are
-- shown by team, against the fleet as a whole, and never sorted into a league table". This view is
-- that reach. It answers "how far did this reach inside this team", with the organization's own
-- numbers beside it, so a team is read against the whole and never against another team.
--
-- Three rules shape it, and each one is a rule somewhere else:
--
--   - Nothing is divided by headcount. ADR-0030's seventh decision says Cardo computes and shows no
--     per-head ratio, so this view carries counts and shares of the fleet, and no "per engineer".
--   - A team below the floor is not rendered at all. ADR-0046 says a team with fewer than
--     min_group_size people is not rendered, because "1 person" is a person. The same floor applies
--     to the artifact names inside a team, as it does everywhere else (ADR-0029).
--   - A service run is not a person. It stays out of every people count (ADR-0036, ADR-0054).
--
-- There is no ordering that ranks teams: rows come out by week and then by artifact, and the
-- dashboard sorts. Nothing here compares one team to another.
--
-- The two views below are one thing split in two, because of INV-3. Gold is the layer a dashboard
-- reads, and a pseudonym may be counted there but never selected or compared — a per-person row is
-- a ranking with one rename to go. Joining a session to its team needs the pseudonym on both sides,
-- so that join lives in the silver layer, where a pseudonym is what the layer is for. The gold view
-- then counts and publishes, and publishes no person.

-- The per-person layer: one row per artifact load, with the team it belongs to.
--
-- The join is on pseudonym and salt version: a directory loaded with one salt does not attribute
-- sessions hashed with another (ADR-0046). Team names are organization structure, so they are
-- visible; the counts under a small team are not.
--
-- team_people is the size of the team in the directory, carried on every row of it, so the gold
-- view can apply the floor without counting people there.
CREATE OR REPLACE VIEW cardo.silver_team_artifact_use AS
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
    ),
    sized AS
    (
        SELECT team, count(DISTINCT pseudonym) AS people_total
        FROM members
        GROUP BY team
    )
SELECT
    toMonday(a.day)          AS week,
    m.team                   AS team,
    st.people_total          AS team_people,
    a.kind                   AS kind,
    a.name                   AS name,
    a.is_org                 AS is_org,
    a.is_builtin             AS is_builtin,
    a.pseudonym              AS pseudonym,
    a.session_id             AS session_id,
    a.day                    AS day
FROM cardo.silver_artifact_load AS a
INNER JOIN cardo.silver_session AS s
    ON s.session_id = a.session_id AND s.actor_kind != 'service'
INNER JOIN members AS m
    ON m.pseudonym = a.pseudonym AND m.salt_version = s.salt_version
INNER JOIN sized AS st
    ON st.team = m.team;

-- What a dashboard reads: one row per week, team and artifact.
--
-- `people` is the distinct people in that team who used the artifact that week; `uses` is the
-- number of loads. `fleet_people` is the whole organization's active people that week, and
-- `team_share` is this team's share of them — the reach of the artifact in this team against the
-- organization as a whole. The organization is the denominator for every team alike, which is what
-- keeps this from being a league table.
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
    -- The artifact name obeys the same floor as everywhere else: a name fewer than
    -- min_group_size people used that week is "other".
    per_name AS
    (
        SELECT week, kind, name, count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_team_artifact_use
        GROUP BY week, kind, name
    ),
    sized AS
    (
        SELECT team, max(team_people) AS team_people
        FROM cardo.silver_team_artifact_use
        GROUP BY team
    ),
    usage AS
    (
        SELECT
            u.week                                                       AS week,
            u.team                                                       AS team,
            u.kind                                                       AS kind,
            if(u.is_org OR u.is_builtin OR u.name = '' OR n.name_people >= min_group,
               u.name, 'other')                                          AS artifact,
            count(DISTINCT pseudonym)                                   AS people_total,
            count()                                                   AS uses_total
        FROM cardo.silver_team_artifact_use AS u
        INNER JOIN per_name AS n
            ON n.week = u.week AND n.kind = u.kind AND n.name = u.name
        GROUP BY week, team, kind, artifact
    )
SELECT
    u.week                                                         AS week,
    u.team                                                         AS team,
    u.kind                                                         AS kind,
    u.artifact                                                     AS artifact,
    u.people_total                                                 AS people,
    u.uses_total                                                   AS uses,
    f.active_people                                                AS fleet_people,
    round(u.people_total / nullIf(f.active_people, 0), 4)         AS team_share
FROM usage AS u
INNER JOIN sized AS s
    ON s.team = u.team
LEFT JOIN fleet AS f
    ON f.week = u.week
WHERE s.team_people >= min_group
ORDER BY u.week ASC, u.artifact ASC, u.team ASC;
