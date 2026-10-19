-- The enablement catalog: one row per version, files on disk (ADR-0045).
--
-- A version is immutable. The current version of a slug is argMax(version); the approved version is
-- the highest version whose status is 'approved'. Drafts exist for the operator and change nothing
-- the views read.

CREATE TABLE IF NOT EXISTS cardo.settings_artifact_versions
(
    slug         LowCardinality(String),
    version      UInt32,
    kind         LowCardinality(String),
    name         String,
    owner        String,
    status       LowCardinality(String), -- draft | approved | retired
    content_hash String,
    path         String,
    note         String,
    created_at   DateTime64(3) DEFAULT now64(3),
    approved_at  Nullable(DateTime64(3))
)
ENGINE = MergeTree
ORDER BY (slug, version);

-- One line per write. This is the audit trail the console's "Recent changes" reads, and the answer to
-- "who made this the standard" without watching anybody.
CREATE TABLE IF NOT EXISTS cardo.settings_changes
(
    at          DateTime64(3) DEFAULT now64(3),
    action      LowCardinality(String), -- upload | approve | retire | rollback | setting
    object      String,                 -- slug, or a settings key
    version     UInt32,
    before_hash String,
    after_hash  String,
    note        String
)
ENGINE = MergeTree
ORDER BY at;
