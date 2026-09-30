-- Bronze: the raw layer.
--
-- ADR-0010 stores payloads verbatim so that a field we parsed wrongly is a view rewrite rather
-- than a re-ingest. That only holds if the read does not impose a schema on the payload, so the
-- column types are pinned explicitly and `payload` stays JSON. With schema inference instead, a
-- new field in one day's file would change the inferred struct and break the read across the
-- whole partition — which is the failure ADR-0010 exists to avoid.
--
-- `cardo_root` must be set before running these files:
--   duckdb -c "SET VARIABLE cardo_root = '/path/to/data'; .read sql/duckdb/010_bronze.sql"

CREATE OR REPLACE VIEW bronze_actor_day AS
SELECT *
FROM read_json(
    getvariable('cardo_root') || '/bronze/*/*.jsonl',
    hive_partitioning = true,
    union_by_name = true,
    columns = {
        source:         'VARCHAR',
        day:            'DATE',
        pseudonym:      'VARCHAR',
        salt_version:   'VARCHAR',
        tier:           'INTEGER',
        actor_type:     'VARCHAR',
        org_id:         'VARCHAR',
        customer_type:  'VARCHAR',
        terminal_type:  'VARCHAR',
        payload:        'JSON',
        unknown_fields: 'VARCHAR[]'
    }
);
