# ADR-0043 — The Admin API poller runs as a service in the reference stack

**Status:** Accepted
**Date:** 2026-10-18
**Narrows:** [ADR-0013](0013-deployment-topology.md) on where the poller runs, and [ADR-0028](0028-file-store-is-admin-api-only.md) on the stack's evaluation mode (the file store is unchanged for the CLI).

## Context

The Admin API path and the collector path write into one ClickHouse and share one pseudonym contract
(ADR-0006, ADR-0034). The collector has always run inside the reference stack. The poller did not: it
was a CLI an operator had to run on a schedule from their own machine.

That is a discipline requirement, and this project treats those as defects. A schedule on someone's
laptop means: it stops when they close the lid, it lives in one person's memory, and a new operator
has to be told about it. The alternative — a cron entry in the repository — is worse, because it
either needs a host or a second orchestration story.

## Decision

`cardo poll` gains `-every <duration>`: it polls the last two days (daily aggregates arrive late),
then sleeps, recomputing the window on each pass. The reference stack gains a `poller` service that
runs that command with `restart: unless-stopped`, gated behind the Compose profile `admin`.

The profile is the whole safety mechanism:

- **No key, no profile.** `cardo up` and the board add `--profile admin` only when `CARDO_ADMIN_KEY`
  is non-empty in `deploy/compose/.env`. A stack without an organization key never carries a container
  that cannot work, and `docker compose up --wait` is not asked to wait for an exited service.
- **A key, and the poller joins the installation.** It starts with the stack, shares the `cardo:dev`
  image and the ClickHouse credentials, and needs no host cron.
- **The CLI stays.** `cardo poll -store clickhouse -days 30` remains the way to backfill by hand, and
  `-store file` keeps the no-infrastructure evaluation mode of ADR-0028.

The board configures it in one place: a card on the start screen shows whether a key is configured,
the last day that landed, rows and days, and one interval field (default `24h`). The key is written to
`.env` with mode 0600 (ACL on Windows), is write-only in the UI, and is never returned by
`GET /api/admin`. Saving starts the poller; `Run now` restarts it, which triggers an immediate poll.

## Consequences

- A pilot now needs one configuration act per organization, not a scheduled job per operator.
- The Admin API path becomes visible in the installation surface, next to the collector path, which
  is where the two-tube model belongs. The start screen says which path is configured and that no
  laptop is touched by the admin path.
- The reference stack grows a fifth service in the `admin` profile. `docker compose ps` without the
  profile shows the four services a collector-only install runs.
- The key sits in the same `.env` as the salt and the two passwords. It is the only secret in that
  file that the installer does not generate, and the only one a person types.
- Backfill depth remains a CLI concern: the service polls a two-day window and never rewrites history.
  A first installation over an existing organization runs `cardo poll -days 30` once, by hand.

## Rejected alternatives

- **A host cron entry shipped in the repository.** Needs a host, differs per platform, and turns an
  installation into an operations task.
- **Poll on every `cardo up`.** The board and `up` are repair commands; they should not make network
  calls to Anthropic as a side effect, and a stack that has been running for a month needs no new
  backfill.
- **A separate `poller` binary or image.** The stack already builds `cardo:dev` for migrations; a
  second artifact would drift from the CLI that operators use by hand.
- **Making the poller unconditional and exiting when the key is missing.** `compose up --wait` treats
  an exited container as a failed stack, so a collector-only installation would report a failure for
  a feature it never asked for.
