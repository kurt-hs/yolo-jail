---
name: context-first-design
description: Run a bounded, evidence-first design-opinion comparison using a cited source packet and staged model review. Use when an owner asks to evaluate a design question without delegating repository exploration or authority to an expensive designer.
---

# Context-first design

Use this workflow only for an owner-requested advisory design opinion. The owner keeps the question, scope, model choice, and any design ruling. The result is advice—not an owner decision, accepted design, implementation plan, or permission to change source, schema, agent configuration, global settings, credentials, or tool access.

## Procedure

1. **Fix the question and boundary.** Use the owner's exact question and constraints. If either is unclear, ask before researching. Do not convert an opinion request into a design ruling or implementation task.
2. **Build a small evidence packet cheaply.** Prefer a minimal-context collector profile, with unrelated project/global/skill inheritance disabled where supported; do not edit agent configuration, add tools, or widen permissions to obtain one. Start from the named source seams, group targeted reads, and reuse already verified evidence. Do not reread whole references or metadata or conduct a general observability survey. Give the collector a deliberately small **soft** exploration budget—not a hard tool/evaluation cap—and stop once the packet is grounded; leave narrow remaining gaps explicit. Cite exact repository-relative paths and stable symbols, headings, or line ranges, and identify the source revision. Do not paste whole files or directories. Separate observed facts from owner constraints, interpretations, and unknowns. Present options neutrally; do not smuggle in a recommendation. Keep usage-accounting collection in its own task/artifact; never append its accounting notes to the evidence packet or pass the collector's complete report through to the designer.
3. **Close only narrow fact gaps.** If a required fact is absent, ask a configured low-cost scout one question tied to the packet's named source seam. Request a concise answer, exact citation, and what would falsify it—not a file, directory survey, or broad exploration. Add a supported answer as a cited packet addendum. If the fact remains unknown, leave it explicitly unknown; never fill it by inference.
4. **Freeze the packet.** Before asking for a comparison, save one compact, design-only packet containing the question, cited evidence, current data flow or contract, owner constraints, neutral options, and unknowns. Inspect the exact text selected for the designer's inline prompt and remove any accounting appendix or unrelated collector output before freezing it. Preserve the exact packet and version. Do not silently change it between model opinions. If a new fact is needed after a model starts, collect it separately, record the continuation and usage, update and freeze a new packet, then give each compared model that same packet and question before treating the opinions as comparable.
5. **Ask the existing designer role for the first opinion.** Launch it with fresh context and pass only the frozen design packet inline. Its role must have project/global context and skill inheritance disabled, and no direct repository read, search, or shell tools. Do not add tools, extensions, permissions, or inherited context to make the run easier. Before relying on the result, verify from native launch/session evidence—not an artifact label alone—that the launch was fresh, the actual tool set lacks repository read/search/shell tools, and the selected prompt contains only the frozen packet. If this boundary cannot be confirmed, report the gap rather than claiming isolation; if the existing role is unavailable or its boundary is misconfigured, stop and ask the owner rather than widening it. Request an advisory opinion only, grounded in the packet, with a recommendation, strongest counterargument, trade-offs, assumptions/unknowns, and confidence. No file edits or implementation.
6. **Let the owner assess the first pass.** Present the opinion, source packet, process observations, and measured usage separately. Wait for the owner to judge process, opinion, and cost. Do not launch an expensive comparator on the designer's suggestion or by default.
7. **Run at most one selected comparator.** Only after the owner explicitly names one configured expensive comparator, give it the same frozen packet, exact question, effort/thinking setting, and output contract as the first opinion. Do not show it the first opinion. Present both opinions side by side. Run a second comparator only if the owner judges the first comparison and explicitly asks for one.
8. **Report the boundary.** State what is evidenced, what remains unknown, each model's advisory conclusion, and that the owner—not either model—decides whether to rule or proceed. Do not claim the design is accepted, pilot-tested, or implemented without separate evidence.

## Evidence packet shape

Keep this compact and cite every factual claim:

```markdown
# Design question
<owner's exact question>

## Evidence and current contract
- `<repo-relative path>` — `<symbol, heading, or line range>`: <observed fact>

## Current data flow
<short path from entry point through relevant contract/seam>

## Owner constraints
- <constraint and its source>

## Neutral options
- A — <behavior and consequence>
- B — <behavior and consequence>

## Unknowns
- <fact not verified, or “none identified”>
```

## Usage and cost record

Record the exact native run ID, session ID, resolved provider/model ID, role, packet version, and usage source for the collector/helper, designer, comparator, and parent/skill-author work separately. Keep usage/accounting collection and its artifact separate from the frozen packet and designer prompt. Use native per-assistant-message provider usage as the source for exact per-call fields; `totalTokens` may be present there even when metadata lacks it. Deduplicate the same message across session/artifact views before summing. Keep native run metadata and aggregate totals for reconciliation, not as a substitute for per-message fields or an additional amount to add. Preserve each reported `input`, `output`, `cacheRead`, `cacheWrite`, `totalTokens`, and cost field separately where available; keep provider field names if they differ. Provider `input` may exclude separately reported cache counters: show each field independently, and label a prompt/input total only when that provider's meaning and composition are established. Every continuation, retry, or additional model call is a separate observed call and remains in its role's total. Cache/replayed context is represented by reported counters; do not estimate it from prompt size.

Keep **raw counters** (the exact values reported per call) distinct from **normalized totals** (the arithmetic sum of deduplicated, attributable raw values for a named role/model). Sum only the same reported field across unique calls; do not derive missing counters or infer that `input`, cache counters, and `totalTokens` have an additive relationship unless the provider's semantics establish it. Do not estimate tokens from characters, add a cumulative run/session total to the messages it summarizes, subtract ambiguous aggregates, or attribute a helper's tokens to the designer. Use aggregate cost/status views only as reconciliation evidence, not as another amount to add. If `reasoning` is reported, note whether it is already included in `output`; do not double-count it or claim a separate reasoning-token total when none is reported.

For each field, distinguish **zero reported** (the source explicitly reports zero) from **missing/incomplete** (no attributable value, unavailable artifact, or partial coverage). Never print an unavailable value as `0`. If the source cannot separate parent, helper, and designer usage, mark that portion incomplete rather than allocating it by guess. Do not include credentials, API keys, or private authentication data in the record.

A compact accounting table may use this shape:

| Role | Native run/session | Resolved provider/model | Calls | Input | Output | Cache read | Cache write | Total tokens | Cost | Status/source |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|

Use exact observed IDs and values for a real run; use no sample IDs or fabricated measurements.
