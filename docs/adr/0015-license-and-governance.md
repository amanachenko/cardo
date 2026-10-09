# ADR-0015 — Apache-2.0, no CLA, private until an organization runs it

**Status:** Superseded by [ADR-0038](0038-public-under-a-collective-copyright.md)
**Date:** 2026-09-22
**Evidence:** Judgment, no evidence.

## Context

The project's future is genuinely uncertain: it may stay small, it may run at a handful of
organizations, or it may become a modest open-source project with outside contributors. The goal is
to keep options open without paying for options that will never be exercised.

Scenario analysis:

| Scenario | What actually matters |
|---|---|
| It stays small | Nothing. Ceremony is wasted. |
| A handful of organizations run it | Their legal teams wave the license through |
| Modest project, occasional external PRs | Contribution friction |
| It takes off | Governance legitimacy, longevity perception |
| The license has to change | Who owns the copyright? |
| A vendor builds a product on it | Can they? |

The last row is the one copyleft protects against, and it is not the real risk: a vendor with
distribution would reimplement the ideas, not fork the repo, and no license prevents that.

## Decision

- **Apache-2.0** in `LICENSE` from the first commit.
- **No CLA.**
- **Repo private initially**, with read access granted to the pilot organization's engineers.
  Publish publicly once an organization is actually running it.
- Personal GitHub account for now; move to a neutral org only if a community forms.

## Consequences

- Apache-2.0 is the license enterprise legal policies already whitelist, and it carries a patent grant.
- The trust premise requires that *the adopting organization's engineers* can read the code, not
  that the internet can. A private repo with read access satisfies the entire trust argument, so
  publishing can wait until the thing works — at no cost.
- **The constraint on future optionality is contributions, not the license.** As sole author we can
  relicense, dual-license or close our own code at any time. That freedom narrows the moment someone
  else's code lands. **The decision point for relicensing is the first external PR, not today** — so
  it can simply be deferred.
- Moving to a neutral org later is free; GitHub redirects the old paths.

## Rejected alternatives and why

- **MIT.** Shorter and matches `claude-code-otel` and ClawMetry, but has no patent grant — occasionally
  a snag in enterprise review, for no benefit.
- **AGPL.** Prevents a vendor wrapping it as SaaS, and is banned outright by a meaningful number of
  enterprise open-source policies. Fatal when the intended users are enterprises, in exchange for
  protection against a threat that is not real.
- **Open-core** (permissive core plus commercial modules). Needs a company behind it to sell the
  commercial part, and there is none.
- **Requiring a CLA.** Preserves the unilateral right to relicense. Signals a future relicense and
  suppresses exactly the drive-by contributions a project like this lives on.
