#!/usr/bin/env bash
# preview.sh -- a second Cardo beside the live one, built from this checkout, over a copy of the
# live stack's stored rows. A change to the views, the dashboards or a command is seen on real
# data before it merges, and its migrations never reach the stack that collects.
#
#   scripts/preview.sh up            build this checkout, start the preview, copy the rows
#   scripts/preview.sh refresh       apply this checkout's migrations again, and copy the rows again
#   scripts/preview.sh compare       rows, migrations, views, and the dashboards' views day by day
#   scripts/preview.sh verify-live   every query the preview sent the live stack: reads only?
#   scripts/preview.sh down          remove the preview and everything it holds
#
# The live stack is only ever read. Every query sent to it goes through clickhouse-client inside
# its own container with readonly=1, under a query id starting "cardo-preview-", so verify-live
# can show from the live server's own query log that nothing else was sent. The rows travel as
# files through this machine, never over a network between the two stacks.
#
# Runs in bash: Git Bash on Windows, or any macOS or Linux shell. Needs Docker with compose.
# CARDO_LIVE_PROJECT names the live stack's compose project (default: cardo).
# The walkthrough that uses it is .claude/skills/preview/SKILL.md.

set -euo pipefail
# Git Bash rewrites arguments that look like paths before docker sees them. None here should be.
export MSYS_NO_PATHCONV=1

root=$(cd "$(dirname "$0")/.." && pwd)
compose_dir="$root/deploy/compose"
env_file=.env.preview
state_file=.preview-state
live_project=${CARDO_LIVE_PROJECT:-cardo}
project=cardo-preview

# The tables the views read. Everything else in the database is derived from these, or is the
# migration ledger, which each stack keeps for itself.
tables="bronze_hook_events bronze_otel_logs bronze_otel_metrics_sum bronze_actor_day settings"

# Rows newer than the cutoff are not copied, so that live and preview can be compared exactly:
# the live stack keeps collecting while the preview stands still.
cutoff_lag_minutes=5

die() { echo "preview: $*" >&2; exit 1; }

cd "$compose_dir"

compose() {
  docker compose -p "$project" --env-file "$env_file" \
    -f docker-compose.yml -f docker-compose.preview.yml "$@"
}

container_of() { # project service -> running container id, or nothing
  docker ps -q --filter "label=com.docker.compose.project=$1" \
    --filter "label=com.docker.compose.service=$2"
}

live_container() {
  [ "$live_project" != "$project" ] || die "CARDO_LIVE_PROJECT cannot be the preview itself"
  local id
  id=$(container_of "$live_project" clickhouse)
  [ -n "$id" ] || die "no running ClickHouse in the compose project '$live_project'. Start the live stack, or set CARDO_LIVE_PROJECT."
  echo "$id"
}

preview_container() {
  local id
  id=$(container_of "$project" clickhouse)
  [ -n "$id" ] || die "the preview is not running. Start it with: scripts/preview.sh up"
  echo "$id"
}

# clickhouse-client inside a container, with that container's own credentials. The password is
# read inside the container and never appears in this script's arguments or output. Only an
# insert gets stdin (-i): a query that reads must not wait on it.
client='exec clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" "$@"'
ch_in() { local c=$1; shift; docker exec "$c" sh -c "$client" sh "$@"; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Every query to the live stack: read-only, and labelled so that verify-live can find it in the
# live server's query log. Each id is written down, so the copy knows how many it sent: the calls
# run in subshells, where a counter would be lost.
live_ch() {
  local id
  id="cardo-preview-$run-$(od -An -N6 -tx1 /dev/urandom | tr -dc 0-9a-f)"
  echo "$id" >> "$tmp/live-queries"
  ch_in "$LIVE" --readonly 1 --query_id "$id" "$@"
}

preview_ch() { ch_in "$PREVIEW" "$@"; }
preview_insert() { docker exec -i "$PREVIEW" sh -c "$client" sh "$@"; }

ensure_env_file() {
  [ -f "$env_file" ] && return
  local pw1 pw2
  pw1=$(od -An -N16 -tx1 /dev/urandom | tr -dc 0-9a-f)
  pw2=$(od -An -N16 -tx1 /dev/urandom | tr -dc 0-9a-f)
  {
    echo "# Written by scripts/preview.sh: throwaway passwords for the preview stack only."
    echo "# Grafana at http://127.0.0.1:3002 signs in as admin with GRAFANA_PASSWORD."
    echo "CLICKHOUSE_PASSWORD=$pw1"
    echo "GRAFANA_PASSWORD=$pw2"
  } > "$env_file"
}

time_filter() { # table -> the condition that keeps rows older than the cutoff
  case $1 in
    bronze_hook_events | bronze_otel_logs) echo "Timestamp < toDateTime('$cutoff', 'UTC')" ;;
    bronze_otel_metrics_sum)               echo "TimeUnix < toDateTime('$cutoff', 'UTC')" ;;
    *)                                     echo "1" ;;
  esac
}

columns() { # who table -> the table's column names, in order, one per line
  "$1" --query "SELECT name FROM system.columns WHERE database = 'cardo' AND table = '$2' ORDER BY position FORMAT TSV"
}

has_table() { # who table
  [ "$("$1" --query "SELECT count() FROM system.tables WHERE database = 'cardo' AND name = '$2' FORMAT TSV")" = 1 ]
}

typed_columns() { # who table -> name<TAB>type, one column per line, in order
  "$1" --query "SELECT name, type FROM system.columns WHERE database = 'cardo' AND table = '$2' ORDER BY position FORMAT TSV"
}

# The gold views file a session's events under the day it started (sql/clickhouse/007), so a
# session still sending after the cutoff keeps changing its start day on live, while the preview
# stands still. This lists, from live, the start day of every such session before the cutoff's
# day (n = 0), and the six days before each (n = 1..6): a week that starts on one of those holds it.
open_session_days_sql() {
  cat <<SQL
SELECT DISTINCT toString(day - n), n
FROM cardo.silver_session
ARRAY JOIN range(7) AS n
WHERE day < '$cutoff_day'
  AND session_id IN
  (
      SELECT LogAttributes['session.id'] FROM cardo.bronze_otel_logs
      WHERE Timestamp >= toDateTime('$cutoff', 'UTC')
      UNION DISTINCT
      SELECT Attributes['session.id'] FROM cardo.bronze_otel_metrics_sum
      WHERE TimeUnix >= toDateTime('$cutoff', 'UTC')
      UNION DISTINCT
      SELECT LogAttributes['session_id'] FROM cardo.bronze_hook_events
      WHERE Timestamp >= toDateTime('$cutoff', 'UTC')
  )
FORMAT TSV
SQL
}

# The dashboards read only gold views (INV-3). Each one with a day or week column is compared day
# by day, before the cutoff's day (or its week), as a count and a hash of its rows per day. Floats
# are rounded first: a sum can differ in its last bit when the server adds in another order. Each
# row is hashed as its text, because cityHash64 of a NULL is NULL, and sum() skips it: a day with a
# NULL anywhere in it would hash to nothing on both sides, and compare as the same.
compare_gold_views() {
  local cutoff_day=${cutoff%% *}
  echo "== Dashboard views: live against preview, each day before $cutoff_day"
  local open="" open_days open_weeks
  if has_table live_ch silver_session; then
    open=$(live_ch --query "$(open_session_days_sql)")
  fi
  open_days=$(echo "$open" | awk -F'\t' '$2 == 0 { print $1 }')
  open_weeks=$(echo "$open" | cut -f1 | sort -u)

  local v ok=1 any_open=0
  for v in $(grep -Fx -f <(echo "$lv") <(echo "$pv") | grep '^gold_' || true); do
    local lc pc col filter exprs sql l p changed n d set opened="" other=""
    lc=$(typed_columns live_ch "$v")
    pc=$(typed_columns preview_ch "$v")
    if [ "$lc" != "$pc" ]; then
      printf '   %-28s columns differ: this checkout changes it\n' "$v"
      continue
    fi
    col=$(echo "$lc" | cut -f1 | grep -Fx -e day -e week | head -1 || true)
    case $col in
      day)  filter="day < '$cutoff_day'"; set=$open_days ;;
      week) filter="week <= toDate('$cutoff_day') - 7"; set=$open_weeks ;;
      *)    printf '   %-28s not compared: no day or week column\n' "$v"; continue ;;
    esac
    exprs=$(echo "$lc" | awk -F'\t' '{ printf "%s%s", (NR > 1 ? ", " : ""), ($2 ~ /Float/ ? "round(`" $1 "`, 6)" : "`" $1 "`") }')
    sql="SELECT toString($col), count(), sum(cityHash64(formatRow('TSV', $exprs))) FROM cardo.$v WHERE $filter GROUP BY 1 ORDER BY 1 FORMAT TSV"
    l=$(live_ch --query "$sql")
    p=$(preview_ch --query "$sql")
    n=$(printf '%s\n%s\n' "$l" "$p" | cut -f1 | sort -u | grep -c . || true)
    changed=$(LC_ALL=C comm -3 <(echo "$l") <(echo "$p") | awk -F'\t' '{ print ($1 == "" ? $2 : $1) }' | sort -u)
    if [ -z "$changed" ]; then
      printf '   %-28s same, %s %s%s\n' "$v" "$n" "$col" "$([ "$n" = 1 ] || echo s)"
      continue
    fi
    for d in $changed; do
      if echo "$set" | grep -Fxq "$d"; then opened="$opened $d"; else other="$other $d"; fi
    done
    if [ -n "$opened" ]; then
      any_open=1
      printf '   %-28s differs on %s%s: a session that started then was still open at the cutoff\n' "$v" "$col" "$opened"
    fi
    if [ -n "$other" ]; then
      ok=0
      printf '   %-28s DIFFERENT on %s%s\n' "$v" "$col" "$other"
    fi
  done
  if [ "$any_open" = 1 ]; then
    echo "   Expected: live keeps adding to the day a session started for as long as it runs. Compare"
    echo "   Grafana on the other days, or refresh once the session has ended."
  fi
  if [ "$ok" = 0 ]; then
    echo "   DIFFERENT is unexplained. From main it means the copy is wrong. From a branch, it should"
    echo "   be only the views the change touches."
  fi
}

migrate_preview() {
  echo "== Building cardo from this checkout, and applying its migrations to the preview"
  compose build migrate
  compose up -d clickhouse grafana
  compose run --rm -T migrate
}

copy_rows() {
  LIVE=$(live_container)
  PREVIEW=$(preview_container)
  run=$(date -u +%Y%m%dT%H%M%S)
  : > "$tmp/live-queries"
  cutoff=$(live_ch --query "SELECT formatDateTime(now('UTC') - INTERVAL $cutoff_lag_minutes MINUTE, '%Y-%m-%d %H:%i:%S', 'UTC') FORMAT TSV")
  echo "== Copying rows older than $cutoff UTC from '$live_project' (read-only)"

  local t
  for t in $tables; do
    if ! has_table live_ch "$t" || ! has_table preview_ch "$t"; then
      printf '   %-26s skipped: not in both stacks\n' "$t"
      continue
    fi
    # Only the columns both have, in the preview's order: a branch may add or drop one.
    local common dropped
    common=$(grep -Fx -f <(columns live_ch "$t") <(columns preview_ch "$t") | sed 's/.*/`&`/' | paste -sd, -)
    dropped=$(grep -Fxv -f <(columns preview_ch "$t") <(columns live_ch "$t") | paste -sd, - || true)
    live_ch --query "SELECT $common FROM cardo.$t WHERE $(time_filter "$t") FORMAT Native" > "$tmp/$t.native"
    preview_ch --query "TRUNCATE TABLE cardo.$t"
    # An empty table exports an empty file, and ClickHouse refuses an insert with no data.
    if [ -s "$tmp/$t.native" ]; then
      preview_insert --async_insert 0 --query "INSERT INTO cardo.$t ($common) FORMAT Native" < "$tmp/$t.native"
    fi
    printf '   %-26s %8s rows%s\n' "$t" \
      "$(preview_ch --query "SELECT count() FROM cardo.$t FORMAT TSV")" \
      "${dropped:+   (not in the preview, left out: $dropped)}"
  done
  # The views read the deployment's choices from settings (sql/clickhouse/005), so the copy, not
  # the migrate run above, decides what the preview's views name.
  echo "   The views use the live stack's settings: $(preview_ch --query "SELECT concat('organization artifacts ', if(org_artifacts = '', '(none named)', org_artifacts), ', minimum group size ', toString(min_group_size)) FROM cardo.settings_effective FORMAT TSV")"

  printf 'run=%s\ncutoff=%s\nlive_queries=%s\nlive_project=%s\n' \
    "$run" "$cutoff" "$(wc -l < "$tmp/live-queries" | tr -d ' ')" "$live_project" > "$state_file"
}

read_state() {
  [ -f "$state_file" ] || die "no copy has been made yet. Run: scripts/preview.sh up"
  run=$(sed -n 's/^run=//p' "$state_file")
  cutoff=$(sed -n 's/^cutoff=//p' "$state_file")
  expected=$(sed -n 's/^live_queries=//p' "$state_file")
  live_project=$(sed -n 's/^live_project=//p' "$state_file")
}

cmd_compare() {
  read_state
  LIVE=$(live_container)
  PREVIEW=$(preview_container)
  run="$run-compare-$(date -u +%H%M%S)"
  echo "== Rows older than $cutoff UTC: live against preview"
  local t ok=1
  for t in $tables; do
    has_table live_ch "$t" && has_table preview_ch "$t" || continue
    local l p
    l=$(live_ch --query "SELECT count() FROM cardo.$t WHERE $(time_filter "$t") FORMAT TSV")
    p=$(preview_ch --query "SELECT count() FROM cardo.$t WHERE $(time_filter "$t") FORMAT TSV")
    if [ "$l" = "$p" ]; then mark=same; else mark=DIFFERENT; ok=0; fi
    printf '   %-26s live %8s   preview %8s   %s\n' "$t" "$l" "$p" "$mark"
  done
  [ "$ok" = 1 ] || echo "   A difference here means rows arrived late, or the copy is wrong. Run refresh, then compare again."

  echo "== Migrations: what this checkout adds to the live stack's"
  local lm pm
  lm=$(live_ch --query "SELECT version FROM cardo.schema_migrations FINAL ORDER BY version FORMAT TSV")
  pm=$(preview_ch --query "SELECT version FROM cardo.schema_migrations FINAL ORDER BY version FORMAT TSV")
  echo "   live: $(echo "$lm" | wc -l | tr -d ' ') applied, newest $(echo "$lm" | tail -1)"
  echo "   preview only: $(grep -Fxv -f <(echo "$lm") <(echo "$pm") | paste -sd' ' - || true)"
  echo "   live only:    $(grep -Fxv -f <(echo "$pm") <(echo "$lm") | paste -sd' ' - || true)"

  echo "== Views: what this checkout adds or removes"
  local q="SELECT name FROM system.tables WHERE database = 'cardo' AND engine = 'View' ORDER BY name FORMAT TSV"
  local lv pv
  lv=$(live_ch --query "$q")
  pv=$(preview_ch --query "$q")
  echo "   preview only: $(grep -Fxv -f <(echo "$lv") <(echo "$pv") | paste -sd' ' - || true)"
  echo "   live only:    $(grep -Fxv -f <(echo "$pv") <(echo "$lv") | paste -sd' ' - || true)"

  compare_gold_views
}

cmd_verify_live() {
  read_state
  LIVE=$(live_container)
  local copy_run=$run
  run="$run-verify-$(date -u +%H%M%S)"
  local q="SELECT count() FROM system.query_log WHERE type != 'QueryStart' AND query_id LIKE 'cardo-preview-$copy_run-%' AND query_id NOT LIKE 'cardo-preview-$copy_run-%-%' FORMAT TSV"
  # The live server writes its query log once a minute (deploy/compose/clickhouse/config.d), so
  # the last copy's queries may not be there yet.
  local seen tries=0
  while :; do
    seen=$(live_ch --query "$q")
    [ "$seen" -ge "$expected" ] && break
    tries=$((tries + 1))
    [ "$tries" -le 9 ] || die "only $seen of the copy's $expected queries are in the live query log after 90 s"
    echo "   waiting for the live query log: $seen of $expected queries so far"
    sleep 10
  done
  echo "== Every query the preview has sent to '$live_project' in its query log (kept 3 days)"
  live_ch --format PrettyCompactMonoBlock --query "
    SELECT
        query_kind,
        Settings['readonly'] AS readonly,
        type,
        count() AS queries
    FROM system.query_log
    WHERE type != 'QueryStart' AND query_id LIKE 'cardo-preview-%'
    GROUP BY query_kind, readonly, type
    ORDER BY queries DESC"
  local bad
  bad=$(live_ch --query "
    SELECT count() FROM system.query_log
    WHERE type != 'QueryStart' AND query_id LIKE 'cardo-preview-%'
      AND (query_kind != 'Select' OR Settings['readonly'] != '1') FORMAT TSV")
  if [ "$bad" = 0 ]; then
    echo "PASS: every one is a SELECT, sent with readonly=1. The last copy's $expected queries are among them."
  else
    echo "FAIL: $bad of them are not a read-only SELECT."
    exit 1
  fi
}

case ${1:-} in
  up)
    live_container > /dev/null
    # Its ClickHouse keeps the password it was first started with, which is in the .env.preview of
    # the checkout that started it. Another checkout's would not open it.
    if [ ! -f "$env_file" ] && [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=$project")" ]; then
      die "a preview started from another checkout is still there. Run scripts/preview.sh down, here or there, first."
    fi
    ensure_env_file
    migrate_preview
    copy_rows
    echo
    echo "Preview Grafana: http://127.0.0.1:3002 (admin, with GRAFANA_PASSWORD from deploy/compose/$env_file)."
    echo "Live Grafana:    http://127.0.0.1:3001. Compare panels on days before ${cutoff%% *} that"
    echo "                 scripts/preview.sh compare shows as the same."
    ;;
  refresh)
    live_container > /dev/null
    [ -f "$env_file" ] || die "the preview is not set up. Run: scripts/preview.sh up"
    migrate_preview
    copy_rows
    ;;
  compare)     cmd_compare ;;
  verify-live) cmd_verify_live ;;
  down)
    if [ -f "$env_file" ]; then
      compose down -v --remove-orphans
    else
      # Started from another checkout. Removing needs no password, only values for compose to
      # read the files with; it finds the containers and volumes by project and service name.
      # Relative, because docker on Windows cannot open Git Bash's /tmp.
      printf 'CLICKHOUSE_PASSWORD=unused\nGRAFANA_PASSWORD=unused\n' > .env.preview-down
      docker compose -p "$project" --env-file .env.preview-down \
        -f docker-compose.yml -f docker-compose.preview.yml down -v --remove-orphans
      rm -f .env.preview-down
    fi
    rm -f "$env_file" "$state_file"
    echo "Preview removed, with its data. The cardo-preview:dev image is kept for the next build;"
    echo "docker image rm cardo-preview:dev removes it."
    ;;
  *)
    sed -n '2,19p' "$root/scripts/preview.sh" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
