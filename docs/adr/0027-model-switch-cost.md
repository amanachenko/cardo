# ADR-0027 — The cost of a model switch joins the mandatory tier

**Status:** Superseded by [ADR-0039](0039-hooks-wait-at-most-one-second.md)
**Date:** 2026-09-24
**Evidence:** `2026-09-24-first-real-hook-payloads.md`, section 2. The field names were observed
from Claude Code 2.1.281; their types and values were not, since none of them were kept.

## Context

`PreModelSwitch` in Claude Code 2.1.281 carries more than the documented `from_model` and
`to_model`. It also carries `requested_model`, `source`, `pricing`, `context_tokens`,
`prompt_cache_warm`, `cache_ttl` and `estimated_cache_write_usd`.

Switching models mid-session discards the prompt cache. The next request then writes the whole
context again, at the new model's cache-write price. Claude Code estimates that cost itself, at the
moment of the switch.

So one efficiency question is squarely in scope ([ADR-0003](0003-efficiency-before-security.md)):
how often do switches throw away a warm cache, and what do they cost? The answer bears on a
platform-team artifact too, the guidance on which model to use when. Keeping these fields adds to
the mandatory tier, which needs this record (INV-4).

## Decision

The collector keeps the following from `PreModelSwitch`, **each only when its value has the type
the name means**:

| Stored | From | Kept only if |
|---|---|---|
| `requested_model` | `requested_model` | a string of model-name characters, at most 80 |
| `model_switch_source` | `source`, on `PreModelSwitch` only | enum-shaped ([ADR-0025](0025-artifact-names-kept-with-guardrails.md) decision 4) |
| `context_tokens` | `context_tokens` | a number |
| `prompt_cache_warm` | `prompt_cache_warm` | a boolean |
| `cache_ttl` | `cache_ttl` | a number, or a short token such as `5m` |
| `estimated_cache_write_usd` | `estimated_cache_write_usd` | a number |

`pricing` is not kept. It is a table of prices, derivable from the model, not a fact about the
event.

## Consequences

- Silver can report cache-discarding switches and their estimated cost by cohort.
- **The type guards are written from what the names mean, not from observed values.** If a real
  type differs, that field is dropped rather than stored. It fails closed, and the next observation
  shows it missing from the row while `cardo.received_keys` still lists it.
- Six fields are added to the mandatory tier.

## Rejected alternatives and why

- **Keep only `from_model` and `to_model`, as before.** That records that a switch happened and
  misses what it cost, which is the part a platform team can act on.
- **Keep `pricing`.** It is a price table with no event information, and a map is a shape the
  allowlist should not have to trust.
- **Compute the cost in silver from token counts.** Claude Code's estimate uses the cache TTL and
  whether the cache was warm at that moment. Nothing else Cardo receives carries either.
- **Keep the fields by name without type guards.** A field with a numeric name is only a promise
  about its value. For a mandatory tier, text arriving under that name must be dropped, not stored.
