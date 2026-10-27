---
title: "Historical whole-document computed-briefings advisory evidence"
date: 2026-10-27
tags: [computed-briefings, advisory, evidence]
summary: "Preserved input and two HIGH advisory opinions with provenance, boundary caveats, usage, and comparison; not a design ruling or implementation."
---

# Historical whole-document computed-briefings advisory evidence

This record preserves the complete input and the two independently produced, whole-document
HIGH advisory opinions from the 2026-10-27 comparison. The adjacent text files retain the
original Markdown bodies byte-for-byte. They are historical inputs and opinions, not current
design documents, not accepted advice, and not implementation authorization. Their former open
question prompts and suggestions are preserved as evidence, not reactivated as current questions.

This is the verified whole-current pair, not the earlier pilot, which used a smaller input and had a failed write-boundary run.

## 1. Input and temporal scope

The complete original input is [`input.txt`](input.txt), 553 lines and 35,004 bytes. Its Git blob
is `202304d5a060e17a1c6a55547621144621cd3faf` and its SHA-256 is
`be625f6d67aa6d1b81123fa38162343e3088859c899136a2849a9cfc623c24fc`. That document was current
at the opinions' invocation on commit `9d040116b531b937d595148a42dcb27cee0c1153`, not at the later
accepted design revision `0aaea898e76f36dce66d5f4017d6e3c09af078df`. At that revision, the current
[computed-briefings design](../../design/computed-briefings.md) had 57
insertions and 113 deletions relative to that captured input. Both task prompts contained the
entire captured document exactly once; their task-prompt SHA-256 values are identical:
`b775b2da6b1da2a85482973a8a64b7bf046002dacc2fd155fa3b8782c161f65a`.

Since those calls, the owner recorded the three rulings in the current
[computed-briefings design](../../design/computed-briefings.md#decision-ledger):

- **CB1:** Core supplies approved facts through named, declared reads; there is no pack-published
  proxy API.
- **CB2:** Producers receive named facts, not the whole effective config; pack-owned typed
  settings may be added when a real need appears.
- **CB3:** Producer Lua errors, timeouts, invalid returns, and oversized returns are fatal to the
  composition and invoking operation; stale, partial, or fallback briefings are not delivered.

Accordingly, the old Fable recommendation to degrade after producer errors is superseded by the
owner's CB3 ruling. Neither opinion settles further design details. A later whole-current review
for criteria 9 and 10 remains owed after their accepted landings; this evidence record does not
claim to satisfy it.

## 2. Original opinions and provenance

- [`astra-high.txt`](astra-high.txt) is Astra's original full opinion, SHA-256
  `7eb56e6e1ddb9d8b45e414945aede0cb8e6726b7ebea46eee0d72d9b33db8048`. Native run
  `f48b7d3d-4132-4343-9c01-8156b2ec289b`, assistant message `e7f5c184`; resolved provider/model
  `amazon-bedrock` / `global.openai.gpt-6-astra`, HIGH.
- [`fable-high.txt`](fable-high.txt) is Fable's original full opinion, SHA-256
  `3b479d5fa0f593800f1ee740b2af494bcf7e8f093ae3052ad5c3ea46e8d4c84a`. Native run
  `fdc3fa89-844e-4033-a409-c9049d111d0f`, assistant message `4554ab68`; resolved provider/model
  `amazon-bedrock` / `global.anthropic.claude-fable-5-1`, HIGH.

The complete original outputs were compared byte-for-byte with those native run outputs. Both
opinions are advice, not owner decisions or an implementation plan. The source document and the
[context-first-design procedure](../../../.pi/skills/context-first-design/SKILL.md) remain the
relevant authorities for the design and advisory boundary.

## 3. Effective tool boundary and context caveat

For both calls, the audited effective direct tool list was exactly `subagent`,
`contact_supervisor`, and `subagent_supervisor`. Direct `write`, repository read/search, shell,
MCP, and internal capabilities were absent; each model made zero direct tool calls and zero
helper calls. Bounded read-only helper agents were permitted through delegation, but none were
used.

This proves the audited direct no-write capability boundary and observed non-use. It does **not**
prove extension-free execution or packet-only/global context isolation. Although inheritance was
configured false, both sessions received the same 16,415-byte ambient persistent-memory message.
Its contents are intentionally not reproduced here. Treat the prompts as identical and blind to
each other's opinion, but not as isolated to the packet alone.

## 4. Deduplicated native usage and recorded estimates

The values below are the one attributable assistant message per model, deduplicated by native
message identity. Cache reads were explicitly reported as zero. Fable's cache writes were all
reported in the one-hour tier.

| Model | Run / message | Input | Output | Cache read | Cache write | Total tokens |
| :--- | :--- | ---: | ---: | ---: | ---: | ---: |
| Astra HIGH | `f48b7d3d-4132-4343-9c01-8156b2ec289b` / `e7f5c184` | 2 | 9,481 | 0 | 14,624 | 24,107 |
| Fable HIGH | `fdc3fa89-844e-4033-a409-c9049d111d0f` / `4554ab68` | 4 | 15,853 | 0 | 22,574 (1h) | 38,431 |

The native usage record's configured estimates are **$0.656870** for Astra and **$1.244170**
for Fable, subtotal **$1.901040**. Their recorded arithmetic uses input at $10 per million
reported tokens, output at $50 per million, Astra cache writes at $12.50 per million, and Fable
one-hour cache writes at $20 per million:

- Astra: `(2 × 10 + 9,481 × 50 + 14,624 × 12.5) / 1,000,000 = 0.656870`.
- Fable: `(4 × 10 + 15,853 × 50 + 22,574 × 20) / 1,000,000 = 1.244170`.

These are recorded configured estimates, not invoices and not a freshly verified public rate-card
calculation. Parent, collector, and audit costs are unavailable or excluded. Native aggregate and
session views refer to these same calls and are not added again; no missing parent/helper usage is
inferred to be zero.

## 5. Findings, differences, and limits

Both opinions support the broad producer shape but advise against handing off the captured
document unchanged. They identify related gaps around the distinction between approved policy,
broker availability, and successful operations; ownership and meaning of source-specific
permissions; where remote identity comes from; deterministic iteration over maps; consistent
failure handling, including missing registrations; and safe binding of affirmative push prose to
a repository and destination ref.

Astra emphasizes a complete contract: source-specific permission semantics, one immutable
composition snapshot, lock and publication behavior, revocation independence, and live-apply
recomposition in the first permission-aware deliverable. Fable argues for a smaller first slice,
including deferring working-set and remote-map support and live-apply recomposition. The difference
is advisory; neither recommendation overrides the owner rulings or authorizes a design change.

Their implementation observations were grounded in the supplied input and dated code claims, not
independently reverified in this evidence capture. The current accepted design records CB1/CB2/CB3
but explicitly says its system facts were checked against the 2026-10-24 snapshot; this record
adds no software validation, new review, pricing research, or current-tree implementation claim.

## 6. Verification and scope

The recorded input blob was read back and matched the preserved input; the two preserved opinion
bodies were each compared byte-for-byte with their original native outputs. Usage figures and
boundary claims above were checked against the existing per-message audit. Only this README and
the three adjacent `.txt` evidence files belong to this tracked capture. No native sessions,
raw audit JSON, prompts beyond the original input document, ambient memory contents, credentials,
or personal progress records are published here.
