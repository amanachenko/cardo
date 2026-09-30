# ADR-0038 — Publish before the adoption pilot, under a collective copyright

**Status:** Accepted
**Date:** 2026-09-30
**Evidence:** Judgment, and a check of the published tree on 2026-09-30.

Supersedes [ADR-0015](0015-license-and-governance.md).

## Context

ADR-0015 chose Apache-2.0 with no CLA, and kept the repository private until an organization was
running Cardo: the trust argument needs that organization's engineers to be able to read the code,
not the internet. It also assumed a single author, free to relicense at will. That no longer fits:

- **Other people will contribute,** and some of them are likely to be the first to run it. No single
  name covers the whole.
- **An organization's legal team approves a public Apache-2.0 component routinely.** A private
  repository on an individual's account is an unusual case, and prompts the question of who owns it
  and on what terms.
- **The trust claim is stronger when anyone can check it,** not only the people granted access.
- **Relicensing is not needed.** ADR-0015 kept that option open until the first outside
  contribution; nothing depends on it.

## Decision

- **Apache-2.0, and no CLA,** unchanged.
- **Copyright "The Cardo Authors",** in a `NOTICE` file: the people the git history records as
  contributors. Each contribution belongs to whoever owns it, and the line claims nothing beyond
  that. Go and Kubernetes use the same convention.
- **Contributors sign off each commit** (`git commit -s`, the Developer Certificate of Origin).
- **The repository is published once it is prepared, and before the adoption pilot.** The internal
  trial can start while it is still private.
- **The public history starts at publication,** with one commit of the code and the design record as
  they stand. The ADRs and research notes carry the reasoning worth keeping.
- **Written for any organization.** The documents say "the organization" for whoever runs Cardo, and
  name no real one.
- **A personal GitHub account,** and a neutral organization only if a community forms, as before.
- **Nothing from a real organization is committed** (see Consequences).

Prepared means `NOTICE`, `SECURITY.md`, `CONTRIBUTING.md`, a pre-alpha note in the README, and every
document worded for any organization. The overview becomes `docs/overview.md`.

## Consequences

- **Anyone can check the trust claim.** An engineer can read what the bundle collects, and a
  security team can check the empty dependency list, without being granted access.
- **An adopting organization is asked for three things:** agreement to use an Apache-2.0
  component, which is routine; agreement that general improvements made on its time may come back to
  this repository, which is not routine and has to be asked; and nothing of its configuration or
  data enters the repository.
- **Pilot findings change form.** The design record commits dated research notes with real
  measurements. A pilot's note is committed only with everything that identifies the organization
  removed, and with the organization's approval; otherwise it lives in a private repository. A note
  from the internal trial reports results across the group, never one person's (INV-3).
- **The ADRs and research notes were edited once, before publication,** so that they use the same
  terms as the rest of the repository. Their decisions, reasons and findings are unchanged. From
  publication on, ADR-0000 applies without exception.
- **Publication cannot be undone.** Forks, clones and Go's module mirror keep copies. The tree was
  checked on 2026-09-30:
  - no secret or key, only `deploy/compose/.env.example` and a password used by CI's throwaway
    ClickHouse;
  - the one real-looking hash is a documented test vector;
  - no real organization that runs Cardo, or might, is named.
- **Relicensing needs every contributor** once others' commits land. Accepted.

## Rejected alternatives and why

- **Stay private until an organization runs it (ADR-0015).** Each adopter's engineers would need
  access to an individual's repository, its legal team would get an unusual case instead of a
  routine one, and only the people granted access could check the trust claim.
- **MIT.** Common among similar tools, but it has no patent grant, which enterprise legal teams
  prefer to have. ADR-0015's reason still holds.
- **Copyright in one person's name.** It goes stale with the first outside contribution, and claims
  more than the history shows.
- **A CLA.** Still rejected, as in ADR-0015: it suppresses contributions. The sign-off records each
  contributor's right to submit without a signing step.
- **Publishing the full development history.** Its commit messages are working notes, and the ADRs
  already carry the reasoning worth keeping.
- **Leaving the ADRs and research notes exactly as first written.** ADR-0000 says to, but they used
  working terms the rest of the repository no longer uses, and publication is the one moment they
  can be aligned without rewriting a public record.
