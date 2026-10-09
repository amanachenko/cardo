---
name: preview
description: Walk someone through what a Cardo branch or pull request changes, on their own data, before it merges. Brings up the preview stack (a second Cardo built from the branch, over a read-only copy of the live stack's rows), checks the copy, then goes through each claim the change makes one step at a time, and proves the live stack was only read. Use when asked to preview, validate, demo or "show me" a branch or PR, or after building a work item and before opening its pull request.
argument-hint: "[pull request number or branch; default: this checkout]"
---

# Preview a change on real data

The goal is that the person sees the change work, on their own sessions, and understands what it
does and why, before anyone merges it. A green CI run is not that. You are a guide: one step at a
time, plain words, and stop for them after each step.

`scripts/preview.sh` does the mechanical work, and this walkthrough uses it. It runs in bash (Git
Bash on Windows). The live stack is the compose project that collects (`cardo` unless
`CARDO_LIVE_PROJECT` says otherwise).

## Rules for the whole walk

- **The live stack is only read.** Never run a write, a migration or `docker compose` against it.
  Everything goes through `scripts/preview.sh`, which reads it with `readonly=1`, and step 6
  proves that from the live server's own query log.
- **Their data stays on their machine.** Show counts, Claude Code's own names and the shape of a
  result. Never print a pseudonym, an email address, a salt or a password, and never put their rows
  into a pull request, an issue or a commit. Grafana shows pseudonyms; that is fine on their screen
  and nowhere else.
- **One step, then stop.** Say what you ran, what it showed, and what it means, in a few lines.
  Ask them to look where it applies, and wait for them before the next step.
- **You cannot see Grafana.** Name the dashboard and panel, give both URLs, and state the numbers
  you expect, read from the preview yourself (a query to its ClickHouse, or `/api/ds/query`
  through its Grafana). Let them say whether the screen agrees.
- **A mismatch stops the walk.** If something does not do what the change claims, say so plainly
  and do not explain it away. It goes in the summary as found, and the change is not ready.

## 1. Orient

Find what is being previewed:

- **A pull request number:** `gh pr view <n> --json title,headRefName,body,files`.
- **A branch, or nothing:** this checkout, `git log --oneline origin/main..HEAD` and
  `git diff --stat origin/main...HEAD`.

The preview builds the checkout that runs it, so the branch must be checked out. If this checkout
is not on it and is not clean, use a separate worktree rather than touching their work:
`git fetch origin <branch>` and `git worktree add --detach <dir> FETCH_HEAD`, then run everything
from `<dir>`.

Then tell them, in four or five lines:

- what the change claims to do, in their terms rather than the diff's;
- which item in [`docs/roadmap.md`](../../../docs/roadmap.md#work-items) it serves, and what
  question of the design it answers;
- what you will show them, and roughly how long it takes.

Stop. Go on when they say so.

## 2. Choose what to show

By what the change touches:

| It changes | Show it with |
|---|---|
| `sql/clickhouse/`, `dashboards/`, `deploy/compose/grafana/` | the preview stack: the views and panels, live against preview |
| a `cardo` command that reads ClickHouse | the command run against the preview (step 5) |
| `deploy/collector/`, `deploy/managed-settings/` | the collector contract tests (`make collector-test`, or CI's collector job). The preview has no collector |
| tests or docs only | the tests, and the guard broken on purpose: make the change it forbids, show it failing, put it back, show it passing. No stack |

Say which applies and why. A change can be in more than one row. If none needs the stack, skip to
step 5.

## 3. Bring it up

```bash
bash scripts/preview.sh up        # or refresh, if a preview from this checkout is running
```

The first run builds `cardo` and starts the preview, in a few minutes. It prints the rows it
copied from each table, up to a cutoff five minutes before now, and the view settings it took from
the live stack. Report those lines, and both Grafana URLs: the preview on 3002, the live one on
3001. The preview's password is `GRAFANA_PASSWORD` in `deploy/compose/.env.preview`. Tell them
where it is, and do not print it.

## 4. Check the copy

```bash
bash scripts/preview.sh compare
```

- **Rows:** every table `same`. A difference means rows arrived late or the copy is wrong: run
  `refresh` and compare again before going on.
- **Migrations, preview only:** what this change adds to the schema. It should match the diff.
- **Views added or removed:** the same.

From main, or a change with no SQL and no dashboard, the two Grafanas must show the same figures
over a time range that ends before the cutoff. That is the check that the preview is faithful.
Ask them to open one panel in each and compare.

## 5. Walk each claim

Take the claims from the pull request's **How to see it** section, or from the work item's brief
if there is no pull request yet. For each claim, one at a time:

1. Say what it claims, in one sentence.
2. Show it:
   - **a panel:** the dashboard and panel name on 3002, the figure to expect, and the same panel
     on 3001 to compare;
   - **a view:** query it in the preview, with a count or a few rows of shape, no pseudonyms:
     `docker exec cardo-preview-clickhouse-1 sh -c 'clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --query "..."'`;
   - **a command:** run the preview's build of it against the preview, from `deploy/compose`:
     `docker compose -p cardo-preview --env-file .env.preview -f docker-compose.yml -f docker-compose.preview.yml run --rm -T migrate <command>`;
   - **a guard:** break it as in step 2, and show the failure message.
3. Explain what they are looking at, and how it follows from the design: the ADR or invariant it
   rests on.
4. Ask whether it matches what they see, and whether it is what they expected. Wait.

## 6. Prove the live stack was only read

```bash
bash scripts/preview.sh verify-live
```

It waits for the live server's query log, which is written once a minute, then lists every query
the preview has sent it in the last three days. It must say PASS: all SELECT, all `readonly=1`. A
FAIL ends the walk, whatever else passed.

## 7. Close

- **A short table:** each claim, what was shown, and whether it held.
- **Anything found:** a mismatch, a surprise, a question they raised. Say what you would do about
  it, and do not do it yet.
- **Next:** if there is no pull request yet, offer to open one, with a **How to see it** section
  listing the steps you just ran, so the next person can repeat them. If there is one, it is theirs
  to merge.
- **The preview:** ask whether to keep it for a second look or remove it with
  `bash scripts/preview.sh down`. Removing a worktree made in step 1 is `git worktree remove <dir>`.
