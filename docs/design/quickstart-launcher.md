# Quickstart launcher — design

Status: draft for review. Closes the first item of Step 1 in the [roadmap](../roadmap.md),
"a quickstart for one machine, and `cardo check`".

**Implemented in this change:** `cardo up`, `cardo check`, `cardo ui` and `cardo connect` in
`cmd/cardo/up.go`, with the engine in `internal/launcher/`. Tests cover secrets, `.env` permissions,
idempotency, port detection, read-only behaviour, the token, the state endpoint and the settings
installer. The board at `internal/launcher/web/` is driven by that engine: a button runs the same
step the CLI runs, and the installation completes without a terminal.

The launcher is one command that takes a person from nothing to a verified row from their own
session, and then asks nothing of them again.

```
cardo up
```

It opens `http://127.0.0.1:8484` and serves the board in
[`prototype/quickstart.html`](prototype/quickstart.html). The visual system is
[`DESIGN.md`](DESIGN.md); the stylesheet is [`cardo-ui.css`](cardo-ui.css).

## What it must achieve

1. **A real decision is possible at the end.** Not "the stack is up": the person knows which of
   their tools is spreading, or that their rules hold, from data that arrived.
2. **Nothing to maintain afterwards.** No cron, no re-run ritual, no dashboard editing, no secret
   rotation by hand. Updating is the same command.
3. **Nothing to remember while working.** One settings file per machine (or per project), installed
   once. No flag on every `claude` invocation, no labelling of sessions, no habit.
4. **Nothing leaves the machine or the network.** No telemetry in the launcher, no hosted lookup,
   no "send diagnostics".

## Two paths, one board

The first screen asks one question, because it is the only one that changes the work: a single
laptop, or a team pilot. Both use the same stack and the same settings file; the pilot adds the
things a second person needs.

| | Solo (`~10 min`) | Pilot (`~40 min`) |
|---|---|---|
| Preflight | Docker, ports, disk | same, plus a hostname and TLS if served over the network |
| Secrets | salt + two passwords into `.env` (0600) | same, with custody recorded |
| Start | `docker compose up -d --wait` | same |
| Verify | OTLP + hook + read-back + dashboard query | same |
| Own session | one row appears | same |
| Connect Claude Code | one settings file | managed-settings through MDM |
| Leaders' logins | — | separate DB user, refused on raw tables |
| Engineer notice | — | one page |

## The board

Each row is a check with four states: waiting, running, done, failed. A row opens in place and
always shows the command it will run. The ticker carries the one sentence that matters right now.

| № | Row | What it proves | How |
|---|---|---|---|
| 01 | Docker | the daemon answers, compose is v2+ | `docker info` |
| 02 | Ports | 8124, 9001, 3001, 4319, 8089 are free | listen check, naming the holding process |
| 03 | Passwords and salt | `.env` exists, is `0600`, salt is 64 hex | generate, write, re-read and validate |
| 04 | Stack | four services healthy, migration applied | `docker compose up -d --wait` |
| 05 | Data arrived | a synthetic session and a hook are readable back | write, read, run a dashboard query |
| 06 | Own session | one row from the person's real Claude Code | read the newest session in the window |
| 07 | Claude Code | settings installed once, hooks answering | ping each of the twelve hooks |
| 08 | Leaders' logins (pilot) | the read-only user is refused on raw and per-person tables | attempt the read, expect the refusal |
| 09 | File on machines (pilot) | the bundle is deployed and reporting | count distinct machines reporting |
| 10 | Notice (pilot) | the page exists with salt custody and settings listed | render and check required fields |

Rows 05 and 06 are the aha. Everything before them is plumbing, and the board says so by grouping:
"machine", "start", "data", "people".

## Errors

Every failure is one row, one cause, one recovery, and an explicit statement of what will not be
touched.

| Failure | What is shown | Recovery |
|---|---|---|
| Docker not running | "демон Docker не отвечает" | start Docker Desktop, press retry |
| Port held | port, process name, pid | stop it, or `cardo up --hook-port 8099` |
| `.env` exists | will not overwrite; shows which keys are set | `cardo up --rotate` or continue |
| Salt present but invalid | "соль должна быть hex, 32+ символа" | `cardo secret --rotate-salt` |
| Migration failed | last migration id and error | `cardo up` retries; data kept |
| Collector refused OTLP | the REFUSED reason from the log | fix salt, retry row 05 |
| Hooks not answering | which of the twelve timed out | `cardo connect --repair` |

Nothing is ever "reinstall from scratch". `cardo up` is idempotent: the same command repairs,
resumes and updates.

## Zero discipline, by construction

| Temptation | Why it is refused | What happens instead |
|---|---|---|
| A flag on every `claude` run | it is a habit, and habits lapse | the settings file is installed once per project or machine |
| A daemon on developer laptops | it needs updates, permissions and trust | hooks are plain HTTP with a one-second timeout |
| A weekly "sync" step | it is invisible work | `cardo up` pulls and rebuilds only what changed |
| Manual `.env` editing | it invites drift and shared passwords | generated once, `0600`, never printed twice |
| Hand-maintained dashboards | they rot | provisioned from the repository |
| Asking engineers to label work | it is surveillance by another name | everything comes from Claude Code's own telemetry |

## CLI surface

| Command | Does |
|---|---|
| `cardo up` | preflight, secrets if missing, start, migrate, wait, open the board |
| `cardo up --pilot` | the same, with the pilot rows enabled |
| `cardo check [--wait 30s]` | the same checks as rows 01–07, non-interactive, exit code 0 or 1 |
| `cardo connect --once` | installs the settings file for this project or machine |
| `cardo connect --repair` | reinstalls and re-pings the twelve hooks |
| `cardo down [--keep-data]` | stops the stack; `--keep-data` is the default |
| `cardo connect --project <dir>` | installs `.claude/settings.json` in that project |
| `cardo secret [--dir]` | generates the salt and passwords into `.env` with mode 0600 |
| `cardo probe [--dir]` | sends one OTLP log and one hook event, then reads them back |
| `cardo session [--dir]` | checks whether a real session has reached the stack |
| `cardo notice` | writes the engineer notice from the current `.env` |
| `cardo version` | exists today |

`cardo check` is the contract: the board is a view over it, not a second implementation. Every row
maps to one check with a machine-readable result, so CI and a person see the same answer.

## The wizard

The board is the wizard, not a dashboard that happens to have buttons:

- the entry screen states the prerequisites as a list and offers one action that installs everything,
  plus a step-through path for a person who wants to watch each result;
- steps are ordered and numbered in a rail, with a segmented progress bar and "Step 3 of 8";
- steps ahead of the first unfinished one are disabled, so the order cannot be skipped by accident;
- the pane names the step, what it will run, and what the next step is;
- the primary action runs exactly that step and nothing else, so a person sees one result at a time;
- a step that needs one value asks for it in place: the settings step takes the project directory,
  and the step refuses to run without it rather than guessing a path;
- every step states its result, not only its state ("DONE v27.3.1", "FAILED ports in use: 8124");
- steps specified but not implemented stay visible, tagged "Later", and are excluded from "Step N of
  M" but named in the completion panel ("8 of 11 steps ran");
- when every implemented step has succeeded, the board adds an "Installation complete" panel with the
  numbers, the Grafana credential location and the commands that matter afterwards;
- "Install everything" exists for the second run and for CI, and stops at the first failure so the
  first cause is not buried;
- "Run checks" after completion is read-only: it re-runs docker, ClickHouse, Grafana and the collector,
  never secrets or the stack, so verifying cannot rewrite `.env` or restart containers.
- **Every failure explains itself.** A failed step carries two separate strings: the cause, taken
  from the observed output (the Docker socket error, the port number, the log line), and the
  recovery, naming the command or the next step. The board opens the failed step on its own and
  shows both above the description, so the person does not have to hunt for the reason. A step that
  cannot state a cause fails a test rather than shipping a dead end.

## The board in a browser

`cardo up --web` serves it after the run; `cardo ui` serves it on its own, so a stack that is
already up needs nothing but a browser.

- **Loopback only.** It binds `127.0.0.1:8484`. There is no flag to expose it.
- **A token, not a login.** The URL the CLI prints carries a one-time `t=` token. Every request that
  acts (`POST /api/run`, `GET /api/events`) is refused with 401 without it, because any page in the
  browser can otherwise reach loopback. Static files need no token and hold no secrets.
- **One implementation.** `POST /api/run` calls `launcher.RunStep`, the same function the CLI calls.
  `GET /api/state` reads the meta list that also feeds the terminal board.
- **One run at a time per step.** A second request for a running step gets 409 instead of starting a
  second `docker compose up`.
- **Live, not polled.** `GET /api/events` is server-sent events: each step reports `run`, then `ok`
  or `fail` with the value and the fix. Closing the tab stops the stream, never the run.
- **Planned rows are labelled.** A step that exists only in this document is served with the state
  `planned` and a disabled button, so the board cannot pretend it ran.
- **Secrets stay in the file.** No endpoint returns a generated password. The board shows only that
  `.env` was written, its mode, and the salt length.

## State

The wizard keeps only what it must:

- `.env` — the source of truth for secrets; never rewritten unless `--rotate`.
- A small state file (`.cardo/launcher.json`) — chosen path, completed checks with timestamps,
  launcher version. It is a cache: deleting it re-checks, nothing breaks.
- Everything else is read from Docker and ClickHouse on demand. The launcher stores no history.

## Acceptance

- On a clean laptop with Docker, `cardo up` reaches row 06 with a real session in under ten minutes,
  with no manual file editing.
- Running `cardo up` a second time changes nothing and says so.
- `cardo check` exits 1 with the failing row named, and 0 when all checks pass.
- Killing the collector mid-session leaves the engineer's Claude Code working (one-second hook
  timeout), and row 05 says why.
- The leaders' Grafana user is refused on `raw_*` and per-person tables, proven by a live attempt.
- Nothing in the launcher sends a byte outside the host. Verified by network capture in CI.

## Open questions

1. Is the launcher a local web page (this design) or a terminal UI? The board works in both; the
   web page makes the first row feel like a product, the TUI keeps one language for the whole tool.
2. Does `cardo up` open a browser automatically, or print the URL? Auto-open is friendlier for the
   solo path and hostile over SSH.
3. For the pilot, is the engineer notice generated from `.env` (this design) or written by the
   organization with placeholders?
4. Does the launcher own the `.env` file, or only generate a proposal the operator saves?
