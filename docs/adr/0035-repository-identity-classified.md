# ADR-0035 — Repository identity is collected, classified at the collector

**Status:** Accepted — **partly built**: collection and classification (decisions 1 and 2). No view
reads them yet, and decisions 3 and 4 are not built.
**Date:** 2026-09-25
**Edited:** 2026-10-09, in place, to word it for any organization. The decision is unchanged.
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (the `vcs.*` attributes and their flag,
**documented, not observed**); `2026-09-24-first-real-hook-payloads.md` (what `InstructionsLoaded`
sends). The classification is judgment.

Narrows [ADR-0002](0002-three-tier-data-model.md): repository identity, *classified*, moves from the
security tier into tier 1, and the full URL stays in the security tier. Extends
[ADR-0026](0026-stale-instructions-by-versioned-name.md) with an optional mode. Adds to the
mandatory tier (INV-4), which is why it is an ADR.

## Context

Four questions need to know which repository a session worked in:

- **Security:** is anyone working outside the organization's repositories?
- **Teams' own instructions:** a team's `CLAUDE.md` is always named `CLAUDE.md`, and Claude Code
  sends no content hash, so the repository is the only thing that identifies it.
- **Delivery outcomes**, which join by repository and week
  ([ADR-0031](0031-what-works-means.md)).
- **Autonomous agents**, whose cost belongs to a repository or a workflow rather than to a person
  ([ADR-0036](0036-service-runs-are-not-people.md)).

Claude Code can send `vcs.repository.url.full`, `vcs.owner.name` and `vcs.repository.name` when
`OTEL_METRICS_INCLUDE_REPOSITORY` is on (2.1.269+, off by default). The shipped bundle sets it
off, and the collector deletes `vcs.*`, because ADR-0002 put repository identity in the security
tier.

`InstructionsLoaded` gives a file's path, its scope (project, user or managed) and why it loaded.
There is no hash and no size. **Hook rows do not carry the repository.** They are matched to it
through the session's OTel records only after storage, so the collector cannot decide, when a hook
row arrives, "keep this name, because this is an organization repository".

## Decision

**1. The bundle turns on `OTEL_METRICS_INCLUDE_REPOSITORY`.**

**2. The collector classifies every repository before storing it.**

- A repository matching a new pattern, `CARDO_ORG_REPOS` (for example `^github\.com/acme/`), is
  kept by owner and name.
- Any other repository becomes `external`, plus its host (`github.com`, `gitlab.com`, ...), and
  nothing else.
- A session outside any git repository is `none`.
- **A blank pattern classifies everything as `external`.** It must not match everything, which is
  what a blank regex does in OTTL.
- The full URL is never stored in tier 1.

**3. The full external URL goes into the identified security stream later**, under the identity
decision for security exceptions ([ADR-0037](0037-policy-evidence.md)).

**4. Teams' own instructions.**

- **By default, presence only.** For each organization repository: do sessions load project-level
  instructions, and how many files. No names. That answers "which repositories have instructions"
  and, through ADR-0031's comparison, "do sessions with instructions do better".
- **An optional setting**, `CARDO_INSTRUCTIONS_NAMES=project`, keeps the names of project-level
  instructions files (for example `testing.md`). They are stored for every repository and shown only
  for organization repositories, under the five-person rule. Any other value means the default, so
  a typo fails closed.
- **The names of a person's own files** (user scope) are never kept, whatever the setting.
- The engineer notice states which way the organization set it.
- Separately, Anthropic is asked for the content hash its documentation promised. If it arrives,
  ADR-0026 already keeps it.

## Consequences

- **The attribute has not been observed.** Real sessions have to confirm three things before any view
  depends on it:
  - that it arrives at all;
  - on which records (metrics only, or logs too);
  - in what URL form (`https`, `ssh`, with or without `.git`). The pattern must match every form.
- The bundle test that requires repository identity to be off changes to require it on, and a
  collector test pins the classification: organization repositories kept, others reduced to host,
  a blank pattern making everything external.
- **External work is often legitimate:** open source, a personal project on a company machine, a
  contractor working in a customer's code. Whether it is allowed is the organization's policy, and
  Cardo reports it without judging it.
- **Edits to a team's instructions stay invisible.** Without a hash, an edited file looks like the
  original.
- Staleness of the organization's own files is still detected by versioned name (ADR-0026).

## Rejected alternatives and why

- **Keep it off.** The out-of-org question, teams' instructions, delivery outcomes and agent cost
  all stay unanswerable.
- **Full URLs for everything, in tier 1.** It names people's side projects and customers'
  repositories in data the platform team reads.
- **A salted hash of external URLs,** so the same external repository can be counted without being
  named. Anyone holding the salt reverses it by hashing candidate public URLs, and no question
  needed the count.
- **Instructions file names always kept.** Stores file names from outside repositories in order to
  show only those from inside, and one day a customer's repository has a rules file named after the
  customer.
- **Instructions file names never kept.** Some organizations will want them despite the risk, and it
  is their data and their choice. So it is a setting, off by default.
