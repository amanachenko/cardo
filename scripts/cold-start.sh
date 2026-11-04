#!/usr/bin/env bash
# cold-start.sh -- the collector starts before ClickHouse can answer, and its rows keep their names.
#
# After a machine or Docker restart, Docker starts every container by its restart policy at once:
# compose's depends_on is not consulted, and migrate, which runs once, does not run again. The
# collector usually wins the race. Its ClickHouse exporter reads each table's columns once, at
# start-up, and when that read fails it leaves EventName out of every INSERT for the rest of its
# life. Every view matches on EventName, so the prompts, cost and tool use of those days counted
# nowhere. Since sql/clickhouse/022 the database fills the name from the row's own attributes.
#
# This reproduces the race and checks the result:
#   1. starts the collector while no ClickHouse exists,
#   2. starts ClickHouse and applies the schema,
#   3. sends one Claude Code OTel event and one hook event,
#   4. requires both rows stored with their names,
#   5. requires the collector's INSERT to have left EventName out. Without that, the race was not
#      reproduced and the run proves nothing, so it fails.
#
# Everything it starts is named cardo-test-coldstart-* and is removed on exit. It never touches
# another stack. Runs in bash: Git Bash on Windows, or any macOS or Linux shell. Needs Docker, curl
# and Go. CI runs it in the collector job.

set -euo pipefail
# Git Bash rewrites arguments that look like paths before docker sees them. None here should be.
export MSYS_NO_PATHCONV=1

root=$(cd "$(dirname "$0")/.." && pwd)
name=cardo-test-coldstart-$$
collector=$name-collector
clickhouse=$name-clickhouse
password=cardo-coldstart
# The repository's public test salt. Fine here, where the only identities are invented.
salt=0123456789abcdef0123456789abcdef
session=cardo-coldstart-$(date +%s)-$$

collector_image=otel/opentelemetry-collector-contrib:0.161.0
clickhouse_image=clickhouse/clickhouse-server:26.6

cleanup() {
  docker rm -fv "$collector" "$clickhouse" >/dev/null 2>&1 || true
  docker network rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "--- collector log, without its stack traces" >&2
  docker logs "$collector" 2>&1 | grep -E '^[0-9]{4}-' | cut -c1-300 | tail -20 >&2 || true
  exit 1
}

# Docker on Windows reads C:/... and not /c/..., so a host path is converted where it can be.
host_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}

# The published port Docker chose for a container port, as host:port.
published() { docker port "$1" "$2" | head -n1 | sed 's/^0\.0\.0\.0/127.0.0.1/'; }

# The HTTP status of a request, with its body discarded through the pipe. Not -o /dev/null: with
# MSYS_NO_PATHCONV set, Windows' curl cannot open that path and fails after the request succeeded.
http_code() { curl -sS -w '\n%{http_code}' "$@" | tail -n1; }

ch_query() {
  curl -sS --fail-with-body -H 'X-ClickHouse-User: cardo' -H "X-ClickHouse-Key: $password" \
    "http://$ch_http/" --data-binary "$1"
}

docker network create "$name" >/dev/null

# 1. The collector, pointed at a ClickHouse that does not exist yet. It reaches it by the name
# clickhouse, which nothing answers to until step 2.
echo "Starting the collector with no ClickHouse..."
docker run -d --name "$collector" --network "$name" \
  -p 127.0.0.1::4318 -p 127.0.0.1::8088 \
  -e CARDO_SALT="$salt" \
  -e CARDO_CLICKHOUSE_ENDPOINT=tcp://clickhouse:9000 \
  -e CARDO_CLICKHOUSE_USER=cardo -e CARDO_CLICKHOUSE_PASSWORD="$password" \
  -v "$(host_path "$root/deploy/collector/config.yaml"):/etc/otelcol-contrib/config.yaml:ro" \
  "$collector_image" >/dev/null
otlp=$(published "$collector" 4318)
hooks=$(published "$collector" 8088)
# The collector starts its exporters before its receivers, so once the hook receiver answers, the
# exporters' start-up read of the tables has already failed. Its log says "schema detection failed",
# once per exporter, and nothing after.
for i in $(seq 1 60); do
  curl -sf "http://$hooks/health" >/dev/null && break
  [ "$i" = 60 ] && fail "the collector's hook receiver did not answer within 60 s"
  sleep 1
done

# 2. ClickHouse, then the schema.
echo "Starting ClickHouse and applying the schema..."
docker run -d --name "$clickhouse" --network "$name" --network-alias clickhouse \
  -p 127.0.0.1::8123 \
  -e CLICKHOUSE_USER=cardo -e CLICKHOUSE_PASSWORD="$password" -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  "$clickhouse_image" >/dev/null
ch_http=$(published "$clickhouse" 8123)
for i in $(seq 1 90); do
  ch_query 'SELECT 1' >/dev/null 2>&1 && break
  [ "$i" = 90 ] && fail "ClickHouse did not answer within 90 s"
  sleep 1
done
if ! out=$(cd "$root" && CARDO_CLICKHOUSE_URL="http://$ch_http" CARDO_CLICKHOUSE_USER=cardo \
  CARDO_CLICKHOUSE_PASSWORD="$password" go run ./cmd/cardo migrate 2>&1); then
  echo "$out" >&2
  fail "cardo migrate failed"
fi

# 3. One OTel event and one hook event, as Claude Code sends them.
echo "Sending one OTel event and one hook event..."
now=$(date +%s)000000000
otel_body=$(cat <<EOF
{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code"}}]},
 "scopeLogs":[{"scope":{"name":"com.anthropic.claude_code.events"},"logRecords":[{"timeUnixNano":"$now",
 "attributes":[{"key":"event.name","value":{"stringValue":"user_prompt"}},
               {"key":"session.id","value":{"stringValue":"$session"}},
               {"key":"prompt.id","value":{"stringValue":"$session-q1"}}]}]}]}]}
EOF
)
hook_body="{\"hook_event_name\":\"UserPromptSubmit\",\"session_id\":\"$session\",\"prompt_id\":\"$session-q1\"}"
code=$(http_code -H 'Content-Type: application/json' "http://$otlp/v1/logs" --data-binary "$otel_body")
[ "$code" = 200 ] || fail "the collector answered the OTel event with $code"
code=$(http_code -H 'Content-Type: application/json' "http://$hooks/v1/hooks" --data-binary "$hook_body")
[ "$code" = 200 ] || fail "the collector answered the hook event with $code"

# 4. Both rows, with their names. The exporter flushes every 10 s.
otel_name="SELECT EventName FROM cardo.bronze_otel_logs WHERE LogAttributes['session.id'] = '$session'"
hook_name="SELECT EventName FROM cardo.bronze_hook_events WHERE LogAttributes['session_id'] = '$session'"
for i in $(seq 1 60); do
  n=$(ch_query "SELECT ($otel_name LIMIT 1) IS NOT NULL AND ($hook_name LIMIT 1) IS NOT NULL" 2>/dev/null || echo 0)
  [ "$n" = 1 ] && break
  [ "$i" = 60 ] && fail "the two rows did not reach ClickHouse within 60 s"
  sleep 1
done
got_otel=$(ch_query "$otel_name")
got_hook=$(ch_query "$hook_name")
[ "$got_otel" = claude_code.user_prompt ] ||
  fail "the OTel row was stored as '$got_otel', want claude_code.user_prompt"
[ "$got_hook" = UserPromptSubmit ] ||
  fail "the hook row was stored as '$got_hook', want UserPromptSubmit"

# 5. The collector's own INSERTs, from the server's query log: the native protocol (interface 1),
# which cardo migrate, over HTTP, does not use. A collector that named its rows
# itself means ClickHouse was up before the exporter started, and nothing above tested the default.
ch_query 'SYSTEM FLUSH LOGS' >/dev/null
inserts=$(ch_query "SELECT count(), countIf(positionCaseInsensitive(query, 'EventName') > 0) FROM system.query_log
  WHERE type = 'QueryFinish' AND query_kind = 'Insert'
    AND interface = 1 AND hasAny(tables, ['cardo.bronze_otel_logs', 'cardo.bronze_hook_events']) FORMAT TSV")
total=${inserts%%$'\t'*}
named=${inserts##*$'\t'}
[ "$total" -ge 2 ] || fail "the query log holds $total collector INSERTs, want at least 2"
[ "$named" = 0 ] ||
  fail "the race was not reproduced: $named of the collector's $total INSERTs named EventName itself"

echo "PASS: the collector started before ClickHouse, left EventName out of all $total INSERTs, and both rows kept their names"
