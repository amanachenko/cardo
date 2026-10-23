#!/usr/bin/env bash
# made-up.sh -- a Cardo holding a made-up organization (ADR-0051): invented engineers whose
# sessions go through the real collector, so the views can be seen and checked on an
# organization's worth of data before any organization's real data exists.
#
#   scripts/made-up.sh up       build this checkout and start the made-up stack, with fresh passwords and salt
#   scripts/made-up.sh load     invent the organization's twelve weeks and send them through the made-up collector
#   scripts/made-up.sh probe    send one backdated hook event and one OTel record, and check both kept their time
#   scripts/made-up.sh down     remove the made-up stack and everything it holds
#
# load passes any flags on to cmd/cardo-madeup, for example -seed 2 or -weeks 2.
#
# It never touches another stack. Its compose project is always cardo-madeup, with its own ports
# (deploy/compose/docker-compose.made-up.yml), and it sends only to those ports.
#
# Runs in bash: Git Bash on Windows, or any macOS or Linux shell. Needs Docker with compose, curl,
# and for load, Go.

set -euo pipefail
# Git Bash rewrites arguments that look like paths before docker sees them. None here should be.
export MSYS_NO_PATHCONV=1

root=$(cd "$(dirname "$0")/.." && pwd)
compose_dir="$root/deploy/compose"
env_file=.env.made-up
project=cardo-madeup
# Relative to the checkout; deploy/compose/made-up/.gitignore keeps it out of git.
key_file=deploy/compose/made-up/loaded/answer-key.json

# The made-up stack's published ports, from docker-compose.made-up.yml.
hooks_url=http://127.0.0.1:8090/v1/hooks
health_url=http://127.0.0.1:8090/health
otlp_url=http://127.0.0.1:4320
grafana_url=http://127.0.0.1:3003

die() { echo "made-up: $*" >&2; exit 1; }

cd "$compose_dir"

compose() {
  docker compose -p "$project" --env-file "$env_file" \
    -f docker-compose.yml -f docker-compose.made-up.yml "$@"
}

container_of() { # service -> the made-up stack's running container id, or nothing
  docker ps -q --filter "label=com.docker.compose.project=$project" \
    --filter "label=com.docker.compose.service=$1"
}

# clickhouse-client inside the made-up stack's ClickHouse, with that container's own credentials.
# The password is read inside the container and never appears in this script's arguments or output.
ch() {
  local id
  id=$(container_of clickhouse)
  [ -n "$id" ] || die "the made-up stack is not running. Start it with: scripts/made-up.sh up"
  docker exec "$id" sh -c 'exec clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" "$@"' sh "$@"
}

hex() { od -An -N"$1" -tx1 /dev/urandom | tr -dc 0-9a-f; }

# The HTTP status of a request, with its body discarded through the pipe. Not -o /dev/null: with
# MSYS_NO_PATHCONV set, Git Bash no longer turns that path into one Windows' curl can open, and curl
# fails with "write error" (23) after the request has already succeeded.
http_code() { curl -sS -w '\n%{http_code}' "$@" | tail -n1; }

# A fresh salt and passwords for each stack. Nothing in it is worth keeping: down deletes it with
# the data, and the next up makes new ones.
ensure_env_file() {
  [ -f "$env_file" ] && return
  {
    echo "# Written by scripts/made-up.sh: throwaway values for the made-up stack only."
    echo "# Grafana at $grafana_url signs in as admin with GRAFANA_PASSWORD."
    echo "CLICKHOUSE_PASSWORD=$(hex 16)"
    echo "GRAFANA_PASSWORD=$(hex 16)"
    echo "CARDO_SALT=$(hex 32)"
  } > "$env_file"
}

wait_for_collector() {
  local tries=0
  until [ "$(http_code "$health_url" 2>/dev/null || true)" = 200 ]; do
    tries=$((tries + 1))
    [ "$tries" -le 120 ] || die "the made-up collector did not answer on $health_url after 120 s. See: docker compose -p $project logs collector"
    sleep 1
  done
}

cmd_up() {
  # Its ClickHouse keeps the password it was first started with, which is in the .env.made-up of
  # the checkout that started it. Another checkout's would not open it.
  if [ ! -f "$env_file" ] && [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=$project")" ]; then
    die "a made-up stack started from another checkout is still there. Run scripts/made-up.sh down, here or there, first."
  fi
  ensure_env_file
  echo "== Building cardo from this checkout, and starting the made-up stack"
  compose build migrate
  compose up -d
  wait_for_collector
  echo
  echo "Made-up Grafana: $grafana_url (admin, with GRAFANA_PASSWORD from deploy/compose/$env_file)."
  echo "It holds no sessions yet."
}

# One hook event and one OTel record, both dated 30 days ago, then read back from ClickHouse. It
# checks the one thing this stack does differently from every other: a hook event keeps the time
# the generator gave it, and the header that carried the time is not stored.
cmd_probe() {
  [ -n "$(container_of collector)" ] || die "the made-up stack is not running. Start it with: scripts/made-up.sh up"
  local sid ns secs code
  sid="made-up-probe-$(hex 6)"
  secs=$(( $(date -u +%s) - 30 * 86400 ))
  ns="${secs}123456789"

  code=$(http_code -H 'Content-Type: application/json' \
    -H "X-Cardo-Made-Up-Time: $ns" \
    --data "{\"hook_event_name\":\"SessionEnd\",\"session_id\":\"$sid\",\"reason\":\"other\"}" \
    "$hooks_url")
  [ "$code" = 200 ] || die "the hook endpoint answered $code"

  code=$(http_code -H 'Content-Type: application/json' \
    --data "{\"resourceLogs\":[{\"resource\":{\"attributes\":[{\"key\":\"service.name\",\"value\":{\"stringValue\":\"claude-code\"}},{\"key\":\"user.email\",\"value\":{\"stringValue\":\"probe@example.com\"}}]},\"scopeLogs\":[{\"scope\":{\"name\":\"com.anthropic.claude_code.events\"},\"logRecords\":[{\"timeUnixNano\":\"$ns\",\"attributes\":[{\"key\":\"event.name\",\"value\":{\"stringValue\":\"user_prompt\"}},{\"key\":\"session.id\",\"value\":{\"stringValue\":\"$sid\"}}]}]}]}]}" \
    "$otlp_url/v1/logs")
  [ "$code" = 200 ] || die "the OTLP endpoint answered $code"

  echo "== Sent one hook event and one OTel record for $(date -u -d "@$secs" +%F 2>/dev/null || echo "30 days ago"); waiting for the exporter's 10 s flush"
  local hook="" otel="" tries=0
  while :; do
    hook=$(ch --query "SELECT toUnixTimestamp64Nano(Timestamp), arrayStringConcat(arrayFilter(k -> k LIKE 'header%', mapKeys(LogAttributes)), ',') FROM cardo.bronze_hook_events WHERE LogAttributes['session_id'] = '$sid' FORMAT TSV")
    otel=$(ch --query "SELECT toUnixTimestamp64Nano(Timestamp) FROM cardo.bronze_otel_logs WHERE LogAttributes['session.id'] = '$sid' FORMAT TSV")
    [ -n "$hook" ] && [ -n "$otel" ] && break
    tries=$((tries + 1))
    [ "$tries" -le 40 ] || die "after 40 s, hook row: '${hook:-none}', OTel row: '${otel:-none}'. See: docker compose -p $project logs collector"
    sleep 1
  done

  local ok=1 hook_ns hook_headers
  hook_ns=$(echo "$hook" | cut -f1)
  hook_headers=$(echo "$hook" | cut -f2)
  if [ "$hook_ns" = "$ns" ]; then
    echo "   hook event   kept its time"
  else
    echo "   hook event   FAIL: stored at $hook_ns, sent for $ns"; ok=0
  fi
  if [ -z "$hook_headers" ]; then
    echo "   hook event   the time header was not stored"
  else
    echo "   hook event   FAIL: stored $hook_headers"; ok=0
  fi
  if [ "$otel" = "$ns" ]; then
    echo "   OTel record  kept its time"
  else
    echo "   OTel record  FAIL: stored at $otel, sent for $ns"; ok=0
  fi
  [ "$ok" = 1 ] || exit 1
  echo "PASS"
}

counts() {
  ch --query "SELECT (SELECT count() FROM cardo.bronze_hook_events), (SELECT count() FROM cardo.bronze_otel_logs), (SELECT count() FROM cardo.bronze_otel_metrics_sum) FORMAT TSV"
}

# The made-up organization's history, invented from a seed by cmd/cardo-madeup and sent through the
# made-up collector as Claude Code would send it. The answer key, what was planted, is written
# beside this stack's files and removed with it.
cmd_load() {
  [ -n "$(container_of collector)" ] || die "the made-up stack is not running. Start it with: scripts/made-up.sh up"
  command -v go >/dev/null || die "load needs Go on PATH, to run cmd/cardo-madeup from this checkout"
  local before
  before=$(counts)
  [ "$before" = "$(printf '0\t0\t0')" ] || die "the made-up stack already holds a load (hook events, log records, metric points: $(echo "$before" | tr '\t' ' ')). Loading again would count every session twice. Run scripts/made-up.sh down, then up, first."

  mkdir -p "$compose_dir/made-up/loaded"
  echo "== Inventing the made-up organization, and sending it through the made-up collector"
  # A path relative to the checkout: with MSYS_NO_PATHCONV set, Go on Windows would read Git
  # Bash's /c/... as a folder named c.
  (cd "$root" && go run ./cmd/cardo-madeup -key "$key_file" "$@")

  echo "== Waiting for the collector's last flush"
  local now last="" tries=0
  while :; do
    now=$(counts)
    [ "$now" = "$last" ] && break
    last=$now
    tries=$((tries + 1))
    [ "$tries" -le 30 ] || break
    sleep 12
  done

  local want got
  want="$(total hook_events)	$(total log_records)	$(total metric_points)"
  got=$now
  echo "   sent:   $(echo "$want" | tr '\t' ' ')   (hook events, log records, metric points)"
  echo "   stored: $(echo "$got" | tr '\t' ' ')"
  [ "$want" = "$got" ] || die "the made-up stack stored a different number of rows than were sent. See: docker compose -p $project logs collector"
  echo
  echo "Made-up Grafana: $grafana_url (admin, with GRAFANA_PASSWORD from deploy/compose/$env_file)."
  echo "Answer key: $key_file"
}

# A total from the answer key. Each name appears once in it, in "totals".
total() { grep -m1 "\"$1\":" "$root/$key_file" | tr -dc 0-9; }

cmd_down() {
  if [ -f "$env_file" ]; then
    compose down -v --remove-orphans
  else
    # Started from another checkout. Removing needs no password, only values for compose to read
    # the files with; it finds the containers and volumes by project and service name. Relative,
    # because docker on Windows cannot open Git Bash's /tmp.
    printf 'CLICKHOUSE_PASSWORD=unused\nGRAFANA_PASSWORD=unused\n' > .env.made-up-down
    docker compose -p "$project" --env-file .env.made-up-down \
      -f docker-compose.yml -f docker-compose.made-up.yml down -v --remove-orphans
    rm -f .env.made-up-down
  fi
  rm -f "$env_file"
  rm -f "$root/$key_file"
  echo "Made-up stack removed, with its data and answer key. The cardo-madeup:dev image is kept for the next build;"
  echo "docker image rm cardo-madeup:dev removes it."
}

case ${1:-} in
  up)    cmd_up ;;
  load)  shift; cmd_load "$@" ;;
  probe) cmd_probe ;;
  down)  cmd_down ;;
  *)
    sed -n '2,17p' "$root/scripts/made-up.sh" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
