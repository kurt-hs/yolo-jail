---
title: "Readable Activation Advisory Evidence — 2026-10-27"
date: 2026-10-27
---

# Readable activation advisory evidence

**Record status:** historical advice and process evidence only. Nothing here is an owner ruling, accepted design, executable plan, current-runtime acceptance, native certificate, or CB implementation. No behavior or policy is authorized by these records.

## Frozen question and opinion records

- [Frozen v2 packet](./packet.txt) is the exact shared question/evidence packet supplied to both initial advisors.
- [Astra initial opinion](./astra-initial.txt) and [Fable initial opinion](./fable-initial.txt) preserve the complete independent final advice bodies byte-for-byte.
- [Astra merged follow-up](./astra-merged.txt) preserves the complete later synthesis byte-for-byte. It is a follow-up critique, not a third blind initial opinion.

The opinions are retained as supplied; the `.txt` files avoid converting their original Markdown question directives or relative links into newly interpreted Vantage cards.

## Provenance and effective capabilities

The frozen question originated from private source `d8ed0c9dd820886d249c66d662474673cd2b2c7a`, tree `72e5f49583d488b2929c7f79a682f5cd30af888d`, collected at `0aa`. Although the Mac source may have advanced, the advice is not acceptance of the current runtime.

One workflow (`b21f2400-91dc-4b96-863d-b36ae07c2901`) ran both initial opinions blind with the identical frozen 8,301-byte packet included exactly once; each actual task prompt was 8,557 bytes with the known runtime wrapper. Astra run `1f4405e8` resolved to `amazon-bedrock / global.openai.gpt-6-astra`, HIGH, message `24e1ee76`. Fable run `d111e8f5` resolved to `amazon-bedrock / global.anthropic.claude-fable-5-1`, HIGH, message `5025218c`. The authorized explicit merge follow-up was Astra run `b9e9e6a0`, HIGH, message `cfbff708`, supplied the original packet, both complete opinions, and a design-only comparison; it was not blind.

Native audits report all three actual effective direct-tool lists as only `subagent`, `contact_supervisor`, and `subagent_supervisor`; allowed helper names were scout, researcher, and evidence-auditor. No direct read/write, shell, search, MCP, or internal tools were available, and direct/helper calls were zero. Each fresh context had the same 16,540-byte ambient memory message. Therefore this was not packet-only or extension-free/global-confinement context; the raw Fable “packet-only” title is qualified by this observed context. Memory contents and raw prompts are deliberately not reproduced here.

The comparison, native audit and exact capability evidence were used to write this summary; raw prompts, session locators and JSON audit data remain outside the tracked record. They are not copied into this public record.

## Comparison and synthesis

Astra favors contract/dependency ordering: finish lower composition, settle G/H carrier ownership, complete H1 records, serialize consumer activation, then close migration/editing/CLI/assets and integrated gates; independent H1 work may proceed when inputs are settled. Fable favors an early drift/check-debt triage and interim-root convergence before consumer work (A′), to avoid muddy regression signals and rework. Astra rejects treating blanket green-base convergence as a proven prerequisite because the packet does not establish all H1 dependencies; Fable’s convergence-first rationale is plausible sequencing advice, not a packet-established requirement.

Astra’s synthesis adopts early failure accounting but rejects a blanket green-base prerequisite and unproved cheap config-only convergence, carrier placement, legacy TOML, or whole-byte provider oracles. It preserves independent progress, all 44, and all authoring. Both views leave exact carrier placement, exhaustive remaining consumer census, native/backend confirmation, and whole-graph migration/editor acceptance unresolved. Multi-pack attribution, physical occurrence, and approved carrier remain engineering gaps, not model policy. Neither opinion is a buildable accepted plan or reopened ruling. Future CB9/10 and historical item19 are separate questions.

## Attributable usage

Raw provider-reported per-message values, deduplicated by native message identity; zeros and unavailable values are not conflated. Input/cache fields are provider fields and are not assumed additive. Costs below are configured estimates, not invoices or public-rate calculations.

| Role | Native message | Model / effort | Calls | Input | Output | Cache read | Cache write | Total tokens | Reported cost |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| Astra initial | `24e1ee76` | `global.openai.gpt-6-astra`, HIGH | 1 | 2 | 4,066 | 0 | 6,755 | 10,823 | $0.2877575 |
| Fable initial | `5025218c` | `global.anthropic.claude-fable-5-1`, HIGH | 1 | 4 | 9,991 | 0 | 11,975 (reported 1h tier) | 21,970 | $0.73909 |
| Astra merge | `cfbff708` | `global.openai.gpt-6-astra`, HIGH | 1 | 2 | 4,746 | 0 | 12,474 | 17,222 | $0.393245 |
| **Three advisory calls** |  |  | **3** |  |  |  |  |  | **$1.4200925** |

The Luna audit/comparison’s eight unique actual-message raw rows (all `global.openai.gpt-6-luna`, LOW) were: `f5e8ac3e` (input 2, output 86, cache read 0, cache write 25,218, total 25,306, cost $0.00319545); `e4e56526` (2, 289, 25,218, 496, 26,005, $0.00045888); `9267408f` (2, 421, 25,714, 3,862, 29,999, $0.00095059); `9ab21a83` (2, 464, 29,576, 14,299, 44,341, $0.002315335); `a249017d` (2, 319, 43,875, 9,805, 54,001, $0.001824075); `c4b951f1` (2, 678, 53,680, 13,814, 68,174, $0.00260275); `9e41e5cb` (2, 702, 67,494, 900, 69,098, $0.00113864); and `f9119546` (2, 1,595, 68,394, 890, 70,881, $0.00159289). The normalized sums are input 16, output 4,554, cache read 313,951, cache write 69,284, total 387,805, logged cost $0.01407861. Aggregate run metadata was not added again. The requested-cycle configured estimate is $1.43417111, not an invoice or verified public-rate estimate. Parent/preparation work is unavailable/excluded; earlier Sol21 pass $0.0569415 is separate. No public pricing research was performed.

## Scope and non-claims

This record does not claim packet-only isolation, extension-free execution, a global confinement guarantee, native certification, or current runtime acceptance. It does not contain raw prompts, JSONL, private memory, status dumps, credentials, or hidden reasoning. The advice and usage are retained for review of evidence and process only.
