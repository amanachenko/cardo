-- The team directory as a hashed mapping (ADR-0046).
--
-- No email column exists, by construction. The uploaded file is the operator's; this database only
-- ever holds (team, pseudonym) pairs produced with the stack's salt, so a backup cannot leak a
-- directory of people.

CREATE TABLE IF NOT EXISTS cardo.settings_teams_batches
(
    batch       String,
    source_hash String, -- SHA-256 of the uploaded file: proves what was loaded without containing it
    rows        UInt32,
    teams       UInt32,
    loaded_at   DateTime64(3) DEFAULT now64(3)
)
ENGINE = MergeTree
ORDER BY batch;

CREATE TABLE IF NOT EXISTS cardo.settings_teams
(
    team         LowCardinality(String),
    pseudonym    String,
    salt_version LowCardinality(String), -- joins only sessions hashed with the same salt version
    batch        String
)
ENGINE = MergeTree
ORDER BY (batch, pseudonym);
