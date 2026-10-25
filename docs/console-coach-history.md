# Operating the console, the coach, and imported history

This is the operator's guide to the surfaces added on 2026-10-18: the five-page console, the coach
and the transcript importer. It says what each piece does, how to run it, what it stores, and where
it stops being trustworthy. It supersedes the working handoff of the same date.

Everything below was exercised on macOS 15.7.7 with Docker Desktop against the reference stack on
loopback (ClickHouse `8124`, Grafana `3001`, collector `4319`/`8089`, console `8484`). Sections that
were **not** exercised are marked as such.

---

## 1. What changed, in one page

- **The board became a console.** Overview, Catalog, Teams, Settings and Coach are separate pages
  behind the same token→cookie auth. Install stopped being a peer tab and became a repair room
  reachable from Settings → Installation and from the Overview next action (ADR-0044).
- **Catalog** stores versioned enablement files on disk with metadata in ClickHouse, and gives
  validate → diff → draft → approve → rollback (ADR-0045).
- **Teams** loads a hashed `email,team` directory, prunes the previous generation, applies the
  minimum-group floor and reports coverage (ADR-0046).
- **The coach** answers one question per person: how did the last 28 days compare with the 28
  before, and what is the one fix worth making. It exists as a CLI, a console page and a signed
  per-person link (ADR-0048).
- **History import** turns existing `~/.claude/projects/**/*.jsonl` transcripts into the same bronze
  rows live telemetry writes, priced at Anthropic's published list prices. It is a one-time command
  the person runs: nothing is left running afterwards (ADR-0050).
- **Integrity fixes**: `audit()` errors fail the write; catalog version allocation is serialised;
  the teams commit marker is written last; settings writes are audited; `silver_session` excludes
  `cardo-probe-%` sessions (migration 013); service runs are labelled by `cardo.actor` /
  `cardo.workflow` and excluded from artifact usage (ADR-0036).
- **Retention** is now usable: `cardo prune`, a Settings field, `CARDO_RETENTION_DAYS`, automatic
  pruning after each poll, and bronze TTL that follows the chosen window in both directions.

---

## 2. Start the console

```sh
cardo up --web          # install/repair the stack, then serve the board
cardo ui                # serve the board without running installation
```

The board is loopback-only. The token is cached per port with mode 0600:

| Platform | Token file |
| --- | --- |
| macOS | `~/Library/Caches/cardo/token-8484` |
| Linux | `~/.cache/cardo/token-8484` |

Open `http://127.0.0.1:8484/?t=<token>`. The token becomes an HTTP-only cookie, so the address bar
can be cleaned afterwards. Rotating the token (delete the file and restart) invalidates every
console session **and every coach link** — see §4.4.

`cardo ui` flags: `-dir` (stack directory, default `deploy/compose`), `-port` (default `8484`),
`-no-open` (print the URL instead of opening a browser).

---

## 3. The five surfaces

### 3.1 Overview

Answers "is anything stale, and what do I do next". It renders warnings produced by the checks and
by the data, an `as of` timestamp with a Refresh action, and the most recent writes from
`settings_changes`. Counters are written honestly: `7 of 10 checks pass · 3 waiting` distinguishes a
wait from a failure, and a failing row renders `FAIL`.

Use it as the entry point. If a check fails, the next-action button leads to the exact repair step.

### 3.2 Settings

Every write is audited into `settings_changes`; an unchanged value does **not** create a new
version. Fields:

| Field | Meaning |
| --- | --- |
| Admin key | Claude Console admin key; written to `deploy/compose/.env` (0600), never shown again. Saving it starts the `poller` Compose profile. |
| Poll interval | How often the poller runs inside the stack. There is no cron on any machine. |
| Minimum group | Floor for naming a team or a home-grown artifact. Minimum 5 (ADR-0030). |
| Org artifacts | Where the organization's approved artifacts live. |
| Account type | `pilot_mode`: changes which installer steps are shown, not the stack composition. |
| Retention window | Days of raw rows to keep. 90 is the default and the organization may change it; `0` keeps everything and takes the TTL off. Changes the bronze TTL as well. |

Settings → Installation → *Open install and repair* is the old installer. It still owns the required
steps, including the new `history` step (§6).

### 3.3 Catalog

Versioned enablement artifacts (skills, `CLAUDE.md` bundles, settings fragments).

- Files are immutable on disk; metadata and status live in ClickHouse.
- The flow is **Validate → Diff → Save draft → Approve / Retire / Roll back**. Approval mutates
  status and is audited; it does not rewrite the file.
- Reach is computed from `gold_artifact_usage`, behind the same minimum-group floor.
- Large catalogs paginate.

If validation refuses a file, the diff is not offered — fix the file, do not force the draft.

### 3.4 Teams

- Upload `email,team` (CSV). Both columns are hashed at the edge; **no table column ever holds an
  email address** (ADR-0046).
- A batch marker is written last, so a half-finished upload never becomes the current generation.
- The previous generation is pruned after the new one is committed.
- The page reports coverage (how many engineers resolve to a team) and suppresses teams below the
  floor instead of naming them.

Re-uploading the same file is safe; it produces a new generation and prunes the old one.

### 3.5 Coach

The console page is the operator preview: type an email, get the same report the person would get,
plus a "copy link" action. Hero figures, a weekly sparkline, and at most one suggestion. The page
reads one pseudonym at a time and has no cross-person view.

CLI details, the link format and the privacy rules are in §4.

---

## 4. The coach, in detail

### 4.1 The report

```sh
cardo coach you@example.com
cardo coach you@example.com -days 14 -json
```

Flags: `-dir` (stack directory holding `.env`), `-days` (window, default 28), `-version` (salt
version, default `v1`), `-json`.

The report compares the chosen window with the window immediately before it and reports:

- sessions and prompts,
- cost,
- peak context tokens,
- tool calls and tool failures,
- artifacts loaded.

It ends with **at most one suggestion, and it names only what the fleet has shown**
([ADR-0041](https://github.com/amanachenko/cardo/blob/main/docs/adr/0041-the-coach-suggests-what-the-fleet-has-shown.md)):
an artifact the organization's engineers adopted, above the organization's minimum group size, with
the number of people who use it — "11 people here use `/review`, and you haven't tried it yet."
There is no catalog of patterns and tools and no fixed tip. The page says explicitly when it is
comparing a person with their own previous window. There is no rank, no percentile, no composite
score, and no other person's rows are read.

A pseudonym is checked for its shape before it reaches a query: 64 lowercase hex characters, which is
what a salted SHA-256 looks like. A value that is not one is refused rather than escaped.

### 4.2 What the numbers mean

- **Sessions** are distinct session ids with at least one prompt; three of the owner's transcripts
  had none and therefore produce no turn.
- **Cost** is Anthropic's published list price, not an invoice (§13, gap 1).
- **Peak context** is the largest `input + cache read + cache create` seen in one request.
- **Tool calls** are only available for live-telemetry sessions. Imported transcripts carry no tool
  decisions or durations, so the imported path reports `0` (§13, gap 2).

### 4.3 Pseudonyms

```sh
cardo pseudonym you@example.com          # salted value + salt version
cardo pseudonym you@example.com -salt … -version v2
```

The address is hashed and discarded; it is never stored. The salt lives with whoever owns the
pseudonyms. Rotating it splits one person into two, so custody belongs with the security team
(ADR-0006).

### 4.4 Per-person links

The console issues `GET /coach/<pseudonym>?k=<hmac>` where

```
k = HMAC-SHA256(boardToken, "coach:" + pseudonym), first 32 hex characters
```

Properties worth knowing before handing one out:

- **This is an own-laptop setup only.** A link opens on the machine that runs the console. How
  engineers on a team reach their page is still open (ADR-0048).
- Nothing is stored: the link is a pure function of the board token and the pseudonym.
- The link opens **only that person's report**. It cannot reach the console, other people, or any
  API.
- Rotating the board token revokes every link at once.
- Individual revocation does not exist yet. If one link must die, rotate the token and reissue the
  rest.
- The HMAC is checked with `hmac.Equal`; a wrong or missing `k` returns 401.

---

## 5. Importing existing history

### 5.1 What it reads, and what it throws away

`internal/transcripts` walks `~/.claude/projects/**/*.jsonl`. It extracts only:

- session ids and timestamps,
- model names,
- token usage (input, output, cache read, cache create),
- prompt lengths.

**Prompt text is measured and discarded.** Tool calls, file paths and any other content are not
read either.

### 5.2 Dry run, then write

```sh
cardo import                 # preview only; writes nothing
cardo import -write          # replace this machine's imported rows
```

Flags: `-dir` (transcript root, default `~/.claude/projects`), `-email` (default: your
`git config user.email`), `-write`.

The preview prints files, sessions, prompts, tokens, cost, the date span, the unpriced models (if
any) and the pseudonym. On the author's machine:

```
TRANSCRIPTS  415 files · 84 sessions · 2505 prompts
TOKENS       in 508543237 · out 12351649 · cache read 17253738503
COST         $211.63 at list price · 0 models unpriced
SPAN         2026-09-08 → 2026-10-18
```

`-write` first deletes prior imported rows for the sessions it is about to write, then inserts
OTel-shaped rows into `bronze_otel_logs`:

- `claude_code.user_prompt` and `claude_code.api_request`,
- `LogAttributes['cardo.import'] = '1'`,
- `LogAttributes['user.pseudonym']`,
- `LogAttributes['cardo.cohort'] = 'imported'`.

Rows have the same shape as live telemetry on purpose, so every existing view, threshold and check
applies without special cases. Re-running `-write` is idempotent: a second run produced the same
row count.

Requests are linked to prompts by walking the transcript's `parentUuid` chain, falling back to the
last prompt seen when the chain is broken.

### 5.3 Pricing

`internal/pricing` holds a table of Anthropic's published prices — input, output, cache read and cache
write, per token. Nothing is fetched: the binary contacts no host (INV-7). A dated model id resolves
to the same price as the bare one, so `claude-haiku-4-5-20251001` and `claude-haiku-4-5` bill the
same.

Unpriced models are reported, never silently zeroed: the preview prints them under `UNPRICED` and
the written row carries no `cost_usd` at all, so the views read a missing value as zero rather than
a false "this cost nothing".

An organization whose contract differs from the list price can replace the table with a local file at
`~/Library/Caches/cardo/pricing-override.json` (Linux: `~/.cache/cardo/pricing-override.json`).

---

## 6. The import, and what runs after it

Nothing runs after the import. There is no watcher, no service, and no file left behind on the
machine: `cardo import -write` is a command, and when it finishes, the machine is exactly as it was
before (ADR-0050).

The installer's `history` step (group **Data**) runs that command, and only that command. It is
**opt-in**: it is not part of "install everything", and in the board it carries an unticked
checkbox. A person who never ticks it is in exactly the position INV-1 describes.

A session that already arrived through live telemetry is skipped, and the importer says how many it
skipped:

```
SKIPPED      12 sessions already collected live
WROTE        318 rows for 31 sessions
```

Counting such a session twice would double its cost, tokens and prompts, and would leave its rows
with a random `cohort` in `silver_session`.

---

## 7. Checks

`cardo check` is read-only and has ten rows:

| # | Row | What it proves |
| --- | --- | --- |
| 01 | DOCKER | Docker and Compose are reachable |
| 02 | SECRETS | `.env` is 0600 and the salt is 64 hex characters |
| 03 | PORTS | All five loopback ports are listening |
| 04 | CLICKHOUSE | Ping succeeds on 8124 |
| 05 | GRAFANA | Health endpoint answers on 3001 |
| 06 | COLLECTOR | OTLP and hook endpoints accept on 4319 / 8089 |
| 07 | FIELDS | The latest **real** session has pseudonym, app version, OTel rows, hook rows and prompts |
| 08 | ADMIN DATA | `bronze_actor_day` has a recent day |
| 09 | ACTOR TYPES | Only `user_actor` and `api_actor` appear |
| 10 | GOLD | `cardo_ro` can read gold and is refused on bronze and silver |

Semantics that matter:

- **Pending is not failing.** `WAIT` means the check cannot judge yet (no live data, no admin key).
  `FAIL` means it looked and the answer was wrong.
- `cardo check --proof` turns a pending FIELDS row into a failure: it exits non-zero unless one of
  your own sessions has the fields the views read.
- FIELDS deliberately ignores imported sessions (`cardo.cohort = 'imported'`), seeded demo
  identities (`demo-%`) and installer probes (`cardo-probe-%`). Transcripts cannot carry hook rows
  or an app version, so they can never satisfy it; treating them as evidence would make the check
  lie in the reassuring direction.

Expected healthy output with imported history but no live session:

```
7 / 10 steps complete
Checks that can run pass; 3 waits for real work. cardo check --proof requires it.
```

`ACTOR TYPES` documents a finding rather than merely guarding a list: `api_actor` is **a person using
an API key**, not a service. The earlier exclusion was wrong and was reverted (ADR-0036). If the API
starts returning `service_account_actor`, the check fails loudly and names it.

---

## 8. Retention

Raw rows are the only rows; silver and gold are views. Deleting raw rows therefore shortens every
trend.

```sh
cardo prune -days 30 --dry-run    # count only
cardo prune -days 30              # delete older raw rows
cardo prune                       # use CARDO_RETENTION_DAYS
```

The same window is available in Settings. **90 days is the default**, and the organization may
change it (ADR-0053); the console shows the effective value and says `(default)` when nothing has
been chosen. Saving a window moves the tables' TTL at once, so the number on screen is the one in
force rather than one that waits for the next poll. `0` keeps everything and takes the TTL off, so
nothing expires on its own either. The poller prunes after every run, and the bronze TTL is
rewritten to the same window in **both** directions, so raising the window back does not leave a
30-day TTL behind. TTL is the privacy control, not a storage tidy-up (ADR-0016, ADR-0053).

`cardo prune` and `cardo import` read the stack's `.env`, so they work from a plain shell. The
window a prune uses is the one in force: the value the console saved, or `CARDO_RETENTION_DAYS` when
the console has never been opened.

---

## 9. Data model and integrity guarantees

| Object | Kind | Notes |
| --- | --- | --- |
| `bronze_otel_logs` | MergeTree | Live telemetry **and** imported rows; imported ones carry `cardo.import=1`, `cardo.cohort=imported` |
| `bronze_hook_events` | MergeTree | Reduced hook payloads (allowlist only) |
| `silver_session`, `silver_turn`, `silver_api_request`, `silver_tool_call` | Views | Exclude `cardo-probe-%` sessions (migration 013) |
| `gold_cost_daily`, `gold_fleet_adoption`, `gold_artifact_usage`, … | Views | `gold_artifact_usage` excludes service runs |
| `settings`, `settings_effective`, `settings_changes` | Tables | Immutable versions; every write audited |
| `settings_artifact_versions` | Table | Catalog metadata; files stay on disk |
| `settings_teams`, `settings_teams_batches` | Tables | Hashed directory and commit markers |

Guarantees added in this slice:

- A failed `audit()` now fails the write instead of being swallowed.
- Catalog version allocation is serialised, so two concurrent uploads cannot claim the same version.
- The teams batch marker is written last.
- Settings writes are audited; unchanged values create no version.
- Service runs are labelled `actor_kind` / `workflow` from `cardo.actor` / `cardo.workflow`;
  `deploy/ci/github-actions.card.yaml` is the ready-made CI block; such runs count as work and cost,
  never as a person (ADR-0036).

Migrations `010`–`013` are embedded in the binary and applied on every run. There is no separate
migrate step to forget.

---

## 10. Privacy and security rules that the UI relies on

- No email address is stored anywhere: the coach and the team directory hash at the edge, and the
  importer hashes the `git config user.email` into `user.pseudonym`.
- Prompt text is never written by the importer; only its length is kept.
- The console is loopback-only and token-gated; the coach link is HMAC-signed and read-only.
- The Admin API endpoint is pinned. A stand-in is allowed only on loopback (`127.0.0.1`, `::1`,
  `localhost`) so an admin key cannot be pointed at a host someone else runs (ADR-0047).
- The binary contacts no host at all. Prices come from a table inside it, and the only other
  address it knows is the Anthropic API it was pointed at. Transcripts are read locally and never
  sent.
- The key, the salt and the Grafana passwords live in `deploy/compose/.env` (0600).
- There is no hosted option, no default salt and no unsalted mode (ADR-0013, ADR-0006).

---

## 11. Operational recipes

**Get your own numbers**

```sh
./bin/cardo coach "$(git config user.email)"
```

**Hand a person their report**

Console → Coach → type the address → *Copy link*. Rotate the board token to revoke all links.

**Bring in history from this machine once**

```sh
./bin/cardo import            # read the preview first
./bin/cardo import -write
./bin/cardo check
```

Nothing runs afterwards. To bring in later sessions, run the same command again; sessions already
collected live are skipped, so nothing is counted twice.

**Change retention**

Settings → Retention window, or `cardo prune -days N`. 90 days is the default; `0` keeps
everything and takes the TTL off.

**Remove synthetic demo data**

```sql
ALTER TABLE cardo.bronze_otel_logs
  DELETE WHERE LogAttributes['session.id'] LIKE 'demo-%' SETTINGS mutations_sync = 1;
```

Silver and gold are views, so they empty immediately. Verify with
`cardo coach demo@cardo.local` (expect "No sessions for this pseudonym in this window") and by
counting `silver_session` rows with `session_id LIKE 'demo-%'` (expect 0).

**Prove the Admin API path without a key**

```sh
./bin/cardo mock-admin -port 8791 -days 2 -users 2 -api-keys 1
./bin/cardo poll -base-url http://127.0.0.1:8791 -days 2 -store clickhouse
```

---

## 12. Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `FIELDS  cannot read back` + `invalid character '…'` | A ClickHouse query parsed as JSON without `FORMAT JSONEachRow`, so the TSV body was handed to `json.Unmarshal` | Append `FORMAT JSONEachRow` to every query whose body is parsed. Fixed in `internal/launcher/readback.go` on 2026-10-18 |
| `FIELDS  … missing app_version, hook rows` | The newest session was an imported or demo one, which can never carry hooks | The check now excludes `cohort='imported'` and `demo-%`; run Claude Code once in an instrumented project to produce a real session |
| `Tool calls 0` in the coach | Imported transcripts carry no tool calls | Expected; label the figure "live only" or add hook capture |
| `unpriced models` in `cardo import` | The model is not in the published table, or the transcript has a typo | Add it to `~/Library/Caches/cardo/pricing-override.json`, or fix the model name in the transcript |
| Console refuses the link | Token rotated, or the port changed (token files are per port) | Open `?t=<token>` from `~/Library/Caches/cardo/token-<port>` |
| Coach link 401 | `k` missing, wrong pseudonym, or the board token rotated | Reissue from Console → Coach |
| `ACTOR TYPES` FAIL naming a type | The API returned an undocumented actor kind | Read the name in the row; do not widen the allowlist without an ADR |

---

## 13. Known gaps and risks

1. **Cost is Anthropic's published list price, not the invoice.** Discounts, credits and batch pricing
   are not modelled.
2. **Imported history has no tool calls.** Transcripts contain no decisions or durations; only live
   telemetry does.
3. Three of the 84 imported sessions have no prompts and therefore no turns (81 appear in
   `silver_turn`).
4. **The import sees one machine.** Cross-machine history needs export/import (roadmap, Step 3,
   absent).
5. **The Admin API has never been called with a live key.** Values are documentation-backed and
   guarded by `ACTOR TYPES`, but the first real poll is still the proof.
6. `pilot_mode` changes which installer steps appear; it does not change stack composition.
7. **Coach link revocation is all-or-nothing** (rotate the board token).
8. The team path still needs `cardo connect --project` per repository; user-level instrumentation is
   the planned fix and needs explicit consent because it edits `~/.claude/settings.json` for every
   project.

---

## 14. Verification log (2026-10-18)

| Check | Result |
| --- | --- |
| `go vet ./...`, `go test ./...` | green |
| `cardo check` | `7 / 10` complete; FIELDS, ADMIN DATA, ACTOR TYPES wait |
| `cardo check` FIELDS parsing | was `FAIL invalid character 'd'`; fixed with `FORMAT JSONEachRow`, then narrowed to real sessions |
| Import dry run | 415 files, 84 sessions, 2505 prompts, $211.63, 0 unpriced |
| Import write | 51 509 rows; a second run produced the same count |
| `silver_turn` for the owner | 81 sessions with prompts, 2505 prompts |
| `cardo coach` (28 d) | 76 sessions, 2 335 prompts, $171.20 vs $7.39, peak context 374 910 vs 109 706 |
| Per-person link | `/api/coach/open` → 200 with the right HMAC, 401 with a wrong one |
| Installer step | `POST /api/run {"step":"history"}` → state `ok` |
| Mock Admin API | 6 records (4 `user_actor`, 2 `api_actor`); unknown type → `ACTOR TYPES` FAIL, exit 1 |
| Retention | Dry run saw 1 old row; the real run deleted it; TTL moved 30 → 90 |
| Probe filter | Inserted a `cardo-probe-…` row → `silver_session` stayed 0 |
| Demo data removed | 48 bronze rows deleted; `silver_session` 93 → 81; `cardo coach demo@cardo.local` empty |

To reproduce the read-only half at any time: `cardo check`, `cardo import` (preview),
`cardo coach <your address>`.

---

## 15. File map

- **ADRs**: `docs/adr/0044-console-surface.md`, `0045-catalog-storage.md`,
  `0046-hashed-team-directory.md`, `0047-loopback-admin-stand-in.md`,
  `0048-coach-in-the-console.md`, `0050-local-transcript-import.md`,
  `0053-retention-default-and-organization-choice.md`,
  `0054-both-actor-types-are-people.md`. `0036-service-runs-are-not-people.md` is unchanged; the
  reasoning that changed is in ADR-0054, and what shipped is in `docs/design/architecture.md`.
- **Console**: `internal/launcher/server.go`, `server_overview.go`, `server_catalog.go`,
  `server_teams.go`, `server_coach.go`, `step_history.go`, `readback.go`,
  `web/{index.html,app.js,cardo-ui.css}`.
- **Data**: `sql/clickhouse/010_catalog.sql`, `011_teams.sql`, `012_service_runs.sql`,
  `013_probe_filter.sql`; `internal/{catalog,directory,coach,transcripts,pricing,retention,mockadmin}`.
- **CLI**: `cmd/cardo/{main.go,up.go,import.go,prune.go,pseudonym.go,coach.go,mockadmin.go}`.
- **Design record**: `docs/design/admin-console.md` (slices and corrections),
  `docs/design/quickstart-launcher.md`, `PRODUCT.md`, `docs/roadmap.md`.
- **Tests**: `internal/launcher/*_test.go`, `internal/{coach,transcripts,pricing}`, `test/e2e_admin_test.go`,
  `test/install_smoke_test.go`.
