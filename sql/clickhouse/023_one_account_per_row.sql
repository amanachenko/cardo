-- 023 — a session can carry more than one account, and each row belongs to the account on it
-- (ADR-0059).
--
-- WHAT WENT WRONG. When someone switches Claude accounts in the middle of a session, Claude Code
-- keeps the session id and sends the new account's email from then on, so one session's rows
-- carry two code names. silver_session_live picked one with anyIf, which returns whichever row
-- ClickHouse reads first, and the other silver views took the session's pick. The whole session,
-- its prompts and its dollars, moved between the two accounts from one read to the next, and the
-- coach showed different figures on each reload.
--
-- 1. THE ACCOUNT ACTIVE AT EACH MOMENT. A hook row carries no identity (ADR-0023), and an OTel
-- event can arrive without an email. Such a row belongs to the account active when it was sent:
-- the last code name the session showed before it. silver_session_account_live lists, for each
-- session, each moment the code name changed, and a view credits an unnamed row with an ASOF join
-- on it:
--
--     ASOF LEFT JOIN cardo.silver_session_account_live AS a
--         ON a.session_id = r.session_id AND r.at >= a.active_from
--
-- For example, a session whose events carry account A from 10:00, B from 10:03 and A again from
-- 10:06 has three rows here: A from the beginning of time, B from 10:03, A from 10:06. A hook row
-- at 10:05 is B's, and the session's end at 10:07 is A's. The session's first account is active
-- from the beginning of time, so a hook row sent before any OTel event, such as the session's
-- start, takes the first account. A session with no code name at all has no row here, and its rows
-- stay unattributed.
--
-- A code name is read from every OTel event, on the record or on the resource, wherever the
-- collector put it, and from the session-count metric. The metric is placed at the start of its
-- interval, which is never later than the session's start: its own time is when it was exported,
-- up to a minute later, which would have looked like a switch back to the first account.
--
-- The hook row's time is the collector's, and an OTel event's is the laptop's. A hook row sent
-- within a few seconds of a switch can land on the other account when the clocks disagree, and it
-- lands there on every read (ADR-0059, consequences).
--
-- This is a plain view, read only by other _live views and the deletion procedure: a rebuild
-- still starts from the raw tables (017, "no stored table reads another").

CREATE OR REPLACE VIEW cardo.silver_session_account_live AS
SELECT
    session_id,
    pseudonym,
    if(n = 1, toDateTime64(0, 9), seen_at)                                     AS active_from
FROM
(
    SELECT
        session_id,
        pseudonym,
        seen_at,
        row_number() OVER w                                                    AS n,
        lagInFrame(pseudonym, 1, '') OVER w                                    AS before
    FROM
    (
        SELECT
            LogAttributes['session.id']                                        AS session_id,
            Timestamp                                                          AS seen_at,
            if(LogAttributes['user.pseudonym'] != '', LogAttributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])                           AS pseudonym
        FROM cardo.bronze_otel_logs
        WHERE session_id != '' AND pseudonym != ''

        UNION ALL

        SELECT
            Attributes['session.id'],
            toDateTime64(StartTimeUnix, 9),
            if(Attributes['user.pseudonym'] != '', Attributes['user.pseudonym'],
               ResourceAttributes['user.pseudonym'])
        FROM cardo.bronze_otel_metrics_sum
        WHERE MetricName = 'claude_code.session.count'
          AND Attributes['session.id'] != ''
          AND if(Attributes['user.pseudonym'] != '', Attributes['user.pseudonym'],
                 ResourceAttributes['user.pseudonym']) != ''
    )
    WINDOW w AS (PARTITION BY session_id ORDER BY seen_at, pseudonym
                 ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
)
WHERE pseudonym != before;
