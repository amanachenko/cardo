# Product

<!-- impeccable:product-schema 1 -->

## Platform

web — served locally by the `cardo` binary at `127.0.0.1`, never hosted.

## Users

Two confirmed entry paths, chosen on the first screen:

- **A solo engineer** evaluating Cardo on their own laptop. No approval, no platform team, about
  ten minutes. Their job: get their own Claude Code session into a working stack and see it arrive.
- **A platform or security lead** running a first team pilot. Their job: stand the reference stack
  up, keep personal rows away from the leaders' dashboards, hand engineers one settings file, and
  be able to say afterwards what data left the network.

Both are technical: they read a terminal, know Docker, and will not accept a wizard that hides the
command it runs.

## Product Purpose

Cardo is self-hosted observability for Claude Code. It shows which of the ways an organization's
teams use AI are worth making standard, whether a rollout reached everyone, and whether the
organization's rules hold — without watching any individual.

This surface is the quickstart launcher. It exists to close roadmap Step 1, "run it safely": one
command, a working stack, verified data, and a first real decision available at the end. Success is
a person seeing a row from their own session in the pipeline and knowing exactly what was sent.

## Positioning

Measures tools, not people. Runs inside the organization's network with no hosted version. Refuses
rankings, "hours saved", per-head metrics and any per-person view. What it claims, it shows with an
example from the organization's own usage rather than a benchmark.

## Operating Context

- The stack is Docker Compose: ClickHouse (HTTP `8124`, native `9001`), Grafana (`3001`), collector
  (OTLP `4319`, hooks `8089`), and a one-shot `migrate` service.
- Credentials and the pseudonymization salt live in `deploy/compose/.env`, hand-edited today. The
  salt must be hex, at least 32 characters; the collector refuses every OTLP batch without it.
- Claude Code is pointed at the stack with a single `--settings` file; the organization variant is
  deployed through MDM. Twelve hooks, each with a one-second timeout.
- The launcher runs where the stack runs: a laptop, or a small server on the organization's network.

## Capabilities and Constraints

- Confirmed: generate the salt and both passwords, write `.env` with `0600`, start the stack, wait
  for `migrate`, verify ingestion with a synthetic session and a hook ping, produce the settings
  file, produce the engineer notice, and hand over Grafana credentials.
- `cardo check` does not exist yet in the CLI (`poll`, `migrate`, `version` only); this launcher
  defines what it should verify.
- Hard rules that the launcher must not break: no telemetry of its own, nothing leaves the machine,
  no per-person view, minimum group size five, no "hours saved", no rankings.
- Preflight facts to handle: Docker daemon absent, occupied ports, an existing `.env` (resume, do
  not overwrite secrets), a stack already running, and a salt that is present but invalid.
- Platform: macOS, Linux, and Windows through Git Bash. The launcher itself must not require bash.

## Brand Commitments

The name is Cardo — the north–south axis of a Roman town. The project's own documents set the voice:
plain words, one example for every claim, no marketing adjectives, no emoji. The repository is
text-first; there is no existing visual identity for a UI to inherit.

## Evidence on Hand

`docs/vision.md` (North Star, Never list, ordering rule), `docs/adr/0042-the-north-star.md`,
`docs/roadmap.md` Step 1, `deploy/compose/README.md` and `.env.example`, `deploy/managed-settings/`
(two settings files), `docs/design/privacy.md`, `docs/design/invariants.md`.

## Open Decisions

- Whether the launcher is a local web UI inside the binary or a terminal UI — this prototype argues
  for the local web UI; the final choice is the project owner's.
- Whether teams get a page of their own (open question in `docs/vision.md`); out of scope here.
