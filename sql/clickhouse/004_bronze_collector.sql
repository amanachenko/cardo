-- 004 — bronze for the collector path (Phase 2): hook events, OTel log events, OTel metrics.
--
-- WHO WRITES THESE. Not cardo. The OpenTelemetry Collector's clickhouse exporter inserts into
-- them, configured in deploy/collector/config.yaml with `create_schema: false`, so the only SQL it
-- ever sends is INSERT. That split is deliberate: the DDL lives here, under the migration ledger
-- (ADR-0019), with retention set by us (ADR-0016) rather than by whichever collector process
-- happened to start first. The exporter's own README recommends exactly this for anything past a
-- demo, because every collector racing to CREATE on startup is how schemas drift.
--
-- WHY THE COLUMNS LOOK LIKE THIS. They are the exporter's, copied from its DDL at the pinned
-- version (opentelemetry-collector-contrib v0.161.0, exporter/clickhouseexporter/internal/
-- sqltemplates/{logs,metrics_sum}_table.sql). Names and types must match its INSERT exactly, and
-- a collector upgrade that changes them is a new migration, not an edit to this one. Two things
-- are dropped from upstream on purpose:
--   - the k8s.* MATERIALIZED columns. They are not INSERT targets, and nothing here runs on
--     Kubernetes metadata.
--   - the bloom-filter skip indexes. They exist for log search across millions of rows; this
--     holds tens of rows per engineer per day, and every index is merge work done at idle.
--
-- WHAT IS IN THE MAPS. Not what Claude Code sent. The collector has already pseudonymized
-- user.email, dropped account identifiers, paths and content, and reduced every hook payload to
-- an allowlist before anything reaches these tables. See the collector config for the contract
-- and test/collector_test.go for the proof. Bronze here is "raw" in the ADR-0010 sense -- nothing
-- normalized, nothing aggregated -- but it is not verbatim; ADR-0023 records why.
--
-- RETENTION is tier 1, 90 days (ADR-0016). Partitions are daily and ttl_only_drop_parts is on, so
-- expiry drops a whole day's part instead of rewriting parts to cut rows out of them.

-- One row per hook POST, from the webhook_event receiver. Body is always empty: the raw payload
-- is parsed into LogAttributes through an allowlist and then discarded (INV-5).
CREATE TABLE IF NOT EXISTS cardo.bronze_hook_events
(
    `Timestamp`          DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    `TraceId`            String CODEC(ZSTD(1)),
    `SpanId`             String CODEC(ZSTD(1)),
    `TraceFlags`         UInt8,
    `SeverityText`       LowCardinality(String) CODEC(ZSTD(1)),
    `SeverityNumber`     UInt8,
    `ServiceName`        LowCardinality(String) CODEC(ZSTD(1)),
    `Body`               String CODEC(ZSTD(1)),
    `ResourceSchemaUrl`  LowCardinality(String) CODEC(ZSTD(1)),
    `ResourceAttributes` Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `ScopeSchemaUrl`     LowCardinality(String) CODEC(ZSTD(1)),
    `ScopeName`          String CODEC(ZSTD(1)),
    `ScopeVersion`       LowCardinality(String) CODEC(ZSTD(1)),
    `ScopeAttributes`    Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `LogAttributes`      Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `EventName`          String CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(Timestamp)
ORDER BY (EventName, Timestamp)
TTL toDate(Timestamp) + INTERVAL 90 DAY DELETE
SETTINGS ttl_only_drop_parts = 1;

-- One row per Claude Code OTel log event (claude_code.user_prompt, tool_result, tool_decision,
-- api_request, ...). Same shape as the hook table because it is the same exporter code path.
CREATE TABLE IF NOT EXISTS cardo.bronze_otel_logs
(
    `Timestamp`          DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    `TraceId`            String CODEC(ZSTD(1)),
    `SpanId`             String CODEC(ZSTD(1)),
    `TraceFlags`         UInt8,
    `SeverityText`       LowCardinality(String) CODEC(ZSTD(1)),
    `SeverityNumber`     UInt8,
    `ServiceName`        LowCardinality(String) CODEC(ZSTD(1)),
    `Body`               String CODEC(ZSTD(1)),
    `ResourceSchemaUrl`  LowCardinality(String) CODEC(ZSTD(1)),
    `ResourceAttributes` Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `ScopeSchemaUrl`     LowCardinality(String) CODEC(ZSTD(1)),
    `ScopeName`          String CODEC(ZSTD(1)),
    `ScopeVersion`       LowCardinality(String) CODEC(ZSTD(1)),
    `ScopeAttributes`    Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `LogAttributes`      Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `EventName`          String CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(Timestamp)
ORDER BY (EventName, Timestamp)
TTL toDate(Timestamp) + INTERVAL 90 DAY DELETE
SETTINGS ttl_only_drop_parts = 1;

-- Claude Code's metrics are all monotonic counters (session.count, cost.usage, token.usage,
-- code_edit_tool.decision, lines_of_code.count, commit.count, pull_request.count,
-- active_time.total), so this is the only metric table that receives rows.
CREATE TABLE IF NOT EXISTS cardo.bronze_otel_metrics_sum
(
    `ResourceAttributes`    Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `ResourceSchemaUrl`     String CODEC(ZSTD(1)),
    `ScopeName`             String CODEC(ZSTD(1)),
    `ScopeVersion`          String CODEC(ZSTD(1)),
    `ScopeAttributes`       Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `ScopeDroppedAttrCount` UInt32 CODEC(ZSTD(1)),
    `ScopeSchemaUrl`        String CODEC(ZSTD(1)),
    `ServiceName`           LowCardinality(String) CODEC(ZSTD(1)),
    `MetricName`            LowCardinality(String) CODEC(ZSTD(1)),
    `MetricDescription`     String CODEC(ZSTD(1)),
    `MetricUnit`            String CODEC(ZSTD(1)),
    `Attributes`            Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    `StartTimeUnix`         DateTime CODEC(Delta, ZSTD(1)),
    `TimeUnix`              DateTime CODEC(Delta, ZSTD(1)),
    `Value`                 Float64 CODEC(ZSTD(1)),
    `Flags`                 UInt32 CODEC(ZSTD(1)),
    `Exemplars` Nested
    (
        FilteredAttributes Map(LowCardinality(String), String),
        TimeUnix           DateTime,
        Value              Float64,
        SpanId             String,
        TraceId            String
    ) CODEC(ZSTD(1)),
    `AggregationTemporality` Int32 CODEC(ZSTD(1)),
    `IsMonotonic`            Boolean CODEC(Delta, ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, toStartOfHour(TimeUnix), cityHash64(Attributes), TimeUnix)
TTL toDate(TimeUnix) + INTERVAL 90 DAY DELETE
SETTINGS ttl_only_drop_parts = 1;
