# Admin console — design brief

Status: **slices 1–3 built** (shell, Overview, Catalog, Teams, Settings). Mode: **Operate** (task completion, scanability,
earned familiarity). Visual world: **inherited** from `docs/design/DESIGN.md` and `cardo-ui.css` — the
console extends the board, it does not restyle the product.

## 1. Job and audience

The operator — a platform or security engineer standing Cardo up for one organization, on one host,
in a terminal-adjacent state of mind. They arrive with three jobs, in this order:

1. **Prove the stack works** (already built: Install).
2. **Prepare a pilot**: load the team directory, publish the few artifacts the pilot will measure,
   set the knobs that shape the views.
3. **Keep it honest**: see whether both ingestion paths are fresh, what changed, and what is waiting
   for evidence.

Not in this console: leaders (gold-only Grafana), engineers (one settings file, later a personal
page), and any per-person view.

## 2. Outcome and proof

Success is a single sentence the operator can say without opening a terminal: *"The stack is fresh,
the directory covers 96% of sessions, four artifacts are approved and reaching people, and nothing
needs my attention."*

The surface proves it rather than asserting it: every number comes from ClickHouse, every settings
write re-runs the step that owns it, and every artifact shows the diff it introduced.

## 3. Composition — three structures, one recommendation

The world is settled, so this is a composition choice. (The concept dealer was run as the skill
requires; it dealt media-feed and acetate-manual worlds that fail audience identification for a data
console, so the incumbent system stays and the choice is structural.)

**A. Rail of surfaces (recommended).** The existing left rail stops being a step list and becomes the
product navigation: Install · Overview · Catalog · Teams · Settings. Same width, same typography,
same six states. The install wizard is the first item, not the whole rail. Inside Catalog and Teams a
master–detail split reuses the rail/pane pair the operator already reads.

**B. Master–detail first.** One list of objects (artifacts, teams, settings, steps) on the left, the
selected object's editor on the right; Install becomes a saved view. Fast for a catalog-heavy week,
weak for the pilot's first hour, and it hides the "is my stack alive" question behind a filter.

**C. Status strip + drill-down.** A full-width strip of four facts on top (stack, poller, sessions,
warnings), each opening a page. Best at a glance, worst for the two tasks that need a form (upload,
team directory) and it duplicates the rail the operator already learned.

Recommendation: **A**, because it preserves the one thing already learned (rail = where I am), gives
Install peers rather than a demotion, and lets B's master–detail live inside the two screens that
actually need it.

## 4. Scope and boundaries

In scope: five screens, local-only, one operator, English, desktop first with a collapsed rail under
900 px.

Untouched: the install steps and their engine, the token/cookie auth, the Grafana provisioning, the
CLI semantics, the design tokens. The console adds no second implementation of any step.

Anti-goals: a file manager, a wiki, a people directory, an approval bureaucracy, a modal-first UI, a
settings dump with no consequences explained.

## 5. Data and realistic ranges

| Object | Typical | Max seen in a pilot |
|---|---|---|
| Artifacts | 12–40 | a few hundred |
| Artifact versions each | 2–5 | 30 |
| Teams | 6–12 | 150 |
| Team file rows | 150–600 | 20,000 |
| Catalog table rows on screen | 20–50 | paginated at 100 |

Realistic empty cases matter as much as full ones: no directory yet, no approved artifact, poller off,
no session, a file with three malformed rows out of four hundred.

## 6. Screens

### 6.1 Install (existing)
Unchanged composition. Gains the rail as navigation; the step list becomes the Install page body.
Completion keeps "Installation complete" and the proof line.

### 6.2 Overview
Four fact cells in one row, then one "next action", then recent changes.

- **Stack** — `6 services up · 4 healthy` or the first failure with cause/recovery (reuses step rows).
- **Poller** — `last day 2026-10-17 · 1,204 rows` or `off — no admin key`.
- **Sessions** — `last session 2h ago · 7 days · 43 sessions` or `waiting for the first session`.
- **Warnings** — count of drift, unknown fields, days missing, artifacts below five users (count only).

Below: **Next action** — exactly one, chosen by precedence: failing service → no admin key → no
directory → no approved artifact → no session → nothing. Each links to the screen that fixes it.

Below: **Recent changes** — last five rows from `settings_changes` (when, what, by "operator").

### 6.3 Catalog
Table, one row per artifact at its current version:

| Kind | Name | Owner | Status | Version | Reach 7d | Updated |
|---|---|---|---|---|---|---|
| skill | `review-pr` | platform | approved | v3 | 62% (24 people) | 2 days ago |

- Kind filter is a segmented control (all · skill · command · subagent · mcp · reference · bundle ·
  cohort); status filter (draft · approved · retired); search matches name and owner.
- Reach comes from `gold_artifact_usage`, suppressed below five people as "—".
- Sort: reach, updated, name. Default: needs-attention first (draft older than a day, retired-but-used).

Row opens the detail pane:

- **Header**: name, kind, owner, status, current version, hash.
- **File**: rendered as text (markdown as text, never HTML), with "Download".
- **Versions**: newest first, each with date, note, hash prefix, and actions `Approve`, `Rollback`.
- **Reach**: one small chart from gold; if below five, the number is not shown.
- **Used by teams**: names of teams at or above the floor, never people.

**Upload** (inline, four steps, no modal):
1. Kind (segmented) + file drop.
2. Validation result: type check, size, frontmatter, secret scan. Failures name the line.
3. **Diff against current**: `+3 commands, −1 skill, ~mcp server "figma"` with the changed lines.
4. Save as **draft**; the artifact does not reach anyone until approved. Approve is one click on the
   detail pane, recorded in `settings_changes`.

### 6.4 Teams
- **Directory upload**: CSV `email,team` or `team` only. Preview shows counts, never addresses:
  `421 rows · 418 unique · 2 duplicates · 3 malformed on lines 17, 88, 402 · 2 teams below five`.
- On save, emails are hashed with the stack salt (`pseudonym.Scrubbed` path). The file is not kept;
  only `team + pseudonym + source_hash + loaded_at` land.
- **Coverage**: `96% of sessions in the last 7 days map to a team · 7 teams · 18 unassigned`.
- Team table: team, people (`—` below five), sessions 7d, top artifacts (floor applied).
- Empty state teaches the format and the privacy behaviour in one sentence each.

### 6.5 Settings
Grouped, one consequence per group, no free-form config:

- **Ingestion** — admin key (write-only, configured/not), poll interval (`24h`), poller status,
  `Test key` and `Run now`.
- **Privacy** — minimum group size (default 5, why it exists), salt holder line (env path, never the
  value), retention days for raw layers.
- **Catalog** — default owner, `CARDO_ORG_ARTIFACTS` list (the inventory the operator rolled out).
- **Stack** — ports (read-only, from compose), compose directory (read-only), versions.

Each field shows current value, help text, and `Apply`; applying runs **only** the owning step and
reports its cause/recovery inline. No global "Save".

## 7. Interaction and layout

- Rail 280 px on desktop, 56 px icon rail at 900–1200, a `<select>` of surfaces below 900. The install
  step list collapses under "Install".
- Tables are the density unit: 34 px rows, mono for identifiers and numbers, 1 px hairlines, no cards.
- Focus: `/` focuses search, `g i|o|c|t|s` jumps between surfaces, `Esc` closes a detail pane,
  `Cmd+Enter` applies the focused form. Every affordance is a real `<button>`/`<a>`.
- Feedback: inline status line under the action, 150–250 ms transitions, no spinners without a label,
  skeletons only for table bodies.
- State vocabulary is fixed: rest, hover, focus, active, disabled, loading, error, warning, success —
  the same on every screen.
- Copy: formal English, imperative, no exclamation, errors are cause + recovery.

## 8. States

- **Empty catalog**: "No artifacts yet. Upload the first one, or let the next poll show what teams
  already use." Two actions.
- **Empty teams**: format + hashing note + example row.
- **Poller off**: Overview next action points to Settings → key; Catalog still works.
- **No sessions**: reach cells read `—` and the Why column explains "needs data".
- **Upload rejected**: per-line reasons, file kept in the drop zone for a retry.
- **Approve after a concurrent upload**: version mismatch is detected by hash; the pane reloads and
  says which version is now newest.
- **Offline ClickHouse**: every screen shows a single top banner with the cause and the step to run;
  forms become disabled rather than failing one by one.

## 9. Data model (new tables)

- `settings_artifacts(kind, slug, version, name, owner, status, content_hash, arrived_via, released_on, updated_at)`
- `settings_artifact_versions(slug, version, content_hash, path, note, created_at)` — files live on
  disk under `deploy/catalog/<kind>/<slug>/<version>/`, never in ClickHouse.
- `settings_teams(team, pseudonym, source_hash, loaded_at)` — no email column exists by construction.
- `settings_rollouts(slug, released_on, cohort_kind, cohort)` — for before/after reach (ADR-0040).
- `settings_changes(id, at, action, object, before_hash, after_hash, note)` — the audit trail.

Writes go through the existing migration runner; no new transport.

## 10. API

- `GET /api/overview` — facts + next action.
- `GET /api/catalog?kind=&status=` — rows; `GET /api/catalog/{slug}` — detail + versions.
- `POST /api/catalog` — multipart: file + kind + owner; returns `{version, hash, validation, diff}`.
- `POST /api/catalog/{slug}/approve|retire|rollback` — body `{version}`.
- `POST /api/teams` — `{csv}` → preview; `POST /api/teams?apply=1` — hashes and saves.
- `GET/POST /api/settings` — read values; write runs the owning step and streams its events.

All behind the existing token → HttpOnly cookie; all writes are POST; every response carries a
`changes` entry id.

## 11. What slice 1 shipped (2026-10-18)

- The rail became a product surface strip — Overview · Install · Settings — with the surface kept in
  localStorage and the header line following it.
- **Overview** reads `/api/overview`: stack (the eight read-only checks), sessions in seven days,
  Admin API state, warning count, exactly one next action, and the newest session as a proof card
  (app version, prompts, OTel/hooks, artifacts loaded, no identity beyond the pseudonym).
- **Settings** reads and writes real sources: the admin key and interval go through the existing
  `/api/admin` path; the minimum group size is written as a new versioned row in `cardo.settings`
  (refused below five); `org_artifacts` is written to both the settings table and `.env`, so the
  collector and the views agree. Ports, directory and version are read-only.
- Install is unchanged and still the only writer of stack state.

Not yet in the console: Catalog, Teams, recent changes, retention. They are slices 2–3 of this brief.

## 11a. What slice 2 shipped (2026-10-18)

- `cardo.settings_artifact_versions` and `cardo.settings_changes` (migration `010_catalog.sql`), files
  under `<compose-dir>/catalog/<kind>/<slug>/<version>/content<ext>` (ADR-0045).
- Four-step upload inline on the page: pick a kind, choose a file, validate, read the diff against the
  approved version, save as a draft. Validation names every problem at once: slug rules, extension per
  kind, frontmatter for skill/command/subagent, headings for a reference, JSON keys for mcp/bundle/
  cohort, a 256 KiB ceiling, and a secret scan that refuses obvious keys.
- Table with kind filter and search, status and version columns, and reach from
  `gold_artifact_usage` — suppressed below the minimum group size, shown as `—`.
- Detail pane: metadata, version table with Approve/Restore, and the file as text (never HTML).
- Every write appends a row to `settings_changes`; approval is one click and keeps history.

Teams and "Recent changes" on Overview remain for slice 3.

## 11b. What slice 3 shipped (2026-10-18)

- `cardo.settings_teams` and `cardo.settings_teams_batches` (migration `011_teams.sql`, ADR-0046).
- Directory surface: paste `email,team`, Preview (counts, duplicates, malformed lines with line numbers,
  teams below the floor) and Load. Malformed lines refuse the whole load with a 422; nothing is written.
- Hashing uses the stack's `CARDO_SALT` and `CARDO_SALT_VERSION`, lowercased emails, the same
  `pseudonym.Hasher` the collectors use. The uploaded text is not stored; the batch keeps its SHA-256.
- Coverage answers three numbers: sessions in 7 days, mapped sessions and percentage, distinct
  unassigned pseudonyms. Teams below `min_group_size` are not rendered; the screen reports how many
  were hidden.
- Verified on the live stack: 3 people / 2 teams loaded, `SELECT count() FROM settings_teams WHERE
  position(pseudonym,'@') > 0` returns 0 — no address reaches storage.

Remaining from the brief: retention.

## 11c. Corrections after the critique (2026-10-18)

Fixed, in the order the critique ranked them:

- **Overview tells the truth.** `N of M checks pass · K waiting` instead of counting pending rows as
  passes; warnings list every failing and pending check; the next action now includes "load the team
  directory" and "approve the first artifact" before it will ever say "nothing needs attention";
  `as of HH:MM:SS` and a Refresh action; Recent changes reads `settings_changes`.
- **One status vocabulary.** `Ready: <next>` became `Next: <next>`; the duplicate Admin API card on
  the Install start screen was deleted (Settings owns the knobs); the HTML it left behind was
  repaired — the five surfaces are children of the shell again.
- **Admin key stops lying.** An empty key is a no-op rather than a poller restart, the first save
  without a key is a 400, a poller that failed to start is a 502 with the cause, and the write is
  audited.
- **Catalog safety loop.** `POST /api/catalog/validate` returns problems and the diff without writing;
  the form validates, shows the diff, then saves on a second confirmed click. Approve needs a second
  click, Retire and Roll back exist (rollback retires newer versions), errors are inline instead of
  `alert()`, the form resets on open, and the nav shows the draft count.
- **Upload validation is real.** The extension comes from the uploaded file name, not from a
  synthesized name; JSON kinds are parsed, not substring-matched.
- **Concurrency and commits.** Uploads for a slug are serialized (version + file + row under one
  mutex); the team batch marker is written *after* the rows; old batches are pruned to two
  generations; the directory stores `salt_version` and only joins sessions hashed with the same one.
- **Audit is mandatory.** `audit()` returns its error; upload, status, teams, settings and the admin
  key fail loudly if the audit line cannot be written; settings writes are audited and unchanged
  values no longer create a version; `.env` is not written when the salt is invalid.
- **Design system.** `--line` was undefined and made every new border paint in text colour; it now
  resolves to `--rule`. `DESIGN.md` was contradicting the code: green is the success colour and says
  so. Inline colours in Overview were replaced with classes.
- **Reach is keyed by kind and name**, so a skill and a command that share a name no longer collapse.
- **Timeouts are a policy, not an accident.** The overview runs live checks behind a 3 s cache and gets
  25 s; every other read gets 12 s; `?fresh=1` (Refresh) bypasses the cache. An abort becomes "no answer in
  time — the stack is busy; press Refresh", with a Try again button, never a DOM exception.
- **Navigation has addresses.** `#/catalog/<slug>` and per-surface hashes, `document.title` follows
  the surface, Back works, and the navigator wraps on narrow screens; tables scroll instead of
  stretching the document.
- **Teams accepts a file**, capped at 2 MB, with the paste field kept as a fallback.

## 11d. Visual pass (2026-10-18)

- One title origin: every surface, wizard included, starts its H1 at (177, 110); the wizard gained an
  `Install` H1 with the step name demoted to h2; DESIGN.md no longer claims a centred wizard.
- The number row is data: mono with tabular numerals, four fixed columns, values on one line, labels
  on one baseline, and a tone that follows the state (a `waiting` value is no longer painted green).
- Tables have geometry: fixed `<colgroup>` widths, first cell flush with the heading, ellipsis instead
  of ragged columns, and the blue uppercase rule scoped to the status column only.
- One shape for empty, error and loading: skeletons in Catalog, Settings and Teams; `.state-empty` and
  `.state-error` replace bare header rows and lone `Loading…`; every fetch has an 8 s timeout.
- Prose has one measure (66ch) and one hint style; native select/file/textarea chrome replaced;
  section rules meet the content column; the pinstripe ground is gone.
- One filled action per screen: the selected path is an amber word and rule, the artifact action sits
  in the toolbar, Settings actions share one column, and the wizard numbers only counted steps.

Verified with a scripted CDP pass (headless Chrome + `Emulation.setDeviceMetricsOverride`): all five
surfaces report H1 at 177×110, document width equals viewport at 1470 and at 390, and the mobile
navigation wraps to two rows instead of clipping.

Still open: WCAG items were explicitly excluded from this pass (labels, aria-live, focus styling);
the catalog detail still sits below the table rather than beside it; no pagination; retention.

## 12. Slices and estimates

1. **Shell, Overview, Settings** (2 days) — rail as navigation, four facts, next action, settings
   forms wired to steps. No new tables.
2. **Catalog read + upload + approve** (2.5 days) — two new tables, disk layout, four-step upload,
   diff, version list, reach column.
3. **Teams** (1.5 days) — hashed directory load, coverage, floor applied everywhere.
4. **ADRs and docs** (0.5 day) — 0044 console surface, 0045 catalog storage, 0046 hashed directory.

Acceptance per slice is a real number on screen from the live stack plus the failure states above.

## 12. Constraints and open decisions

- Storage: files on disk, metadata in ClickHouse (recommended; keeps the binary free of blobs).
- Approval model: `draft → approved`, one click, recorded (recommended); the alternative is immediate
  publish, which has no undo and no audit.
- Team file format: `email,team` with `team`-only rows allowed (recommended).
- Whether the console shows the operator's own session first (nice for trust) — open.
- Whether `retention` writes affect raw tables immediately or on the next poll — open.

## 12. Navigation decision (ADR-0044)

Install left the tab strip. Four surfaces remain — Overview, Catalog, Teams, Settings — and the
installer is the repair room: reached from Settings → Installation (account type, project directory,
Open install and repair, Run checks) and from the Overview next action when a check fails. Account type
is stored as `pilot_mode`; it changes which steps are shown, not what is installed. "Reinstall" is not
a word the product uses: `cardo up` repairs in place.
