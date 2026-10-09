---
name: preview
description: Orient someone on a Cardo branch or pull request in plain words - where it sits in the roadmap, what changes, what to look at, what was checked, what needs them - and offer a guided demo on their own data with the preview stack, run only if they ask. Use when asked to preview, explain, demo or "show me" a branch or PR, or after building a work item.
argument-hint: "[pull request number or branch; default: this checkout]"
---

# Orient someone on a change

The person wants to know where things stand and what this change does, often while several other
sessions compete for their attention. Your job is to lower that load, not add to it. The summary
is the default. A demo on their own data is an offer, run only when they want more than the
summary.

## Rules

- **Plain words.** Say what a thing does ("the check that no dashboard shows one person's data"),
  not its code name. A test name, an ID, SQL or command output appears only if they ask for it.
- **Verification is yours.** Check the change before you summarize it, put the details in the pull
  request, and report the result in a line. Do not walk them through the checking.
- **Say a problem first,** in a sentence: something that does not do what it claims, or a mistake
  in the pull request. The change is not ready until it is fixed or they decide otherwise.
- **The live stack is only read,** and their data stays on their machine: never print a pseudonym,
  an email address, a salt or a password, and never put their rows in a pull request, an issue or
  a commit.

## 1. Read the change, yourself

- A pull request: `gh pr view <n> --json title,headRefName,body,files`. A branch, or nothing:
  `git log --oneline origin/main..HEAD` and `git diff --stat origin/main...HEAD`.
- Its item in [`docs/roadmap.md`](../../../docs/roadmap.md#work-items).
- If its claims have not been checked, check them now: the tests, a guard broken on purpose and put
  back, or the preview stack below. The summary reports what you found.

## 2. The summary

Five short parts, a few lines in all:

1. **Where it sits:** work item N, and what it unblocks.
2. **Before and after,** in plain words.
3. **One thing to look at** on their own data: the dashboard and panel, or the command, and what
   they should see. If there is nothing to see, say so and why ("it changes only tests").
4. **What was checked,** in a line or two.
5. **What needs them:** a decision, the merge, or nothing.

Then one line offering the demo: what it would show and roughly how long it takes. Stop.

For example:

> **Work item 1.** CI checks that no dashboard shows one person's data, but it looked only at a
> dashboard's top layer: a collapsed row or a dropdown could hide a per-person panel. Now it looks
> everywhere. **Nothing to see on your data:** it changes only tests. **Checked:** it catches a
> hidden panel, a dropdown and an annotation, and raises no false alarm. **Needs you:** the merge.
> I can show it catching a hidden panel, in about two minutes, if you want.

## 3. The demo, only if they ask

Guide them through it one step at a time, in plain words, and wait after each step.

**Choose by what the change touches:**

| It changes | The demo |
|---|---|
| views or dashboards (`sql/clickhouse/`, `dashboards/`, `deploy/compose/grafana/`) | the preview stack: the panels it changes, on their data |
| a `cardo` command that reads ClickHouse | the command, run against the preview |
| the collector or the settings bundle | the collector contract tests: the preview has no collector |
| tests or docs only | no stack: break the guard once, show the one-line failure, put it back |

**The preview stack** is a second Cardo built from the branch, over a read-only copy of their
rows. Run everything in bash (Git Bash on Windows) from the repository root, on the branch: if
this checkout is elsewhere and not clean, export the branch to a plain directory
(`git archive <ref> | tar -x -C <dir>`) rather than touching their work.

1. `bash scripts/preview.sh up` builds and starts it, in a few minutes. Tell them: the preview's
   Grafana is <http://127.0.0.1:3002>, signed in as `admin` with `GRAFANA_PASSWORD` from
   `deploy/compose/.env.preview` (say where it is, never print it); theirs stays on 3001, and both
   can be open at once.
2. `bash scripts/preview.sh compare`, then give them its conclusion in a sentence, not its table:
   - every table and dashboard view `same`: the copy is faithful;
   - a day named as "still open": a session that started then was still running at the copy, so
     their live stack keeps adding to that day. Leave it out of any comparison. The session
     running this demo is usually one;
   - a migration only the preview has: the change's own, or one on main their stack has not
     applied yet. Check `git log origin/main -- sql/` before putting it down to the change;
   - `DIFFERENT`, unexplained: from main or a change with no SQL, the copy is wrong; stop. From a
     change to the views, it should be only the views the change touches.
3. Show the one or two things the change is about: the panel on 3002 beside the same one on 3001,
   or the command's output, with the figure they should see. You cannot see Grafana: read the
   figure from the preview yourself first. A command runs from `deploy/compose` as
   `docker compose -p cardo-preview --env-file .env.preview -f docker-compose.yml -f docker-compose.preview.yml run --rm -T migrate <command>`.
4. `bash scripts/preview.sh verify-live` must say PASS. Report it in a line: their live stack was
   only read. A FAIL ends the demo, whatever else passed.
5. Ask whether to keep the preview for another look, or remove it with
   `bash scripts/preview.sh down`.
