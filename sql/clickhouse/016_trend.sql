-- 016 — trend rows that outlive their rows (ADR-0055).
--
-- ADR-0016 promised that derived aggregates carrying no pseudonym are retained indefinitely, so a
-- trend survives the expiry of the rows behind it. risks.md #10 records that this was promised and
-- not built: every gold mart is a view over bronze, and bronze expires at the window in force.
--
-- This migration adds the aggregate that finally does it, and nothing else. Two tables, one per
-- trend the product has a question about:
--
--   trend_day              day                  how much the fleet did
--   trend_week_artifact    week, kind, name     how adoption moved
--
-- No TTL on any of them. That is the point, and it is a privacy decision with a name on it rather
-- than an accident: a row here is retained past the window because it is not a person's record.
--
-- There is no table by team. Friction is shown by approach, rule or tool, never by team
-- (ADR-0030 decision 4), and a forbidden breakdown in a permanent table outlives the decision that
-- forbids it. The fleet's day minus the named teams' days is the day of the teams too small to
-- name, so per-team rows kept for good give back the small teams by subtraction. Trends of the
-- efficiency measures can follow once their views exist, under their own decision.
--
-- WHY EVERY ROW IS GATED AT THE FLOOR. ADR-0029 says a name one person uses identifies them. A row
-- describing fewer than min_group_size people is personal data whatever its columns are, and
-- personal data expires with the window (ADR-0016). So a row is written only when its own group had
-- at least min_group_size people in its own period, and the published views check the count again
-- at read time against the floor in force now -- which is how a floor raised later still hides
-- history (ADR-0029's rule is "may raise, never lower"). Where one name is split over rows -- an
-- artifact name loaded as more than one file kind -- each row is gated on its own people count
-- too, so no row ever describes fewer than the floor.
--
-- WHY THERE IS NO `other` ROW. The live marts fold every small group into `other`, and its
-- distinct-people count and its wait-time quantiles are computed over the union. Neither is
-- derivable from per-group rows -- a person in two small groups is one person, and a median of
-- medians is not a median -- so a stored `other` could not be re-folded when the floor is raised,
-- and a recomputed one would disagree with the live marts. The trend surface therefore publishes
-- named groups only. A bucket that is wrong is worse than a bucket that is absent.
--
-- WHY THE LABELS ARE NOT STORED. `is_org` and `is_builtin` are computed in the published views from
-- the stored name, exactly as silver_artifact_load computes them. A pattern corrected later has to
-- relabel the past (ADR-0029 rejected freezing the label at ingest for this reason), and a name
-- written into a permanent table is a label whose floor decision would be frozen with it.
--
-- SERVICE RUNS ARE NOT PEOPLE. Every count here leaves them out, as the live views do (ADR-0036,
-- ADR-0054): a file loaded by four people and one CI job is four people.
--
-- The refresh is a REPLACE PARTITION per day (or week), written by `cardo trend`. A refresh that
-- fails partway must leave the previous numbers in place: an empty day on a dashboard looks exactly
-- like a quiet one.

CREATE TABLE IF NOT EXISTS cardo.trend_day
(
    day              Date,
    active_people    UInt64,
    sessions         UInt64,
    prompts          UInt64,
    compactions      UInt64,
    auto_compactions UInt64
)
ENGINE = MergeTree
PARTITION BY (day)
ORDER BY (day);

-- Staging mirrors the table's shape so drift between the two is impossible. Rows live here for
-- seconds; the swap below is what a reader sees.
CREATE TABLE IF NOT EXISTS cardo.trend_day_staging AS cardo.trend_day
ENGINE = MergeTree
PARTITION BY (day)
ORDER BY (day);

-- `name_people` is the count over the whole name for the week, and `people` the count over this
-- row's name and file_kind. ADR-0029's floor is about the name, so it is `name_people` that decides
-- the name; `people` decides this row, because one name loaded as two file kinds splits its people
-- between two rows and each row has to stand for at least min_group_size of them.
CREATE TABLE IF NOT EXISTS cardo.trend_week_artifact
(
    week          Date,
    kind          String,
    artifact      String,
    file_kind     String,
    name_people   UInt64,
    people        UInt64,
    sessions      UInt64,
    uses          UInt64,
    active_people UInt64
)
ENGINE = MergeTree
PARTITION BY (week)
ORDER BY (week, kind, artifact, file_kind);

CREATE TABLE IF NOT EXISTS cardo.trend_week_artifact_staging AS cardo.trend_week_artifact
ENGINE = MergeTree
PARTITION BY (week)
ORDER BY (week, kind, artifact, file_kind);

-- What `cardo trend` writes into the tables above.
--
-- These views read the silver layer, which is where the definitions the gold marts publish already
-- live, so a trend row and a live row are computed from the same thing. They apply the floor in
-- force when they run, so none of them can be read to see what the floor hides. Every session is
-- taken from silver_session with service runs left out, and the joined silver tables carry the
-- same filter, so a CI job is never a person here (ADR-0036).
--
-- Day grain, the fleet as its own group (ADR-0029's last exemption: the fleet is the cohort of last
-- resort). The gate is the fleet's own people count for that day.
CREATE OR REPLACE VIEW cardo.trend_day_source AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sessions AS
    (
        SELECT day AS day,
               count(DISTINCT pseudonym) AS people_total,
               count()                   AS sessions_total
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY day
    ),
    turns AS
    (
        SELECT s.day                            AS day,
               countIf(NOT t.is_builtin_command) AS prompts_total
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session AS s
            ON s.session_id = t.session_id AND s.actor_kind != 'service'
        GROUP BY day
    ),
    compactions AS
    (
        SELECT s.day                                                 AS day,
               countIf(e.kind = 'compaction')                        AS compactions_total,
               countIf(e.kind = 'compaction' AND e.compaction_reason = 'auto') AS auto_compactions_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session AS s
            ON s.session_id = e.session_id AND s.actor_kind != 'service'
        GROUP BY day
    )
SELECT
    s.day                                       AS day,
    s.people_total                              AS active_people,
    s.sessions_total                            AS sessions,
    t.prompts_total                             AS prompts,
    m.compactions_total                         AS compactions,
    m.auto_compactions_total                    AS auto_compactions
FROM sessions AS s
LEFT JOIN turns AS t ON t.day = s.day
LEFT JOIN compactions AS m ON m.day = s.day
WHERE s.people_total >= min_group;

-- Adoption, per week and name. The gate is the name's people count for the week and this row's
-- own people count, so no row describes fewer than the floor.
CREATE OR REPLACE VIEW cardo.trend_week_artifact_source AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    fleet AS
    (
        SELECT toMonday(day) AS week, count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session
        WHERE actor_kind != 'service'
        GROUP BY week
    ),
    per_name AS
    (
        SELECT toMonday(day) AS week, kind AS kind, name AS name,
               count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_artifact_load
        WHERE session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service')
        GROUP BY week, kind, name
    )
SELECT
    toMonday(a.day)                   AS week,
    a.kind                            AS kind,
    a.name                            AS artifact,
    a.file_kind                       AS file_kind,
    n.name_people                     AS name_people,
    count(DISTINCT pseudonym)         AS people,
    count(DISTINCT a.session_id)      AS sessions,
    count()                           AS uses,
    any(f.active_people)              AS active_people
FROM cardo.silver_artifact_load AS a
INNER JOIN per_name AS n ON n.week = toMonday(a.day) AND n.kind = a.kind AND n.name = a.name
LEFT JOIN fleet AS f ON f.week = toMonday(a.day)
WHERE a.session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service')
GROUP BY week, kind, artifact, file_kind, name_people
HAVING name_people >= min_group AND people >= min_group;

-- What is published. Each view re-applies the floor to the stored count, so raising
-- CARDO_MIN_GROUP_SIZE hides history as well as the present. Nothing here folds into `other`
-- (see the header), and nothing here selects a pseudonym.
CREATE OR REPLACE VIEW cardo.gold_trend_day AS
SELECT
    day,
    active_people,
    sessions,
    prompts,
    compactions,
    auto_compactions,
    round(compactions / nullIf(sessions, 0), 4)                  AS compactions_per_session
FROM cardo.trend_day
WHERE active_people >= (SELECT min_group_size FROM cardo.settings_effective)
ORDER BY day ASC;

-- `is_org` and `is_builtin` are decided here, from the stored name, so a corrected pattern
-- relabels the past. `people_share` is the share of that week's active people. The floor is
-- re-applied to both counts: the name's, and this row's own.
CREATE OR REPLACE VIEW cardo.gold_trend_artifact_usage AS
WITH
    (SELECT org_artifacts FROM cardo.settings_effective) AS org_pattern
SELECT
    week,
    kind,
    artifact,
    file_kind,
    (artifact != '' AND (kind = 'instructions'
        OR (org_pattern != '' AND (match(artifact, org_pattern)
            OR (kind = 'mcp' AND match(concat('mcp__', artifact, '__'), org_pattern)))))) AS is_org,
    (kind = 'subagent' AND artifact IN ('general-purpose', 'Explore', 'Plan', 'statusline-setup', 'claude-code-guide'))
        OR (kind = 'skill' AND file_kind = 'builtin')                                    AS is_builtin,
    people,
    sessions,
    uses,
    active_people,
    round(people / nullIf(active_people, 0), 4)                                          AS people_share
FROM cardo.trend_week_artifact
WHERE name_people >= (SELECT min_group_size FROM cardo.settings_effective)
  AND people >= (SELECT min_group_size FROM cardo.settings_effective)
ORDER BY week ASC, kind ASC, people DESC;