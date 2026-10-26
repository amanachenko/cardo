# Cardo UI — durable visual decisions

This file records the visual world for every screen after the launcher. It is not a style guide
written in advance: it describes what the first surface committed to, so the next one inherits it
instead of inventing a second identity.

Read it together with:

- [`cardo-ui.css`](cardo-ui.css) — the only stylesheet. Tokens and components live there.
- [`components.html`](components.html) — every component and state, rendered from that stylesheet.
- [`prototype/quickstart.html`](prototype/quickstart.html) — the launcher comp that produced it.

## What this is, and why

Cardo's job is to tell an organization whether something is working, right now, without watching
anyone. The surface is an **operate** screen: a person arrives to find out the state of a system and
do the one thing that unblocks it.

The world is a **departure board in a station**: fixed character cells, rows that are entities,
solid signal lamps, one announcement line, and a board that never decorates. It is the one object in
the audience's world that already means "current state of many moving things, readable in one
glance, by a person in a hurry".

Two consequences, both deliberate:

- **There is no card grid, no hero, no icon set.** A row is the unit. Rows stack, group under a
  heading, and open in place.
- **There is no light theme.** The board is opened next to a terminal, often at night. Light is a
  choice about the use scene, not about the category.

Impeccable's mode is `operate`; the build path is code-led; the composition was chosen from the
dealt surface structures (seed `a1c07896`) and locked by the project owner.

## Tokens

Values are in `cardo-ui.css`. Changing one here means changing it there.

### Colour: four roles, three signals

| Token | Value | Role |
|---|---|---|
| `--void` | `#0b0c0a` | page ground |
| `--flap` | `#141513` | board surface |
| `--steel` | `#33352f` | structure, hairlines |
| `--paint` | `#ece5d3` | verified state, primary ink |
| `--paint-dim` | `#a9a494` | secondary text |
| `--paint-faint` | `#85877a` | micro labels only |
| `--amber` | `#d7a12a` | in progress, needs a look |
| `--red` | `#a8453c` | failed, refused, stopped |
| `--ok` | `#86b47a` | verified: a check, a session, a proof that passed |

Rules:

- A signal colour means one thing. Amber never decorates; red never marks a person.
- Success is `--ok` (green), and only for a verified state: a passing check, a read-back, an approved
  is the board's normal state, not an achievement.
- Contrast on `--flap`: paint 14.6:1, paint-dim 7.4:1, paint-faint 5.0:1, amber 7.9:1. Red is
  graphical only (3.1:1) and never carries text.

### Mark: two squares

A filled amber square (done) and an outlined paper square (pending), offset diagonally on a 24 × 24
grid. It is the rail's own language, not a metaphor: the installer always has a finished state and a
next one. Sizes: 17 px in the gantry, 24 px stand-alone, 56 px on covers. Below 20 px the *tiny* variant is used
(both squares filled), because an outline next to a fill closes up at that size; the favicon is the
same tiny variant. Clear space is one square's side on all sides. One
colour is allowed — amber — plus paper. No gradients, no rounded corners, no letterforms.

### Type: six sizes, one step of roughly 1.2

| Token | Size | Use |
|---|---|---|
| `--t-board` | `clamp(22px, 3.2vw, 34px)` | one per screen, uppercase mono |
| `--t-title` | `19px` | step names, section titles |
| `--t-body` | `16px` | explanations, sans-serif |
| `--t-small` | `13px` | values, tables, commands |
| `--t-label` | `12px` | button labels, counts |
| `--t-micro` | `11px` | column headers, status lines |

Mono is used for data, row names, commands and labels: the board's cells are fixed-width, so
monospace is the material, not a costume. Sans is used only for prose. Tabular numerals are on
every number.


### Density: one 4 px grid and one measure

| Where | Value |
|---|---|
| Rail row | 10 px vertical padding, 37 px tall, 1 px hairline |
| Pane | 20/24 px padding, 12 px below the head, 18 px between sections |
| Facts | 7 px cell padding, tabular numerals, 34 % label column |
| Command | 40 px high, 13 px mono, scrolls horizontally instead of clipping |
| Actions | 16 px above, 10 px gap, 40 px buttons |

Density is read as calm, not cramped: the rail is tight because it is scanned, the pane is airier
because it is read. Nothing is smaller than 11 px.

### Motion: state, not decoration

**Focal moment:** the segment bar turning green when a step passes. It is the only place where the
install's progress is felt, and it is a colour transition (220 ms), not a moving element.

**Feedback:** the amber dot pulses (1.5 s) only while a step runs — the one signal that says "work is
happening"; the check mark pops in (180 ms) when it passes.

**Continuity:** the pane enters with 160 ms opacity plus a 3 px rise, and only when the current step
changes — a state tick does not replay it.

**Budget:** transform and opacity only; no loops that outlive a run; no motion on data.

**Reduced motion:** `prefers-reduced-motion` removes every animation and transition; colour and state
changes remain, so feedback is still legible.

### Accent

One accent, three signals, and a rule for each:

| Signal | Use | Never |
|---|---|---|
| `--amber` | in progress, needs a look, the next step | decoration, success, headings |
| `--danger` / `--danger-line` | a failed step, a refusal, a blocked action (text 6.6:1, border 3.1:1) | a person, a warning that is not a failure |
| `--ok` | verified: a passed check, a finished step, a completed install (7.7:1) | decoration, "brand" green, a step that has not run |
| `--paint` (no hue) | the single primary action, and body text | state signalling |

The primary action is the only filled button on a screen (`--paint` fill, ink text). Everything else
is outlined or ghost. That is why the accent stays free for state: if the call to action were amber,
amber would stop meaning "in progress".

### Metrics

- Base spacing `4px`; horizontal padding `16px`; column gap `14px`.
- Row height `46px` on desktop, auto on mobile.
- Borders are always `1px`. There are no radii, no shadows, no gradients, no blur.

## Components

Every one is in `cardo-ui.css` and shown in `components.html`.

| Component | Class | States |
|---|---|---|
| Board | `.board`, `.bhead`, `.grouphead` | — |
| Row | `.row` | `data-state` = `wait` `run` `ok` `fail`, plus `aria-expanded` |
| Lamp | `.lamp` | `data-s` = `wait` `run` `ok` `fail` |
| Drawer | `.rowwrap.open .drawer` | open, closed |
| Command | `.cmd` + `.copy` | default, copied |
| Facts | `.kv` | `on`, `off` |
| Log | `.log` | — |
| Result table | `.arrival` | — |
| Buttons | `.act`, `.act.ghost`, `[disabled]` | hover, disabled |
| Ticker | `.ticker` | default, `.ok`, `.fail` |
| Bottom bar | `.bar`, `kbd`, `.progress` | — |
| Departure row | `.departures`, `.dep` | hover, focus |
| Empty | `.state-empty` | — |
| Error | `.state-error`, `.wiz-error` | cause, recovery |
| Wizard | `.wiz`, `.wiz-rail`, `.wiz-pane` | rail, pane |
| Step result | `.wiz-result` | waiting, running, done, failed, later |
| Step subtitle | `.wiz-sub` | — |
| Rail links | `.rail-link`, `.rail-toggle` | rest, hover |
| Entry field | `.start-project` | — |
| Step list | `.wiz-step` | `data-state`, `aria-current`, locked |
| Progress | `.segbar`, `.wiz-progress` | ok, run, fail |
| Requirements | `.reqlist` | — |

Invariants:

1. Four row states, no fifth. A new state is a design decision, not a CSS addition.
2. The drawer is the only way to show detail. No modals, no popovers, no side panels.
3. The value column is right-aligned and tabular. Numbers never move when they change.
4. The command is always shown in full before it runs.
5. Every row is reachable by keyboard: `↑` `↓` move, `Enter` opens, `Esc` closes, `C` copies.
6. A lamp is a 6px solid square with a word beside it. No glow, no pulse, no animation loop.

## Wizard composition

The installer is the first multi-screen flow, and it fixes three rules for every later one:

1. **Position is always visible.** A segmented bar shows one cell per step, the rail lists the steps
   in order, and the header states "Step 3 of 8". A person never has to work out where they are.
2. **One primary action per screen.** The button names the step it will run ("Check and continue",
   "Generate secrets and continue"). A second action appears only after success ("Run again").
   "Install everything" exists once, on the entry screen and in the rail foot.
3. **The rail is the map, the pane is the work.** Steps ahead of the first unfinished one are
   disabled with a reason, so the order cannot be skipped by accident, and the pane always answers
   three questions in the same order: what this step does, what it will run, what happens next.

A step always states its result, not only its state: the pane opens with a lamp and the value the
engine returned ("DONE v27.3.1", "FAILED ports in use: 8124 8089"). A tick without a value is not
shown anywhere.

Steps that exist only in the specification remain visible, tagged "Later", and do not count towards
"Step N of M". The completion panel reports both numbers: "8 of 11 steps ran", with the names of the
three that were not.

Failure occupies the pane, not a toast: the cause is a sentence taken from the output, the recovery
is a separate labelled line, and the primary button becomes "Retry". The failed step is selected in
the rail, so the wizard does not move on while something is broken.

## Icons

There is no icon set beyond five authored marks, drawn as inline SVG at one stroke weight (1.6 at
13px, 2 at 10px):

| Icon | Where | Why |
|---|---|---|
| check | a completed step in the rail, the completion panel | scanning a long step list |
| alert | the failure block, a failed step | the only place a symbol may replace words |
| copy | the command block | the action is repeated often |
| external | a link that leaves the board | the only link of its kind |
| arrow | not used yet; reserved for "continue" | — |

Rules: icons never replace a label on their own; no icon is decorative; no emoji, no unicode glyphs,
no icon fonts; a new icon is added to the sprite and to this table in the same change.

## Element inventory

Everything a screen may use, and the states each must support:

| Element | Class | States |
|---|---|---|
| Primary action | `.act.primary` | rest, hover, disabled |
| Secondary action | `.act`, `.act.ghost` | rest, hover, disabled |
| Text input | `.input` | rest, focus, invalid |
| Command | `.cmd` + `.copy` | rest, copied |
| Facts | `.wiz-facts`, `.kv` | — |
| Failure | `.wiz-error` | cause, recovery |
| Step rail | `.wiz-step` | wait, run, ok, fail, locked, current |
| Progress | `.segbar`, `.wiz-progress` | ok, run, fail |
| Requirements | `.reqlist` | — |
| Completion | `.pane-done` | — |
| Departure row | `.dep` | default, selected |
| Result lamp | `.lamp` | wait, run, ok, fail |
| Credentials (once) | `.wiz-section` + `.wiz-facts` | — |

Not in the interface on purpose: tooltips-only information, modals, toasts, spinners without a
label, and any element whose only state is "looks nice".

## Motion

One authored moment: when a row settles, its value and lamp take a two-step flap
(`flapIn`, 150ms, `steps(2, end)`). Nothing else animates. No easing curves, no parallax, no
entrance animations on page load.

`prefers-reduced-motion: reduce` removes it entirely.

## Layout

- One column, max `1240px`, content never centred as a hero.
- The board header is sticky; the gantry scrolls away so the screen belongs to the rows.
- Group headings are hairlines, not containers.
- On mobile the value moves under the name, the lamp keeps its column, and the command block wraps
  with the copy button below it.

## Voice and language

- **English, formal register.** Interface copy is written in English for the repository and for
  every deployment. No contractions, no colloquialisms, no exclamation marks, no emoji. "Verifies
  that the daemon responds", not "make sure Docker is up".
- Plain words, one example per claim, no adjectives. The project's own docs set this tone.
- A control names the action: "Проверить цепь", not "Продолжить".
- An error names the cause and the recovery, in that order, and says what will not be touched.
- An empty state says what to do next, never "данных нет".
- No emoji, no exclamation marks, no "успешно!" — the lamp already said it.

## Never in this UI

- A ranking of people or teams, a leaderboard, a per-person comparison.
- "Hours saved", ROI, or a per-head metric.
- A number without its source visible next to it.
- A chart that only looks good: if a view cannot change a decision, it does not ship.

## Reuse

1. Link `cardo-ui.css`. Do not copy CSS into a page.
2. Compose from the table above. If a screen needs something new, add the component to the
   stylesheet and to this table in the same change.
3. Tokens before components: no raw hex, no arbitrary `px` font sizes, no new radius.
4. Name states in `data-*` attributes, not in class soup.
5. New screens are checked against the contrast pairs above and against
   `prefers-reduced-motion`.

## Known gaps

- The launcher comp predates the shared stylesheet; it is self-contained on purpose (one file to
  open and send). When the launcher becomes real code, it imports `cardo-ui.css` and the comp is
  archived.
- Grafana dashboards are provisioned JSON and do not inherit these tokens. Their palette is a
  separate decision, recorded where the dashboards live.
- No print stylesheet yet. The engineer notice will need one.
