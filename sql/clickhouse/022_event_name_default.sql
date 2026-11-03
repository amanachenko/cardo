-- 022 — a row that reaches the store without its event name takes it from its own attributes.
--
-- WHAT WENT WRONG. The collector's ClickHouse exporter reads each table's columns once, when it
-- starts (collector-contrib v0.161.0, exporter_logs.go: start, then detectSchemaFeatures). If
-- ClickHouse is not answering yet, that read fails, the error is logged once, and for the rest of
-- the process's life the exporter writes rows without the EventName column, so ClickHouse stores
-- ''. Every view matches on EventName. A prompt, a request's cost or a tool call written that way
-- counts nowhere, and a week of work reads as a week nobody worked, until someone restarts the
-- collector.
--
-- It happens after a machine or Docker restart. Compose's depends_on holds the collector back only
-- on `docker compose up`. After a reboot Docker restarts each container by its restart policy
-- alone: the collector comes up before ClickHouse answers, and migrate, which runs once, does not
-- run again.
--
-- WHY THE DATABASE FIXES IT, NOT THE COLLECTOR. The name survives in every such row. An OTel row
-- carries it as LogAttributes['event.name'], user_prompt for claude_code.user_prompt, and a hook
-- row as LogAttributes['hook_event_name'], UserPromptSubmit. A column DEFAULT is computed whenever
-- an INSERT leaves the column out, which is exactly what the exporter does once its start-up read
-- has failed. So the name is filled however the collector started, and nothing has to wait for
-- anything: the collector image has no shell for a wait script, and the upstream exporter still
-- reads the columns only once.
--
-- The DEFAULT repeats the collector's own naming, the OTTL statements that set log.event_name in
-- deploy/collector/config.yaml. A change there needs a new migration here (the upgrade checklist
-- in deploy/collector/README.md says so). An INSERT that names its EventName keeps it, as the
-- healthy exporter, the tests and the made-up organization do. Changing a sort-key column's
-- DEFAULT changes metadata only: on ClickHouse 26.6 no row is rewritten and the codec is kept.
--
-- THE REPAIR. Rows already stored without their name are given it, once, in this order:
--   1. the DEFAULT, so nothing new arrives nameless while the rest runs,
--   2. a named copy of each nameless row that carries its name,
--   3. the trend rows of every day and week those rows touched are cleared, so that `cardo trend`
--      recomputes them from the repaired rows on its next pass. It fills every day in the window
--      that has no row (the trend rows that outlive their rows, ADR-0055).
--   4. the nameless rows are deleted.
-- No view counts a row twice in between: views match on EventName, and the original's is ''. The
-- trend rows are cleared after the copies exist, so a trend run that falls in between reads the
-- repaired rows, and before the originals go, because the originals are how the days are found.
-- A day is touched through a nameless row on it, or through a session it belongs to: a session is
-- counted on the day it started, so a prompt after midnight still changes the day before.
--
-- A TREND ROW THAT CANNOT BE RECOMPUTED IS LEFT ALONE. `cardo trend` refuses a day older than the
-- retention window, because recomputing it would replace a complete row with an empty one. A row
-- cleared there would be lost for ever, so only days inside the window are cleared, with a day's
-- margin for the clocks. The window is the retention_days setting (the organization's choice,
-- ADR-0053), 90 days when none is set (internal/retention.DefaultDays), and 0 keeps everything.
-- Stored silver needs nothing: its next rebuild reads the repaired rows.
--
-- RE-RUNNABLE. The runner runs a migration again from its first statement when one fails. The copy
-- skips a row whose named copy is already there (same time, same attributes), so a second run adds
-- nothing, and the statements after it find nothing left to do.

-- 1. The DEFAULT, on both tables the exporter writes logs to.

ALTER TABLE cardo.bronze_otel_logs
    MODIFY COLUMN EventName DEFAULT if(LogAttributes['event.name'] = '', '',
                                       concat('claude_code.', LogAttributes['event.name']));

ALTER TABLE cardo.bronze_hook_events
    MODIFY COLUMN EventName DEFAULT LogAttributes['hook_event_name'];

-- 2. A named copy of each nameless row that carries its name, unless one is already there. The
-- rows are chosen in a subquery so that WHERE reads the stored EventName, not the copy's.

INSERT INTO cardo.bronze_otel_logs
SELECT * REPLACE (concat('claude_code.', LogAttributes['event.name']) AS EventName)
FROM
(
    SELECT *
    FROM cardo.bronze_otel_logs
    WHERE EventName = '' AND LogAttributes['event.name'] != ''
      AND (Timestamp, cityHash64(LogAttributes)) NOT IN
      (
          SELECT Timestamp, cityHash64(LogAttributes)
          FROM cardo.bronze_otel_logs
          WHERE EventName != ''
            AND toDate(Timestamp) IN
            (
                SELECT toDate(Timestamp) FROM cardo.bronze_otel_logs
                WHERE EventName = '' AND LogAttributes['event.name'] != ''
            )
      )
);

INSERT INTO cardo.bronze_hook_events
SELECT * REPLACE (LogAttributes['hook_event_name'] AS EventName)
FROM
(
    SELECT *
    FROM cardo.bronze_hook_events
    WHERE EventName = '' AND LogAttributes['hook_event_name'] != ''
      AND (Timestamp, cityHash64(LogAttributes)) NOT IN
      (
          SELECT Timestamp, cityHash64(LogAttributes)
          FROM cardo.bronze_hook_events
          WHERE EventName != ''
            AND toDate(Timestamp) IN
            (
                SELECT toDate(Timestamp) FROM cardo.bronze_hook_events
                WHERE EventName = '' AND LogAttributes['hook_event_name'] != ''
            )
      )
);

-- 3. The trend rows of the days and weeks the nameless rows touched, inside the window only. The
-- days are written to a scratch table first: a DELETE's condition cannot hold a UNION, and the
-- days are read once rather than once per part.

CREATE TABLE IF NOT EXISTS cardo.repair_022_days (day Date) ENGINE = MergeTree ORDER BY day;

TRUNCATE TABLE cardo.repair_022_days;

INSERT INTO cardo.repair_022_days
SELECT DISTINCT touched FROM
(
    SELECT day AS touched FROM cardo.silver_session_live
    WHERE session_id IN
    (
        SELECT LogAttributes['session.id'] FROM cardo.bronze_otel_logs
        WHERE EventName = '' AND LogAttributes['event.name'] != ''
        UNION ALL
        SELECT LogAttributes['session_id'] FROM cardo.bronze_hook_events
        WHERE EventName = '' AND LogAttributes['hook_event_name'] != ''
    )
    UNION ALL
    SELECT toDate(Timestamp) FROM cardo.bronze_otel_logs
    WHERE EventName = '' AND LogAttributes['event.name'] != ''
    UNION ALL
    SELECT toDate(Timestamp) FROM cardo.bronze_hook_events
    WHERE EventName = '' AND LogAttributes['hook_event_name'] != ''
);

ALTER TABLE cardo.trend_day DELETE
WHERE day IN (SELECT day FROM cardo.repair_022_days)
  AND ((SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
        FROM cardo.settings WHERE key = 'retention_days') = 0
       OR day >= today() - (SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
                            FROM cardo.settings WHERE key = 'retention_days') + 2)
SETTINGS mutations_sync = 2;

ALTER TABLE cardo.trend_week_artifact DELETE
WHERE week IN (SELECT toMonday(day) FROM cardo.repair_022_days)
  AND ((SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
        FROM cardo.settings WHERE key = 'retention_days') = 0
       OR week >= today() - (SELECT toInt64OrDefault(argMax(value, version), toInt64(90))
                             FROM cardo.settings WHERE key = 'retention_days') + 2)
SETTINGS mutations_sync = 2;

-- 4. The nameless rows that carry their name. Their named copies are in place.

ALTER TABLE cardo.bronze_otel_logs DELETE
WHERE EventName = '' AND LogAttributes['event.name'] != ''
SETTINGS mutations_sync = 2;

ALTER TABLE cardo.bronze_hook_events DELETE
WHERE EventName = '' AND LogAttributes['hook_event_name'] != ''
SETTINGS mutations_sync = 2;

DROP TABLE IF EXISTS cardo.repair_022_days;
