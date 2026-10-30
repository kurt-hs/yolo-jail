---
title: "One login per machine — sharing credentials between the host and every jail"
date: 2026-10-13
status: in-review
stage: DESIGN
next: "Rule OQ-ULS1 (how the host reaches a machine-shared credential), then OQ-ULS2 (whose declaration turns it on) and OQ-ULS3 (the pi↔Muse translation layer)"
tags: [credentials, auth, host, jails, shared-credentials, pi, muse, oauth, subscription]
summary: "yolo already shares an agent's login across every jail on a machine through a pack-declared machine-scope state dir plus the shared_credentials hook. The host is the one notch that reads none of it, and two agents that authenticate against the same provider still keep two files. This design asks whether the machine store should become the host's store too, and whether one agent's login should serve another agent of the same provider."
vantage:
  status-chip: true
---

# One login per machine — sharing credentials between the host and every jail

**Status:** 2026-10-13. Nothing built by this design. Current state verified at `80e08f98b`:
the pack-declared machine-share mechanism exists and is in use by four packs
([`packs/claude`](../../packs/claude/pack.toml), [`packs/agy`](../../packs/agy/pack.toml),
[`packs/pi`](../../packs/pi/pack.toml), [`packs/muse-code`](../../packs/muse-code/pack.toml)), and
[`../reference/agent-credentials.md`](../reference/agent-credentials.md) is the authority for what is
built.

> **In short.** The machine store already makes one login serve every jail; the host and the
> non-container notches are simply not wired to it, and two agents of one provider keep two stores.
> Closing both is a declaration and a wiring change, not new credential machinery.

**Why it matters.** A user with a host login and three jails authenticates a different copy in each,
and the copy the next launch reaches is whichever one is freshest — so "I already logged in" is
usually true of some other notch. The pi/Muse pair is the sharpest case: both authenticate against
the same Meta IdP and mint from the same endpoint, yet a login taken in one leaves the other asking
again.

**The shape.** Generalize the two sharing patterns yolo already has — a pack-declared
machine-scoped store read by every jail, and a host-owned canonical service materialized as views —
into one rule that names, per credential, which notch owns the canonical copy and which notches read
a view.

**Cost.** A host-writable credential is a wider trust surface: a jail that shares with the host can
write a credential the host user then uses outside any confinement. The reference doc's standing
warning is *do not generalize either sharing mechanism*, and this design exists to decide whether to
overturn that warning deliberately.

**Start at [§3](#3-the-host-is-the-missing-half)** — the asymmetry. The mechanism inventory in [§2](#2-what-exists-today) and the translation question in [§5](#5-the-pimuse-translation-layer) fall out of it.

**Needs your ruling:** [OQ-ULS1](#OQ-ULS1), [OQ-ULS2](#OQ-ULS2), [OQ-ULS3](#OQ-ULS3).

**Reads with:** [`../reference/agent-credentials.md`](../reference/agent-credentials.md) (what is
built today; the credential boundary and the per-backend table),
[`provider-reachability-and-model-names.md`](provider-reachability-and-model-names.md#9-decision-ledger)
([OQ-PR2](provider-reachability-and-model-names.md#9-decision-ledger), the ruling this design extends: pack-declared only, adopted incrementally),
[`claude-login-without-interception.md`](claude-login-without-interception.md) (the one host view
that exists), [`agent-credentials.md`'s OpenAI section](../reference/agent-credentials.md#the-openai-subscription-credential-service) and
[`keychain-from-a-jail.md`](keychain-from-a-jail.md) (the host-canonical and native-store
directions), [`universal-login-sharing-plan.md`](universal-login-sharing-plan.md) (the implementation
sketch — incomplete while the questions are open).

---

## 1. The goal

**Every credential yolo touches should be shared between the host and every jail, uniformly where it
is safe to be, and configurable where it is not.**

Stated as a property of the system:

- **P1 — one login.** A credential is taken once per *machine*, not once per notch. A login taken at
  the host reaches every jail that selects the agent, and a login taken in a jail becomes the one the
  host uses, without a copy step the user has to remember.
- **P2 — one owner.** Each credential has exactly one canonical copy, and every other notch holds a
  *view* of it. "Shared" must not become "N copies that happen to be equal for now"; the failure mode
  to avoid is the one [`../reference/agent-credentials.md`](../reference/agent-credentials.md) names
  for the per-workspace overlays — N drifting copies, and the freshest one decided by launch order.
- **P3 — opt-in, declared.** Following
  [`OQ-PR2`](provider-reachability-and-model-names.md#9-decision-ledger), the sharing is a property a
  **pack declares**, never a config key core invents and never a value. A pack that authenticates
  against a provider says which of its files is the login; yolo decides where the one copy lives.
- **P4 — the boundary is a decision, not an accident.** Sharing with the host crosses the credential
  boundary on purpose. Each credential states whether the host is a *reader* of the machine store, a
  *writer* into it, or neither.

**Non-goals.** This design does not move a credential to the host's real home as the canonical copy
(the home is what jails exist to keep out of reach); it does not add a static-key path for a
subscription-only provider ([§6](#6-what-this-does-not-license)); and it does not fold a provider's
API-key names into the same mechanism — those already travel
[`env_sources`](../reference/providers.md#the-credential-gate).

## 2. What exists today

Two mechanisms already share a credential, in opposite directions, and neither generalizes the
other. The inventory matters because the design is a choice between extending one of them or
replacing both.

| Mechanism | Canonical copy | Who reads it | Declared by | Built in |
| :--- | :--- | :--- | :--- | :--- |
| **Machine-shared agent file** | a pack's machine-scope `state` dir (`paths.GlobalHome()`, `<GlobalStorage>/home`) | every jail (a relative symlink from the agent's own path, `linkSharedCredential` → `linkThroughShared`) | the `state` contribution plus the `shared_credentials` hook | [`packhooks.go`](../../internal/entrypoint/packhooks.go), [`sharedlink.go`](../../internal/entrypoint/sharedlink.go) |
| **Host-owned credential service** | the host daemon's own state (OpenAI subscription, AWS SSO) | Codex, Pi and opencode through yolo-managed views; an in-jail adapter | the pack's loophole and `env_sources/awsauth` pointer | [`agent-credentials.md`'s OpenAI section](../reference/agent-credentials.md#the-openai-subscription-credential-service), [`aws-auth`](../../packs/aws-auth) |

Three facts about the first mechanism decide most of the design:

- **The hook runs only in the jail entrypoint.** `yolo host apply` composes config surfaces into the
  real home but runs no pack hook and mounts no machine-scope dir, so the host reads its own
  `~/.config/muse/auth.json`, `~/.pi/agent/auth.json` and so on. The one exception is
  [`hostClaudeView`](../../internal/cli/hostclaudeview.go), which points a host-launched Claude at a
  broker-written *view* — read-only, off by default behind `YOLO_CLAUDE_CREDENTIAL_VIEW=1`, and only
  for `yolo host -- claude`.
- **`shared_credentials` collapses by base name.** The shared side is
  `filepath.Join(sharedDir, filepath.Base(from))`, so two packs that declare the *same* machine-scope
  `at` and whose `from` both end in `auth.json` already resolve to **one shared file**. The mechanism
  therefore needs no change to make two agents share one login; only the *format* blocks it
  ([§5](#5-the-pimuse-translation-layer)).
- **It is jail-only today because nothing declares a host target.** The `at` names a machine-scope
  `state` dir, which only a jail mount consumes. A host reader needs a second, host-side statement of
  where the canonical file lands in the real home.

## 3. The host is the missing half

The asymmetry is the whole problem, and it is not an accident of code:

- **Jails share with each other** for the four packs that declare the pair. Log in once in any jailed
  agent, and every later jail finds it.
- **The host shares with nothing** for those same agents. It keeps its own file, so a host login and a
  jail login are separate credentials that expire separately and can disagree.
- **The credential services are the mirror image**: canonical on the *host*, viewed in a jail. So the
  system already contains a working host-canonical pattern — it just is not the pattern the
  agent-native logins use.

Why the boundary was drawn where it was is sound and should be stated before arguing to move it:
the host home is exactly what a jail must not mount, so the safe direction has been "the jail gets a
copy of the host's thing, or a service, never the host's file." A machine-scope store was added for
the *agent-native* logins because an agent obtains its own credential and the host has no service
to hand it out. What was never done is the last step: pointing the *host's* agent at that same store.

A host that reads the machine store crosses no confinement boundary the machine store does not
already cross — the store is host-owned and jail-writable by design. What it changes is that the
host's own agent now consumes a file a confined process can write. That is the trust trade
[OQ-ULS1](#OQ-ULS1) rules on.

## 4. How the host reaches the store

Three shapes, in increasing cost. The choice is [OQ-ULS1](#OQ-ULS1); this section states the
trade-offs each carries.

**A — Host view (extend `hostClaudeView`'s shape).** The canonical copy stays in the machine store;
the host agent is pointed at a yolo-managed *view* directory, and the host broker keeps the view
current. Read-only from the host's side; a login taken in a jail propagates out, a login taken at the
host must still be captured back into the store by the broker. Precedent: the Claude host view and
the OpenAI/AWS service views. Cost: a refresh-capable daemon per agent, and a host agent that refuses
a redirected store cannot use it.

**B — The store is canonical for both notches.** yolo redirects the host agent at the store —
either by linking the real home's credential path into it (a symlink yolo writes into `~/.config/...`)
or by pointing the agent's config at the store through an environment or config knob. Two-way by
construction: whoever writes the file, both notches read the same inode. Cost: mutating the real home
(the exact surface the boundary protects), and a store under yolo's state dir becoming the *only*
copy — a state reset would destroy a login the user cannot re-obtain without another browser trip. If
B is chosen, the store must move out of `state` into a durable location, or the design must forbid a
reset from deleting it.

**C — A service for every credential.** Generalize the OpenAI/AWS daemon so every credential is
minted and held host-side and materialized as narrow views at every notch. No file is shared at all.
Cost: the most machinery, a daemon per provider, and it does not fit an agent that insists on writing
its own login file in place (Muse Code among them) without the same redirection B needs.

**Rejected — mount the host home.** [`../reference/agent-credentials.md`](../reference/agent-credentials.md)
is explicit that nothing is mounted from the real home for credentials, and a read-write `mounts`
element cannot substitute (refused outside `/ctx/`). Not revisited.

## 5. The pi/Muse translation layer

This section names a **cross-agent credential translation** *(coined here)*: a mechanism that lets
two agents which authenticate against the same provider read and write **one** credential file,
though each expects a different schema.

**The facts, verified.** Both clients authenticate against `auth.meta.com` through the same device
flow and mint from `https://api.meta.ai/muse-code/key`; both renew automatically. Muse writes
`~/.config/muse/auth.json` as
`{schema_version, providers:{meta:{mechanism, access_token, expires_at}}}`; pi writes
`~/.pi/agent/auth.json` as a flat `providerId → credential` map, and its reader is strict — an extra
top-level key such as `providers` is rejected with `Invalid auth.json credential for provider
"providers"`. Because the base-name collapse in [§2](#2-what-exists-today) already puts both files
at one path when both packs declare the same machine-scope `at`, the *storage* half is free and the
*format* half is the whole problem.

**The options.** [OQ-ULS3](#OQ-ULS3) chooses between them.

| Option | Shape | Cost | Status |
| :--- | :--- | :--- | :--- |
| **A — One file, dual-parse** | both packs declare the same `at`; each agent reads the same `auth.json`. Requires that one file satisfy both readers, or that each reader tolerate the other's keys. | no core code; depends on a format both readers accept, which pi's reader **rejects** for each candidate shape | spike run 2026-10-13 — pi fails on both shapes; [see below](#the-dual-parse-spike-measured) |
| **B — A derive/transform** | yolo renders each agent's file from one canonical store, the way `models.json` is derived. | new core code: a derive and a hook kind that does not exist today (the hook set is closed — `shared_credentials`, `shared_directory`, `unshare_directory`, `per_jail_history`) | largest change; only worth it if a second agent pair needs it |
| **C — Leave two stores** | accept [`OQ-PR2`](provider-reachability-and-model-names.md#9-decision-ledger)'s gap; each agent keeps its own login. | the user logs in twice | today's state |

**Why "one file" may not survive a spike.** The two schemas disagree at the top level, not in a
field: pi's reader treats the file as a map of provider ids, so a Muse doc's `providers` key is
itself read as a provider id with a credential of the wrong shape. A tolerated unknown key would
have to be added to pi's reader (a Pi-side change, not a pack declaration), or the shared file must
be Muse's shape plus a flattened pi map that pi ignores — which is B wearing A's clothes. This is why
the leaning defers to the spike.

### The dual-parse spike, measured

A read-only spike ran pi's own reader against candidate files. Muse's reader was **not** run: the jail
holds no `~/.config/muse/auth.json`, and launching the Muse CLI was out of scope. No network is
involved — the reader parses a local file. pi's reader is `ReadOnlyAuthStorage.load()` in the build
that runs (`dist/core/auth-storage.js:164-215`): its top level **is** the provider map, and every value
must be a `type:"api_key"` or `type:"oauth"` credential.

```console
$ export PI=/home/agent/.local/share/pi-fork/node_modules/@earendil-works/pi-coding-agent
$ node --input-type=module -e '
  const { ReadOnlyAuthStorage } = await import(process.env.PI + "/dist/core/auth-storage.js");
  const { writeFileSync } = await import("node:fs");
  const load = (doc) => {
    writeFileSync("/tmp/pi-dualparse.json", JSON.stringify(doc));
    try { new ReadOnlyAuthStorage("/tmp/pi-dualparse.json").load(); return "PASS"; }
    catch (e) { return "FAIL: " + e.message; }
  };
  console.log(load({schema_version:1, providers:{meta:{mechanism:"oauth", access_token:"a", expires_at:1}}}));
  console.log(load({schema_version:1, providers:{meta:{type:"oauth", access:"a", refresh:"r", expires:1}}}));
  console.log(load({meta:{type:"oauth", access:"a", refresh:"r", expires:1}, providers:{meta:{}}}));
'
FAIL: Invalid auth.json credential for provider "schema_version"
FAIL: Invalid auth.json credential for provider "schema_version"
FAIL: Invalid auth.json credential for provider "providers"
```

- **The Muse shape is rejected before `providers` is ever reached.** The reader walks the top level,
  meets the `schema_version` number first, and rejects it as a credential for a provider of that name.
- **A Muse-shaped file whose provider *values* are valid pi credentials fails identically** — the
  `schema_version` key still comes first, so fixing only the inner credentials changes nothing.
- **A flat pi map with an extra `providers` key fails too.** An unknown top-level key is never
  tolerated: its value must itself be a pi credential, and the Muse doc's `providers` map is not one.

A's storage half is unmet as well. The two packs declare **different** machine-scope `at` dirs today —
pi at `.pi-shared-credentials` ([`packs/pi/pack.toml`](../../packs/pi/pack.toml)) and Muse Code at
`.muse-shared-credentials` ([`packs/muse-code/pack.toml`](../../packs/muse-code/pack.toml)) — while the
base-name join that would put both `auth.json` files at one path joins inside a **single** `at` dir
([`sharedlink.go`](../../internal/entrypoint/sharedlink.go)). A therefore needs one pack's `at` changed
*as well as* a file both readers accept.

**A precedence asymmetry to settle with it.** A stored pi credential wins over `META_API_KEY`, while
Muse lets `META_API_KEY` override its login. Whichever storage shape wins, the two agents must agree
on which source is authoritative when both a file and a supplied key exist, or a shared file and a
supplied key will fight.

## 6. What this does not license

- **No static key for a subscription-only provider.** Muse Code for this user mints from a
  subscription and cannot issue a Model API key, so a `META_API_KEY` plus `env_sources` is not a
  solution to Muse auth and must not be proposed as one. The login travels the agent-native file path
  this design is about, never an environment variable.
- **Nothing is shared that the pack does not declare.** No config key expresses sharing, matching
  [`OQ-PR2`](provider-reachability-and-model-names.md#9-decision-ledger); a read-write `mounts`
  element cannot substitute.
- **The host is never a mount source.** A view or a store, never the real home.
- **Not a generalization of Claude's OAuth interception.** The broker serializes a refresh; it is not
  a credential store every agent reads.

## 7. Risks

| Risk | Mitigation |
| :--- | :--- |
| A jail writes the host's credential, so a confined session can poison a login used unconfined | [OQ-ULS2](#OQ-ULS2)'s opt-in default; the store's one-writer ordering rule (`linkThroughShared`) already exists and is the reason a file swap cannot lose a login |
| The store becomes the only copy, and a `yolo` state reset destroys a login | [§4](#4-how-the-host-reaches-the-store)'s option B names this explicitly: canonical copy lives outside `state` or is exempt from reset |
| A host agent refuses a redirected credential path, so "uniform" cannot be literal | per agent, fall back to a view ([§4](#4-how-the-host-reaches-the-store) A) and disclose it |
| The pi/Muse dual-parse works for these two schemas and not for the next pair | [OQ-ULS3](#OQ-ULS3)'s options keep the general case (B) separate from the spike (A), so a one-off does not force a derive framework |
| Subscription-only logins cannot be re-obtained offline, so a broken share is worse than no share | never fatal, like `hostClaudeView`: a launch that cannot share starts on its own login and says why |

## 8. Open Questions

1. 💬 **OQ-ULS1: How does the host reach a machine-shared credential?**

   Decides whether the machine store becomes the host's store ([§4](#4-how-the-host-reaches-the-store)).
   Every later piece of the design assumes one of these three.

   - **A — Host view.** Canonical store stays jail-side; the host agent reads a broker-kept,
     read-only view.
   - **B — The store is canonical for both notches.** The host's real-home credential path is linked
     into the store, or the host agent's config is pointed at it; two-way by construction.
   - **C — A service for every credential.** Generalize the OpenAI/AWS daemon; no file is shared.

   <!-- vantage: question id=OQ-ULS1 leaning="A first, for the four agent-native logins: it extends the one host view that already exists, mutates no real-home file, and fails safe to the host's own login. B is revisited only for an agent whose client refuses a redirected store, and only with the canonical copy moved out of yolo's state dir." -->

   _Leaning:_ A first, for the four agent-native logins: it extends the one host view that already
   exists, mutates no real-home file, and fails safe to the host's own login. B is revisited only for
   an agent whose client refuses a redirected store, and only with the canonical copy moved out of
   yolo's state dir.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-ULS2: Whose declaration turns host sharing on — the pack, the user, or every pack with the pair?**

   The mechanism ([OQ-ULS1](#OQ-ULS1)) can be built before this is ruled, but the trust posture
   cannot. The reference doc's standing warning — *do not generalize either sharing mechanism* — is
   what this question is asked to overturn or keep.

   - **A — Opt-in twice: the pack declares a host target, and a switch (config or environment) turns
     it on per machine.** Claude's shape today.
   - **B — On for every selected pack that declares the pair**, with no separate switch. The
     uniform default the goal asks for.
   - **C — User config chooses per provider**, so the pack declares the capability and the user picks.

   <!-- vantage: question id=OQ-ULS2 leaning="A on landing, then B once the host view is measured on a real machine — claudeview.DefaultOn's own path. The cost A avoids is a jail-writable credential the host user then relies on, and a measure is the cheap way to earn the generalization the reference warns against." -->

   _Leaning:_ A on landing, then B once the host view is measured on a real machine —
   `claudeview.DefaultOn`'s own path. The cost A avoids is a jail-writable credential the host user
   then relies on, and a measure is the cheap way to earn the generalization the reference warns
   against.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-ULS3: Is pi/Muse a one-off dual-parse, a general derive, or out of scope?**

   Decides whether cross-agent sharing gets a mechanism at all, or whether the two stores stay two
   ([§5](#5-the-pimuse-translation-layer)).

   - **A — One shared file both readers accept.** No core code; needs a spike proving one file
     satisfies both parsers, or a Pi-side tolerance change.
   - **B — A derive/transform from one canonical store into each agent's schema.** New core code and
     a new hook shape; justified only if a second agent pair needs it.
   - **C — Out of scope.** Two stores remain, as
     [`OQ-PR2`](provider-reachability-and-model-names.md#9-decision-ledger) accepts.

   <!-- vantage: question id=OQ-ULS3 leaning="Not A on present evidence: the spike ran, and pi rejects both candidate shapes, so one file does not satisfy both readers as they stand. Its own fallback was C; whether to take that now or build B is the maintainer's ruling, and Muse's half is unmeasured." -->

   _Leaning:_ Not A on present evidence: the spike ran, and pi rejects both candidate shapes, so one
   file does not satisfy both readers as they stand. Its own fallback was C; whether to take that now
   or build B is the maintainer's ruling, and Muse's half is unmeasured.

   **Answer:**
   > **Measured 2026-10-13, not ruled.** A's pi half is measured and **fails**: pi rejects both the
   > Muse shape and a flat map carrying an extra `providers` key, so one file cannot satisfy both
   > readers as they stand ([the spike, below](#the-dual-parse-spike-measured)). Its storage half is
   > unmet too — the two packs declare different machine-scope `at` dirs, and the base-name join works
   > inside one `at`. A is therefore **not available** without a Pi-side tolerance change or a schema
   > change. Muse's half is **unmeasured** (no Muse login in the jail; launching the CLI was out of
   > scope). [OQ-ULS3](#OQ-ULS3) stays open between B (a derive) and C (two stores) — the maintainer's
   > ruling, not the spike's.

## 9. Decision Ledger

Nothing is decided by this document yet. The one prior ruling it extends is
[`OQ-PR2`](provider-reachability-and-model-names.md#9-decision-ledger): the sharing mechanism is
BUILT and generic, pack-declared only, and adopted per provider rather than per agent.

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| [OQ-PR2](provider-reachability-and-model-names.md#9-decision-ledger) | Sharing an agent-obtained credential across jails is pack-declared only, and providers are adopted incrementally. Extended here to the host notch and to cross-agent sharing. | 2026-10-12 | [§4](#4-how-the-host-reaches-the-store) | machine-share for four packs; host view for Claude only |
