-- 016 — trend rows that outlive their rows (ADR-0055).
--
-- ADR-0016 promised that derived aggregates carrying no pseudonym are retained indefinitely, so a
-- trend survives the expiry of the rows behind it. risks.md #10 records that this was promised and
-- not built: every gold mart is a view over bronze, and bronze expires at the window in force.
--
-- This migration adds the aggregate that finally does it, and nothing else. Three tables, one per
-- trend the product has a question about:
--
--   trend_day              day                  how much the fleet did, and what it cost it
--   trend_day_cohort       day, cohort          how friction moved for a team
--   trend_week_artifact    week, kind, name     how adoption moved
--
-- No TTL on any of them. That is the point, and it is a privacy decision with a name on it rather
-- than an accident: a row here is retained past the window because it is not a person's record.
--
-- WHY EVERY ROW IS GATED AT THE FLOOR. ADR-0029 says a name one person uses identifies them, and
-- that a two-person team's friction chart is two people's friction. A row describing fewer than
-- min_group_size people is personal data whatever its columns are, and personal data expires with
-- the window (ADR-0016). So a row is written only when its own group had at least min_group_size
-- people in its own period, and the published views check the count again at read time against the
-- floor in force now -- which is how a floor raised later still hides history (ADR-0029's rule is
-- "may raise, never lower").
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
-- The cohort column holds the raw label, including the empty one for a session nobody assigned a
-- team to. It is not mapped to `unassigned` here: the mapping happens in the published view, so two
-- raw groups are never merged before the floor is applied. Merging first would publish a group of
-- five assembled from a group of two and a group of three, which the live marts correctly hide.
--
-- The refresh is a REPLACE PARTITION per day (or week), written by `cardo trend`. A refresh that
-- fails partway must leave the previous numbers in place: an empty day on a dashboard looks exactly
-- like a quiet one.

CREATE TABLE IF NOT EXISTS cardo.trend_day
(
    day                           Date,
    active_people                 UInt64,
    sessions                      UInt64,
    prompts                       UInt64,
    edit_proposals                UInt64,
    edit_rejections               UInt64,
    permission_prompts            UInt64,
    permission_answered           UInt64,
    permission_prompts_two_clocks UInt64,
    permission_wait_ms_total      UInt64,
    permission_wait_ms_p50        Float64,
    permission_wait_ms_p90        Float64,
    compactions                   UInt64,
    auto_compactions              UInt64
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

CREATE TABLE IF NOT EXISTS cardo.trend_day_cohort
(
    day                           Date,
    cohort                        String,
    active_people                 UInt64,
    sessions                      UInt64,
    prompts                       UInt64,
    edit_proposals                UInt64,
    edit_rejections               UInt64,
    permission_prompts            UInt64,
    permission_answered           UInt64,
    permission_prompts_two_clocks UInt64,
    permission_wait_ms_total      UInt64,
    permission_wait_ms_p50        Float64,
    permission_wait_ms_p90        Float64,
    compactions                   UInt64,
    auto_compactions              UInt64
)
ENGINE = MergeTree
PARTITION BY (day)
ORDER BY (day, cohort);

CREATE TABLE IF NOT EXISTS cardo.trend_day_cohort_staging AS cardo.trend_day_cohort
ENGINE = MergeTree
PARTITION BY (day)
ORDER BY (day, cohort);

-- `name_people` is the count over the whole name for the week, and `people` the count over this
-- row's name and file_kind. ADR-0029's floor is about the name, so it is `name_people` that decides
-- whether the row exists at all; `people` is what is published. The two differ only when one name
-- is loaded as two file kinds in the same week.
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
-- force when they run, so none of them can be read to see what the floor hides.
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
        GROUP BY day
    ),
    turns AS
    (
        SELECT s.day                            AS day,
               countIf(NOT t.is_builtin_command) AS prompts_total
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session AS s ON s.session_id = t.session_id
        GROUP BY day
    ),
    edits AS
    (
        SELECT s.day                                     AS day,
               countIf(k.is_edit AND k.decision != '')   AS edit_proposals_total,
               countIf(k.is_edit AND k.decision = 'reject') AS edit_rejections_total
        FROM cardo.silver_tool_call AS k
        INNER JOIN cardo.silver_session AS s ON s.session_id = k.session_id
        GROUP BY day
    ),
    permissions AS
    (
        SELECT s.day                              AS day,
               count()                            AS prompts_total,
               countIf(p.answered)                AS answered_total,
               countIf(p.clock = 'two_clocks')    AS two_clocks_total,
               sumIf(p.wait_ms, p.answered)       AS wait_ms_total,
               quantileExactIf(0.5)(p.wait_ms, p.answered) AS wait_ms_p50,
               quantileExactIf(0.9)(p.wait_ms, p.answered) AS wait_ms_p90
        FROM cardo.silver_policy_decision AS p
        INNER JOIN cardo.silver_session AS s ON s.session_id = p.session_id
        GROUP BY day
    ),
    compactions AS
    (
        SELECT s.day                                                 AS day,
               countIf(e.kind = 'compaction')                        AS compactions_total,
               countIf(e.kind = 'compaction' AND e.compaction_reason = 'auto') AS auto_compactions_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session AS s ON s.session_id = e.session_id
        GROUP BY day
    )
SELECT
    s.day                                       AS day,
    s.people_total                              AS active_people,
    s.sessions_total                            AS sessions,
    t.prompts_total                             AS prompts,
    e.edit_proposals_total                      AS edit_proposals,
    e.edit_rejections_total                     AS edit_rejections,
    p.prompts_total                             AS permission_prompts,
    p.answered_total                            AS permission_answered,
    p.two_clocks_total                          AS permission_prompts_two_clocks,
    p.wait_ms_total                             AS permission_wait_ms_total,
    p.wait_ms_p50                               AS permission_wait_ms_p50,
    p.wait_ms_p90                               AS permission_wait_ms_p90,
    m.compactions_total                         AS compactions,
    m.auto_compactions_total                    AS auto_compactions
FROM sessions AS s
LEFT JOIN turns AS t ON t.day = s.day
LEFT JOIN edits AS e ON e.day = s.day
LEFT JOIN permissions AS p ON p.day = s.day
LEFT JOIN compactions AS m ON m.day = s.day
WHERE s.people_total >= min_group;

-- The same, per cohort. The gate is the cohort's own people count for that day, which is the
-- number ADR-0029's floor is defined on.
CREATE OR REPLACE VIEW cardo.trend_day_cohort_source AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    sessions AS
    (
        SELECT day AS day, cohort AS cohort,
               count(DISTINCT pseudonym) AS people_total,
               count()                   AS sessions_total
        FROM cardo.silver_session
        GROUP BY day, cohort
    ),
    turns AS
    (
        SELECT s.day                            AS day, s.cohort AS cohort,
               countIf(NOT t.is_builtin_command) AS prompts_total
        FROM cardo.silver_turn AS t
        INNER JOIN cardo.silver_session AS s ON s.session_id = t.session_id
        GROUP BY day, cohort
    ),
    edits AS
    (
        SELECT s.day                                     AS day, s.cohort AS cohort,
               countIf(k.is_edit AND k.decision != '')   AS edit_proposals_total,
               countIf(k.is_edit AND k.decision = 'reject') AS edit_rejections_total
        FROM cardo.silver_tool_call AS k
        INNER JOIN cardo.silver_session AS s ON s.session_id = k.session_id
        GROUP BY day, cohort
    ),
    permissions AS
    (
        SELECT s.day                              AS day, s.cohort AS cohort,
               count()                            AS prompts_total,
               countIf(p.answered)                AS answered_total,
               countIf(p.clock = 'two_clocks')    AS two_clocks_total,
               sumIf(p.wait_ms, p.answered)       AS wait_ms_total,
               quantileExactIf(0.5)(p.wait_ms, p.answered) AS wait_ms_p50,
               quantileExactIf(0.9)(p.wait_ms, p.answered) AS wait_ms_p90
        FROM cardo.silver_policy_decision AS p
        INNER JOIN cardo.silver_session AS s ON s.session_id = p.session_id
        GROUP BY day, cohort
    ),
    compactions AS
    (
        SELECT s.day                                                 AS day, s.cohort AS cohort,
               countIf(e.kind = 'compaction')                        AS compactions_total,
               countIf(e.kind = 'compaction' AND e.compaction_reason = 'auto') AS auto_compactions_total
        FROM cardo.silver_context_event AS e
        INNER JOIN cardo.silver_session AS s ON s.session_id = e.session_id
        GROUP BY day, cohort
    )
SELECT
    s.day                                       AS day,
    s.cohort                                    AS cohort,
    s.people_total                              AS active_people,
    s.sessions_total                            AS sessions,
    t.prompts_total                             AS prompts,
    e.edit_proposals_total                      AS edit_proposals,
    e.edit_rejections_total                     AS edit_rejections,
    p.prompts_total                             AS permission_prompts,
    p.answered_total                            AS permission_answered,
    p.two_clocks_total                          AS permission_prompts_two_clocks,
    p.wait_ms_total                             AS permission_wait_ms_total,
    p.wait_ms_p50                               AS permission_wait_ms_p50,
    p.wait_ms_p90                               AS permission_wait_ms_p90,
    m.compactions_total                         AS compactions,
    m.auto_compactions_total                    AS auto_compactions
FROM sessions AS s
LEFT JOIN turns AS t ON t.day = s.day AND t.cohort = s.cohort
LEFT JOIN edits AS e ON e.day = s.day AND e.cohort = s.cohort
LEFT JOIN permissions AS p ON p.day = s.day AND p.cohort = s.cohort
LEFT JOIN compactions AS m ON m.day = s.day AND m.cohort = s.cohort
WHERE s.people_total >= min_group;

-- Adoption, per week and name. The gate is the name's people count for the week, which is the
-- number gold_artifact_usage's floor is defined on.
CREATE OR REPLACE VIEW cardo.trend_week_artifact_source AS
WITH
    (SELECT min_group_size FROM cardo.settings_effective) AS min_group,
    fleet AS
    (
        SELECT toMonday(day) AS week, count(DISTINCT pseudonym) AS active_people
        FROM cardo.silver_session
        GROUP BY week
    ),
    per_name AS
    (
        SELECT toMonday(day) AS week, kind AS kind, name AS name,
               count(DISTINCT pseudonym) AS name_people
        FROM cardo.silver_artifact_load
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
GROUP BY week, kind, artifact, file_kind, name_people
HAVING name_people >= min_group;

-- What is published. Each view re-applies the floor to the stored count, so raising
-- CARDO_MIN_GROUP_SIZE hides history as well as the present. Nothing here folds into `other`
-- (see the header), and nothing here selects a pseudonym.
CREATE OR REPLACE VIEW cardo.gold_trend_day AS
SELECT
    day,
    active_people,
    sessions,
    prompts,
    edit_proposals,
    edit_rejections,
    round(edit_rejections / nullIf(edit_proposals, 0), 4)        AS edit_rejection_rate,
    permission_prompts,
    permission_answered,
    permission_prompts_two_clocks,
    round(permission_wait_ms_total / 1000, 1)                    AS permission_wait_s_total,
    round(permission_wait_ms_p50 / 1000, 1)                      AS permission_wait_s_p50,
    round(permission_wait_ms_p90 / 1000, 1)                      AS permission_wait_s_p90,
    round(permission_prompts / nullIf(prompts, 0), 4)            AS permission_prompts_per_prompt,
    compactions,
    auto_compactions,
    round(compactions / nullIf(sessions, 0), 4)                  AS compactions_per_session
FROM cardo.trend_day
WHERE active_people >= (SELECT min_group_size FROM cardo.settings_effective)
ORDER BY day ASC;

CREATE OR REPLACE VIEW cardo.gold_trend_day_cohort AS
SELECT
    day,
    if(cohort = '', 'unassigned', cohort)                         AS cohort,
    active_people,
    sessions,
    prompts,
    edit_proposals,
    edit_rejections,
    round(edit_rejections / nullIf(edit_proposals, 0), 4)        AS edit_rejection_rate,
    permission_prompts,
    permission_answered,
    permission_prompts_two_clocks,
    round(permission_wait_ms_total / 1000, 1)                    AS permission_wait_s_total,
    round(permission_wait_ms_p50 / 1000, 1)                      AS permission_wait_s_p50,
    round(permission_wait_ms_p90 / 1000, 1)                      AS permission_wait_s_p90,
    round(permission_prompts / nullIf(prompts, 0), 4)            AS permission_prompts_per_prompt,
    compactions,
    auto_compactions,
    round(compactions / nullIf(sessions, 0), 4)                  AS compactions_per_session
FROM cardo.trend_day_cohort
WHERE active_people >= (SELECT min_group_size FROM cardo.settings_effective)
ORDER BY day ASC, cohort ASC;

-- `is_org` and `is_builtin` are decided here, from the stored name, so a corrected pattern
-- relabels the past. `people_share` is the share of that week's active people.
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
ORDER BY week ASC, kind ASC, people DESC;