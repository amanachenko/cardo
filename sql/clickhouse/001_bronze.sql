-- Bronze: the raw layer, ClickHouse reference stack (ADR-0009).
--
-- This mirrors sql/duckdb/010_bronze.sql. The two dialects are one semantic layer, so a change to
-- the column set belongs in both files or in neither.
--
-- The column set is deliberately identical to the JSONL store's row struct
-- (internal/store/jsonl/jsonl.go). That is what lets the INV-5 allowlist test check one list and
-- cover both storage targets. Note what is absent: there is no email column, and there is no
-- column that could hold one.
--
-- `payload` is the verbatim API response element, stored as a String rather than a typed JSON
-- column. ADR-0010 requires that a field we parsed wrongly be a view rewrite rather than a
-- re-ingest, and that only holds if storage imposes no schema on the payload.

CREATE TABLE IF NOT EXISTS cardo.bronze_actor_day
(
    source          LowCardinality(String),
    day             Date,
    pseudonym       String,
    salt_version    LowCardinality(String),
    tier            UInt8,
    actor_type      LowCardinality(String),
    org_id          String,
    customer_type   LowCardinality(String),
    terminal_type   LowCardinality(String),
    payload         String,
    unknown_fields  Array(String)
)
ENGINE = MergeTree
PARTITION BY (source, day)
ORDER BY (pseudonym)
-- ADR-0016: tier 1 is retained for 90 days. With a stable salt this data stays personal data for
-- its whole lifetime, so retention is the control that actually bounds exposure -- not the word
-- "pseudonymous". This line deletes data. It is meant to.
TTL day + INTERVAL 90 DAY DELETE;

-- Staging table for atomic day replacement.
--
-- `Put` replaces a day rather than appending, because both analytics APIs revise recent days and
-- an appending poller would silently double every total. ClickHouse has no transactional delete
-- that fits, so the swap is done with ALTER TABLE ... REPLACE PARTITION, which is atomic: readers
-- see either the old day or the new one, never a half-written mixture. DROP-then-INSERT would
-- leave the day empty if the process died between the two.
--
-- `AS cardo.bronze_actor_day` is load-bearing rather than shorthand: REPLACE PARTITION requires
-- both tables to have identical structure and an identical partition key, so deriving the staging
-- table from the real one makes drift impossible. The explicit ENGINE clause replaces the source
-- table's engine definition, which is why staging carries no TTL -- rows live here for seconds.
CREATE TABLE IF NOT EXISTS cardo.bronze_actor_day_staging AS cardo.bronze_actor_day
ENGINE = MergeTree
PARTITION BY (source, day)
ORDER BY (pseudonym);
