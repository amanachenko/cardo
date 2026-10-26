<!-- impeccable:product-schema 1 -->
# Product

## Platform

web — served locally by the `cardo` binary at `127.0.0.1`, never hosted. A terminal CLI ships the same
operations (`cardo up`, `check`, `poll`, `gold`, `connect`).

## Users

- **The operator** — a platform or security engineer standing Cardo up for one organization. Works on
  one host, reads a terminal, knows Docker, and refuses a UI that hides the command it runs. Their job:
  install and prove the stack, load the things a pilot needs (teams, artifacts, settings), and be able
  to say afterwards exactly what data left the network.
- **The engineering leader** — reads gold-only dashboards. Never uses this console: their login cannot
  reach bronze or silver (ADR-0029, roadmap item 7).
- **The engineer** — receives one settings file and, later, a personal page showing their own figures.
  Never appears in the console as a person.
  
## Product purpose

Cardo is self-hosted observability for Claude Code. It shows which ways of working with AI an
organization's teams invented and which are worth making standard, whether a rollout reached everyone,
and whether the organization's rules hold — without watching any individual. Its rule for choosing
work: build what gets an organization to its first real decision soonest.

Two ingestion paths feed one ClickHouse and share one pseudonym contract: the Admin API poller
(organization-wide daily cost) and the collector (session-grain, artifacts, repositories). Gold views
answer leadership questions; the raw layers exist only for the operator.

## The surface being designed

An **operator console**: the local board that today only installs the stack. It grows into the place
where a pilot is prepared and governed — the team directory, the enablement catalog (skills, commands,
subagents, MCP servers, reference docs, settings bundles, pilot cohorts), the settings that shape the
views, and the health of both ingestion paths. Installation stays as its first screen.

## Product truth that constrains design

- **No person is a row.** No screen lists, ranks or opens an individual. Team files are hashed on load;
  the email never lands in storage. Any cohort below five people is suppressed, not shown small.
- **The operator holds the salt.** The UI may say where it is, never show it.
- **Nothing requires discipline.** Settings are made once per machine; no recurring ritual, no cron on a
  laptop, no dashboard to maintain. Every action is idempotent and re-runnable.
- **Every claim is evidence.** A step that says "installed" must have read something back; a catalog row
  that says "reach" must come from gold, not from the operator's memory.
- **The surface stays quiet.** Mono data, one amber signal for "in progress", green for verified, red for
  failed, paper for actions. No cards, shadows or rounded corners.

## Non-goals

- A general file manager, a wiki, or a place to upload arbitrary content.
- A people directory or an HR view; a per-person cost or performance view (ADR-0033 stays unbuilt).
- Editing the canonical baseline or the ADRs from the UI.
- Multi-tenant hosting, SSO, or remote access beyond the operator's host.

## Evidence

`docs/overview.md`, `docs/roadmap.md`, `docs/design/DESIGN.md`, ADR-0024, ADR-0029, ADR-0034, ADR-0040,
ADR-0043, and `docs/design/quickstart-launcher.md`.
