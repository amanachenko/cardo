# ADR-0049 — Cost prices come from the public OpenRouter catalogue

**Status:** Accepted — **built**
**Date:** 2026-10-18
**Narrows:** ADR-0013 (no phone-home) on one outbound read; depends on ADR-0032 (the coach) and
ADR-0033 (cost for budget owners).

## Context

Imported transcripts name whatever models the person actually used — on the author's machine that
includes `mimo-v2.6-pro`, `z-ai/glm-5.3-flash`, `deepseek/deepseek-v4.1-flash` and
`meta/muse-spark-1.3-contributor`. Anthropic's API prices none of them, so a cost figure built only
from Anthropic prices would be silently wrong; a hardcoded table would drift within weeks and turn
"cost" into a number nobody can defend.

OpenRouter publishes its model catalogue, including per-token prompt, completion, cache-read and
cache-write prices, as a public JSON document that requires no key and no account. It is the same
price list its own invoices are derived from.

## Decision

1. **One host, one request per day.** `internal/pricing` issues a plain `GET
   https://openrouter.ai/api/v1/models`, caches the parsed result for 24 hours, and bills from the
   cache. No key, no query string, no cookies, no identifiers: the request carries nothing about the
   organization, its people, its transcripts or its token counts.
2. **It is the only non-Anthropic runtime host.** INV-7's allowlist names `https://openrouter.ai`
   with a reference to this ADR. Everything else the binary contacts stays pinned to the Analytics
   endpoint (ADR-0021) and the loopback stand-in (ADR-0047).
3. **Cost is labelled as list price.** Every surface that shows it — the coach, the console, the
   importer preview — reports OpenRouter list price, not an invoice. Discounts, credits and batch
   pricing are out of scope and must not be implied.
4. **Missing prices are reported, never zeroed.** The importer counts unpriced models and prints
   them; an unpriced request contributes no cost rather than a fabricated `$0.00`. An operator can
   edit the cached `pricing.json` by hand when a model is new or misnamed.
5. **Failure degrades, it does not block.** If the catalogue is unreachable, import and watch still
   run; they bill what the cache knows and report the rest as unpriced. There is no retry storm and
   no second endpoint to fall back to.

## Rejected alternatives

- **Hardcode a price table.** It drifts with every model launch, and a stale table is worse than an
  explicit "unpriced" because it looks authoritative.
- **Use Anthropic prices only.** Most of an imported corpus is third-party models; the resulting
  number would be off by more than an order of magnitude and would not reconcile with anything.
- **Ask the operator to paste prices.** That is recurring maintenance, which is exactly the
  discipline the product is supposed to remove (and the reason the watcher exists).
- **Read the provider's billing API.** Every provider needs its own key, scope and adapter, and the
  figure becomes invoice-dependent and non-comparable across people. The list price is the same for
  everyone, which is what a self-comparison needs.
- **Bundle the catalogue into the release.** Prices change faster than releases, and a bundled table
  would go stale between versions with no way to refresh it.

## Consequences

- Cardo now makes one outbound read that is not the Analytics API. This is a real change to the
  "nothing leaves the network" story, so it is documented in the README and in
  `docs/console-coach-history.md` rather than left implicit.
- An air-gapped install still works: the first import reports unpriced models, and the operator can
  seed `pricing.json` from any machine that can reach the catalogue.
- The catalogue is untrusted input: it is decoded strictly, only numeric prices and model ids are
  read, and a malformed document is ignored in favour of the previous cache. No code, templates or
  shell are executed from it.
- Cost remains an estimate of list price. The first invoice reconciliation is still the proof, and
  the surfaces say so.

## Evidence

`cardo import` on 2026-10-18: 415 files, 84 sessions, 2505 prompts, **$211.63 at OpenRouter prices,
0 models unpriced**. The same run listed seven model families, none of them Anthropic's.
