---
name: context-first-design
description: Use for bounded, evidence-first advisory design opinions from cited packets, with owner-directed comparison and honest usage accounting; no design authority or repository exploration.
---

# Context-first design

Use this workflow only for an owner-requested advisory design opinion. The owner keeps the question, scope, model choice, and any design ruling. The result is advice—not an owner decision, accepted design, implementation plan, or permission to change source, schema, agent configuration, global settings, credentials, or tool access.

## Procedure

1. **Fix the question and boundary.** Use the owner's exact question and constraints. If either is unclear, ask before researching. Do not convert an opinion request into a design ruling or implementation task.
2. **Build or reuse a small evidence packet.** Prefer a minimal-context collector profile, with unrelated project/global/skill inheritance disabled where supported; do not edit agent configuration, add tools, or widen permissions to obtain one. Start from named source seams, group targeted reads, and reuse verified evidence. A dated, cited packet may be reused when its source revision and relevant seams are still valid; collection-profile optimization is not a prerequisite. Give a collector a deliberately small **soft** exploration budget—not a hard tool/evaluation cap—and stop once the packet is grounded; leave narrow gaps explicit. Cite exact repository-relative paths and stable symbols, headings, or line ranges, and identify the source revision. Do not paste whole files or directories unless the owner explicitly asks for review of a named whole document; then include that full document rather than excerpts. Separate observed facts from owner constraints, interpretations, and unknowns. Present options neutrally; do not smuggle in a recommendation. Keep usage-accounting collection in its own task/artifact; never append its accounting notes or complete collector report to the designer packet.
3. **Close only narrow fact gaps.** If a required fact is absent, ask a configured low-cost scout one question tied to the packet's named source seam. Request a concise answer, exact citation, and what would falsify it—not a file, directory survey, or broad exploration. Keep the initial designer opinions blind to each other. For a bounded helper dispatch, use `async: false`, set `maxOutput: { bytes: N }` with `N <= 6000`, and set `output: false`; do not nest helpers or ask them to publish/write anything. `maxOutput` limits returned text only; it is not a tool-call, token, or evaluation budget. If the fact remains unknown, leave it explicitly unknown; never fill it by inference.
4. **Freeze the packet.** Before asking for a comparison, save one compact, design-only packet containing the question, cited evidence, current data flow or contract, owner constraints, neutral options, and unknowns. Inspect the exact text selected for the designer's inline prompt and remove any accounting appendix or unrelated collector output before freezing it. Preserve the exact packet and version. Do not silently change it between model opinions. If a new fact is needed after a model starts, collect it separately, record the continuation and usage, update and freeze a new packet, then give each compared model that same packet and question before treating the opinions as comparable.
5. **Run advisory opinions with a verified boundary.** Select the dedicated workspace profile [`designer-advisory`](../../agents/designer-advisory.md) for this workflow; do not use the write-capable document-author profile. That profile pins `output: false`; keep the launch free of any output-path override and use the native passive agent list or launch preflight to confirm the selected project profile and no output-file path. Its direct tools must be only `subagent`, `subagent_supervisor`, and `contact_supervisor`; `systemPromptMode` must be `replace`; project/global context and skill inheritance must be off. A per-call `options.tools` subset is not proof of restriction: in the audited Astra/Fable pair, the requested subset did not remove capabilities and both effective lists still included `write` and `subagent`. Before a paid launch, confirm the profile's tool allowlist, inheritance, fresh context, and absence of an output-file path. At completion, inspect native launch/session evidence and actual effective tool capability audit as well: `write`, repository read/search, shell, and any unapproved tools must be absent, not merely unused. Retain only explicitly authorized bounded-helper or supervisor tools; their presence does not relax the direct-tool restriction. If the preflight or run audit shows a forbidden tool or cannot establish the boundary, say so plainly and do not claim isolation or a verified pair. Do not treat a prompt, no tool calls, or a per-call subset as proof. Do not widen access to make the run easier, and do not disable writing globally: design-document authors may need write access to publish. Keep the selected task-specific prompt to the frozen packet plus known runtime wrapper material—no accounting appendix, unrelated context, or output-file directive. If a helper is needed for a narrow packet gap, use only an allowed helper and set its synchronous, inline-only, bounded-output parameters as in step 3; do not ask it to edit or publish. If the profile or API cannot enforce the boundary, stop and seek owner direction before launching or retrying. Request advice only, grounded in the packet, with a recommendation, strongest counterargument, trade-offs, assumptions/unknowns, and confidence. No file edits or implementation.
6. **Let the owner assess the first pass when using the default path.** Present the opinion, source packet, process observations, and measured usage separately. Wait for the owner to judge process, opinion, and cost before a comparator. This staged review is the default, not a gate when the owner has already explicitly requested a named parallel comparison.
7. **Honor the owner's comparison shape.** The default is one opinion, then wait for the owner to assess it and explicitly name any comparator; never launch an expensive comparator by default or on a model's suggestion. If the owner explicitly names multiple models for a comparison, run those named opinions independently in one parallel fanout without a staged permission gate. Initial opinions are blind: give every model the same frozen packet, exact question, requested effort/thinking setting, and output contract; do not expose another model's answer or intermediate result. An explicitly owner-authorized follow-up may instead critique another opinion only when the owner supplies that opinion and asks for critique; mark that task as a follow-up, distinguish the other model's claims from packet/source facts, and keep the critique advisory. Before launch, verify the actual API supports the required fanout and return-only child behavior. Compare the native effective tool lists for all named runs; if any boundary differs or fails, report it and do not call the pair verified. Present opinions side by side, with differing conclusions and uncertainties intact. Run a further comparator only if the owner explicitly asks after assessing the comparison.
8. **Report evidence and ownership.** State what is evidenced and unknown, each model's advisory conclusion, requested and resolved effort, and whether the native tool boundary was actually satisfied. The owner—not either model—decides whether to rule or proceed. Do not claim the design is accepted, pilot-tested, or implemented without separate evidence.

## Follow-ups and retained runs

A continuation or retry is not implied by a completed run's label. Before resuming, inspect the runtime's authoritative retained-run eligibility and the exact original packet, model, effort, tool contract, opinion, and native IDs. Resume only when eligible and when the same verified boundary and return-only contract can be preserved. A later profile change cannot remove tools from an already-created retained child; never describe an unsafe retained run as write-free. Carry forward every new run/session ID. If eligibility or contract preservation fails, stop and report the exact observed state; do not silently fall back to a CLI, another model, or a fresh run. Never promise that a worktree, cache, or other retained state will survive. After a profile correction, any continuation must be explicitly owner-authorized and launched fresh under the corrected profile; preserve the original run references and label the tool-contract change rather than presenting it as a retained resume.

A fresh continuation is a different procedure: do it only when the owner explicitly authorizes a fresh run of the same model. Give it the frozen packet, its own prior opinion, and the new question; label it fresh, not retained. Do not inject the collector transcript or hidden reasoning. Initial opinions remain blind; if the owner explicitly authorizes critique of another opinion, include that opinion only because the owner supplied it for that follow-up, and separate its claims from facts established by the packet or cited sources. Apply the same audited tool boundary and return-only contract as a first opinion.



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

Record the exact native run ID, session ID, resolved provider/model ID, role, packet version, and usage source for the collector/helper, designer, comparator, and parent/skill-author work separately. Keep usage/accounting collection and its artifact separate from the frozen packet and designer prompt. Use native per-assistant-message provider usage as the source for exact per-call fields; `totalTokens` may be present there even when metadata lacks it. Deduplicate the same message across session/artifact views before summing. Keep native run metadata and aggregate totals for reconciliation, not as a substitute for per-message fields or an additional amount to add. Preserve each reported `input`, `output`, `cacheRead`, `cacheWrite`, `totalTokens`, and cost field separately where available; keep provider field names if they differ. Provider `input` may exclude separately reported cache counters: show each field independently, and label a prompt/input total only when that provider's meaning and composition are established. Every continuation, retry, or additional model call is a separate observed call and remains in its role's total. Cache/replayed context is represented by reported counters; do not estimate it from prompt size. A fresh context describes conversation state, not cache state: never infer cold cache from freshness, and keep cache reads/writes distinct from ordinary input.

Keep **raw counters** (the exact values reported per call) distinct from **normalized totals** (the arithmetic sum of deduplicated, attributable raw values for a named role/model). Sum only the same reported field across unique calls; do not derive missing counters or infer that `input`, cache counters, and `totalTokens` have an additive relationship unless the provider's semantics establish it. Do not estimate tokens from characters, add a cumulative run/session total to the messages it summarizes, subtract ambiguous aggregates, or attribute a helper's tokens to the designer. Use aggregate cost/status views only as reconciliation evidence, not as another amount to add. If `reasoning` is reported, note whether it is already included in `output`; do not double-count it or claim a separate reasoning-token total when none is reported.

For each field, distinguish **zero reported** (the source explicitly reports zero) from **missing/incomplete** (no attributable value, unavailable artifact, or partial coverage). Never print an unavailable value as `0`. If the source cannot separate parent, helper, and designer usage, mark that portion incomplete rather than allocating it by guess. Count every continuation, retry, and helper call separately, and deduplicate copied messages across a resume lineage by native message identity. Do not include credentials, API keys, or private authentication data in the record.

For a public rate-based cost estimate, verify the current provider documentation for the resolved model, requested/resolved effort, cache category and retention tier, and rates at the time of the estimate. Multiply only observed, attributable token-category footprints by those rates; state the pricing date, tier, cache retention/TTL, category mapping, and source. Keep this estimate distinct from native-reported cost, configured estimates, and invoices; do not embed static prices or model levels in this reusable procedure. When effort levels or cache terms are ambiguous, report that ambiguity instead of assuming equal reasoning effort or interchangeable tiers.



A compact accounting table may use this shape:

| Role | Native run/session | Resolved provider/model | Calls | Input | Output | Cache read | Cache write | Total tokens | Cost | Status/source |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|

Use exact observed IDs and values for a real run; use no sample IDs or fabricated measurements.
