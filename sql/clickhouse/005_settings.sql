-- 005 — the deployment choices the collector-path views read when they run (ADR-0029).
--
-- Two values, both chosen by the operator rather than by a migration:
--
--   org_artifacts    CARDO_ORG_ARTIFACTS, the regex naming the organization's own artifacts. The
--                    collector reads the same variable to decide what to store; the views read it
--                    to decide what to label as the organization's. Empty names nothing.
--   min_group_size   CARDO_MIN_GROUP_SIZE. A home-grown artifact name or a cohort label is shown
--                    only once this many people are in it; below that it is counted as "other".
--
-- `cardo migrate` writes them from its environment. A migration cannot, because migrations are
-- immutable and these are not. Nothing else writes here, and the poller never does.
--
-- Rows are appended, never updated, each with a version one above the highest so far, and the
-- highest version for a key wins. Not the newest timestamp: a server's clock can step backwards,
-- and the reference stack's did (its Docker VM clock was measured drifting by seconds and
-- jumping). A write stamped earlier than the one before it would silently lose. updated_at is
-- kept for whoever reads the table, and decides nothing.
--
-- ReplacingMergeTree collapses superseded rows in the background, but correctness never waits
-- for that, because settings_effective picks the highest version itself.

CREATE TABLE IF NOT EXISTS cardo.settings
(
    `key`        LowCardinality(String),
    `value`      String,
    `version`    UInt64,
    `updated_at` DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(version)
ORDER BY key;

-- One row, always, whether or not anything was ever written.
--
-- The floor of five is applied here as well as in `cardo migrate`, so a value inserted by hand
-- cannot lower it (ADR-0029). A value that is not a whole number falls back to the default
-- rather than to zero, which would name everyone.
CREATE OR REPLACE VIEW cardo.settings_effective AS
SELECT
    greatest(toUInt32(5), toUInt32OrDefault(argMaxIf(value, version, key = 'min_group_size'), toUInt32(5))) AS min_group_size,
    argMaxIf(value, version, key = 'org_artifacts') AS org_artifacts
FROM cardo.settings;
