# Cardo — development tasks.
#
# Go is expected on PATH. If it was installed as a zip rather than system-wide:
#   export PATH="$$LOCALAPPDATA/cardo-toolchain/go/bin:$$PATH"

SAMPLE_DIR ?= .sample
DUCKDB     ?= duckdb

.PHONY: all
all: fmt vet test

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: vet
vet:
	go vet ./...

.PHONY: build
build:
	go build -o bin/cardo ./cmd/cardo

.PHONY: test
test:
	go test ./...

# The race detector needs cgo, which needs a C toolchain. Absent on a bare Windows box, so this
# is a separate target rather than the default; CI runs it on Linux where it always works.
.PHONY: test-race
test-race:
	CGO_ENABLED=1 go test ./... -race

# The invariant tests guard docs/design/invariants.md. Run them alone when changing anything
# that touches identity, storage or the SQL layer.
.PHONY: invariants
invariants:
	go test ./test/ -v -run 'TestINV'

# Regenerate the sample store and re-check every DuckDB view against it.
#
# The script goes in on stdin rather than through `duckdb -c`: dot commands like `.read` are a
# feature of the CLI's input loop, and `-c` parses its argument as SQL, so the `-c` form fails with
# a syntax error on the leading dot.
.PHONY: sql
sql:
	CARDO_SAMPLE_DIR=$(SAMPLE_DIR) go test ./test/ -run TestGenerateSampleStore
	printf '%s\n' \
	  "SET VARIABLE cardo_root = '$(SAMPLE_DIR)';" \
	  ".read sql/duckdb/010_bronze.sql" \
	  ".read sql/duckdb/020_silver.sql" \
	  ".read sql/duckdb/030_gold.sql" \
	  "SELECT * FROM gold_fleet_adoption ORDER BY day;" \
	  "SELECT * FROM gold_schema_drift;" \
	| $(DUCKDB) -init /dev/null

.PHONY: clean
clean:
	rm -rf bin $(SAMPLE_DIR)

# --- ClickHouse reference stack -------------------------------------------------------------
#
# The integration tests need a real server: REPLACE PARTITION's atomicity, ClickHouse's JSON
# functions and its cyclic-alias rule are all server behaviour that a mock would only agree with.
#
# Every target below that talks to a live server passes -count=1. Go caches a test result keyed on
# the binary, the flags, the environment and the files the test read -- not on the network -- so
# without it a second run against a changed server replays the first run's PASS, and a second
# `make sample-seed` does not seed anything. Both happened.

COMPOSE   ?= docker compose -f deploy/compose/docker-compose.yml
CH_URL    ?= http://127.0.0.1:8124
CH_USER   ?= cardo

.PHONY: stack-up
stack-up:
	$(COMPOSE) up -d

# The same stack, with the collector also served to a team over the network through TLS. Needs
# CARDO_HOSTNAME and CARDO_TLS_DIR in deploy/compose/.env; see "Serving a team over the network"
# in deploy/compose/README.md.
.PHONY: stack-up-network
stack-up-network:
	$(COMPOSE) -f deploy/compose/docker-compose.network.yml up -d

.PHONY: stack-down
stack-down:
	$(COMPOSE) down

# Stop the containers without deleting the data. Worth preferring over stack-down: with nothing
# running, Docker Desktop can idle its VM, which it will not do while any container is up.
.PHONY: stack-stop
stack-stop:
	$(COMPOSE) stop

.PHONY: clickhouse-test
clickhouse-test:
	CARDO_CLICKHOUSE_URL=$(CH_URL) CARDO_CLICKHOUSE_USER=$(CH_USER) \
		CARDO_CLICKHOUSE_PASSWORD=$$CLICKHOUSE_PASSWORD \
		go test -count=1 ./internal/store/clickhouse/ -v

# Invented data so the dashboards have something to draw before a real key exists. Writes under
# source='sample' and does not clean up; `make sample-drop` removes it.
.PHONY: sample-seed
sample-seed:
	CARDO_SEED_SAMPLE=1 CARDO_CLICKHOUSE_URL=$(CH_URL) CARDO_CLICKHOUSE_USER=$(CH_USER) \
		CARDO_CLICKHOUSE_PASSWORD=$$CLICKHOUSE_PASSWORD \
		go test -count=1 ./internal/store/clickhouse/ -run TestSeedSampleData -v

.PHONY: sample-drop
sample-drop:
	CARDO_DROP_SAMPLE=1 CARDO_CLICKHOUSE_URL=$(CH_URL) CARDO_CLICKHOUSE_USER=$(CH_USER) \
		CARDO_CLICKHOUSE_PASSWORD=$$CLICKHOUSE_PASSWORD \
		go test -count=1 ./internal/store/clickhouse/ -run TestDropSampleData -v

# Apply the ClickHouse schema without polling. The compose stack does this itself with its
# one-shot migrate service; this is for a ClickHouse the stack did not start.
.PHONY: migrate
migrate:
	CARDO_CLICKHOUSE_URL=$(CH_URL) CARDO_CLICKHOUSE_USER=$(CH_USER) \
		CARDO_CLICKHOUSE_PASSWORD=$$CLICKHOUSE_PASSWORD \
		go run ./cmd/cardo migrate

# The collector contract tests, against the running reference stack: fixtures go in over HTTP,
# and what reached ClickHouse is checked. CARDO_SALT must be the one the collector is running
# with -- one of these tests exists to prove the collector's pseudonym is the poller's -- and
# CARDO_ORG_ARTIFACTS, CARDO_ARTIFACT_NAMES and CARDO_ORG_REPOS, if set, must match too.
COLLECTOR_HOOKS_URL ?= http://127.0.0.1:8089/v1/hooks
COLLECTOR_OTLP_URL  ?= http://127.0.0.1:4319

.PHONY: collector-test
collector-test:
	CARDO_COLLECTOR_HOOKS_URL=$(COLLECTOR_HOOKS_URL) \
		CARDO_COLLECTOR_OTLP_URL=$(COLLECTOR_OTLP_URL) \
		CARDO_CLICKHOUSE_URL=$(CH_URL) CARDO_CLICKHOUSE_USER=$(CH_USER) \
		CARDO_CLICKHOUSE_PASSWORD=$$CLICKHOUSE_PASSWORD \
		go test -count=1 ./test/ -run 'TestCollector_|TestINV2_Collector' -v
