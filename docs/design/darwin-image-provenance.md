---
title: "The macOS nightly has been red for six runs, and neither ruling is wrong"
date: 2026-09-13
status: accepted
tags: [design, ci, macos, image, provenance]
summary: "The nightly macOS integration job failed every run on one chain: a forced image rebuild, a Linux builder the runner cannot start, and a harness that correctly refuses a result from an image it could not verify. Two fixes, a day apart. 2026-09-12 made the image identity a content hash any host can compute, which fixed the harness's oracle and left the nightly red. 2026-09-13 found why: the launcher's nix build was UNCONDITIONAL and compared nothing at all, so there was no identity decision to fix. It now asks the runtime for a stock-tagged image before building."
vantage:
  status-chip: true
---

# The macOS nightly has been red for six runs, and neither ruling is wrong

**Status:** BUILT in two parts — 2026-09-12 ([OQ-IP1](#OQ-IP1)) and 2026-09-13
([OQ-IP4](#OQ-IP4)). Resolution **A** shipped and did NOT turn the nightly green:
[§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060) is the correction, measured on run
`34753694060`. Evidence verified against the tree and against runs `34753694060`, `34687723913`,
`34590383317`, `34467461758`.

> [!WARNING]
> **The chain below mis-attributes L2, and that error cost a day.** L1→L2 reads as "the identity
> varies, *therefore* every launch demands a rebuild". There was no *therefore*: the launcher
> compared no identity, no store path and no sentinel — it simply built, first thing, every time.
> Fixing L1 fixed the harness's oracle and left L2 untouched.
> [§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060) has the measurement.

> **In short.** This is not an infrastructure flake. It is two correct safety rulings
> colliding over a third fact neither of them knows: the image the job is testing
> **was built from this very commit**, and no darwin host could prove it. Now one can.

**Why it matters.** The nightly is the only automated instrument pointed at macOS, and it has produced no signal since at least 2026-09-09 — six consecutive scheduled runs, one identical cause. Every macOS design in the tree is stamped NOT MEASURED, and this is the machine that was supposed to change that.

**The shape.** A four-link chain — arch-varying identity → forced rebuild → absent Linux builder → harness refusal. L1 and L2 turned out to be INDEPENDENT, and each needed its own break ([§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060)).

**Cost.** The fix changes what `imageIdentity` *is*, and that value is compared by a shipped test and baked into every image. Existing images stop matching once, on the commit that lands it — ruled acceptable ([OQ-IP3](#OQ-IP3)).

**Start at [§3](#3-the-chain-and-the-one-link-worth-breaking)** — the chain. Which link you break is the whole decision.

**Ruled:** [OQ-IP1](#OQ-IP1), [OQ-IP2](#OQ-IP2), [OQ-IP3](#OQ-IP3), [OQ-IP4](#OQ-IP4) — see the [Decision Ledger](#decision-ledger).

**Reads with:** [`image-staging-vs-baking.md`](../reference/image-staging-vs-baking.md) (what the image must bake, and the rebuild cost model), [`macos-user-provisioning.md`](macos-user-provisioning.md) (the designs whose claims this instrument was meant to measure).

---

## 1. Verdict

**Break the first link: make the image's identity content-addressed rather than store-path-addressed.** Then a darwin host can vouch for a Linux-built image by inspection, no launch demands a rebuild it cannot perform, the Linux builder stops being on the critical path, and **no safety ruling is weakened to get there.**

> [!IMPORTANT]
> **Half of that was wrong, and the measurement is [§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060).** A darwin host CAN now vouch for a Linux-built image — that part shipped and holds. But "no launch demands a rebuild it cannot perform" did not follow, because the launch demanded one for a reason that had nothing to do with the identity: it built before asking anything. Breaking L1 was necessary and it was not sufficient. The second break is [`OQ-IP4`](#OQ-IP4), and it needs L1's invariant to work — so the order was right even though the causal claim was not.

The two rulings that currently collide are both correct and both should survive:

- **P1 — A failed image build must fail as itself.** A run may end up on an image it could not rebuild, but it must never *look* successful while silently stale (`internal/image/buildfailure.go`, which exists because a failed build once presented as a lib-farm assertion two layers from its cause).
- **P2 — A stale image is never a legitimate basis for an integration result.** `YOLO_ALLOW_STALE_IMAGE` is a legitimate choice for a human at a terminal and never for a test, so the harness fails on the report either way (`integration/imagebuildfailure_test.go`, stated in its own header comment).

Every fix that widens a hatch weakens P2. Every fix that suppresses the build weakens P1. The content-addressing fix weakens neither, because it makes the *question* answerable instead of making a wrong answer tolerable.

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in |
| :--- | :--- | :--- | :--- |
| **OQ-IP1** | **A REQUIREMENT.** *"An identity a second host cannot compute is not an identity; it is a local cache key wearing one."* **BUILT 2026-09-12**: `imageIdentity` is a `sha256:` over `flake.nix` + `flake.lock`, computed with `builtins.hashFile` and declared **outside `eachDefaultSystem`**, so neither `system` nor `pkgs` is in scope to leak in. The image carries it as the CONTENTS of `/etc/yolo-jail-image-identity` (read with `cat`, not `readlink`) and as the `org.yolo-jail.image-identity` label; the oracle is `nix eval --raw .#imageIdentity`, still an eval and now ~0.1s because nixpkgs is never touched. **The darwin downgrade in `integration/imageskew_test.go` is deleted**, which was the point. The guard against a relapse is `TestImageIdentityIsSystemInvariant`, which evaluates the identity under all four default systems and requires agreement | 2026-09-12 | [§4](#4-the-three-candidate-resolutions), [`OQ-IP1`](#OQ-IP1) |
| **OQ-IP2** | **FILED SEPARATELY, not coupled.** Only A is on the critical path, and coupling would keep the nightly red until both land. Nothing in this work touches `containerbuilder`, the builder image, or the nightly's `podman machine` setup. ⚠ **A mac that cannot offload a Linux build still cannot BUILD an image** — this change means such a mac no longer needs to, for a job that was handed one | 2026-09-12 | [§4 row B](#4-the-three-candidate-resolutions) |
| **OQ-IP3** | **ACCEPT THE ONE-TIME REBUILD.** No dual-spelling window: the comparison knows exactly one spelling. What was added instead is a DIAGNOSTIC — the in-image probe falls back to `readlink`, so an image built before this commit answers with its old store path and the failure message says *"this image predates the identity becoming content-addressed"* rather than leaving a bare failed probe. That recognises the old shape; it never accepts it, and it expires on its own because nothing can produce that shape again | 2026-09-12 | [`identityHint`](#OQ-IP3) (`integration/imageskew_test.go`) |
| **OQ-IP4** | **ASK THE RUNTIME BEFORE BUILDING — a STOCK TAG, not a hatch. BUILT 2026-09-13.** L2 has a cause this doc never named: the build was UNCONDITIONAL ([§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060)). `AutoLoadImage` now asks first — `nix eval --raw .#imageIdentity` (0.49 s measured), then `image inspect <repo>:stock-<64 hex>` — and runs that image without building. The identity is a complete key for the **stock image** (the default `.#ociImage` variant with no `packages:` extras) because flake.nix + flake.lock are that image's entire input set; it is deliberately invariant across the lean/minimal variants and every `packages:` list, so the check is keyed on a TAG only stock-aware code writes, never on the label. Every miss — wrong identity, lean attr, any `packages:` entry, an ambient `YOLO_EXTRA_PACKAGES`, an unanswerable `nix eval` — falls through to the build that used to happen unconditionally. **`YOLO_ALLOW_STALE_IMAGE` is removed from the nightly**: with no build there is nothing to allow past, and the hatch never stopped the build in the first place | 2026-09-13 | [§3.1](#31-l2-was-not-caused-by-l1-measured-on-run-34753694060), [`OQ-IP4`](#OQ-IP4) |

## 2. What is actually failing

Measured on run `34687723913` and confirmed identical on the two runs before it:

```
| cannot build on 'ssh-ng://root@127.0.0.1:31022': error: failed to start SSH connection
| Failed to find a machine for remote build!
    IMAGE BUILD FAILED — the jail image was NOT rebuilt from this source tree.
        cgroup_test.go:78: THE JAIL IMAGE BUILD FAILED — this test never ran
        against the image it asked for.
```

The job's own steps tell the story: `✓ Download jail image`, `✓ Load jail image`, then `X Integration tests`. **The image arrives fine and is then rejected.**

> [!NOTE]
> **The workflow already tried to fix this, and its fix is defeated by P2.** `.github/workflows/nightly-macos.yml` sets `YOLO_ALLOW_STALE_IMAGE: "1"` with a comment explaining the reasoning — *"this job asserts its own state: the loaded image IS this commit's."* That reasoning is correct. But `image.BuildFailedMarker` is printed on **both** report branches — refusing and continuing — and the harness matches the marker, not the outcome. So the hatch lets the launch proceed and the harness fails the test anyway.

## 3. The chain, and the one link worth breaking

```mermaid
flowchart TD
  A["<b>L1</b> imageIdentity is a runCommand<br/>→ store path varies by system"] --> B["<b>L2</b> darwin eval ≠ loaded image's path<br/>→ every launch demands a rebuild"]
  B --> C["<b>L3</b> rebuild is an x86_64-linux derivation<br/>→ offloads to a Linux builder"]
  C --> D["<b>L4</b> builder unreachable → build fails<br/>→ harness refuses the result (P2)"]
  A -.->|"break here"| E["darwin can vouch for a Linux image;<br/>L2–L4 never occur"]
```

**L1 is the root and it is a two-line fact.** `imageIdentity` is declared in `flake.nix` as:

```nix
imageIdentity = pkgs.runCommand "yolo-jail-image-identity" { } ''
  mkdir -p $out/etc
  cp ${./flake.nix} $out/flake.nix
  cp ${./flake.lock} $out/flake.lock
  ln -s $out $out/etc/yolo-jail-image-identity
'';
```

> [!NOTE]
> **That is the DEFECT, not the current code.** Since 2026-09-12 `imageIdentity` is a `sha256:`
> string built with `builtins.hashFile`, declared outside `eachDefaultSystem` — see
> [`OQ-IP1`](#OQ-IP1)'s answer. Measured here on Linux the same day, the block above evaluated to
> **three different store paths** for `x86_64-linux`, `aarch64-linux` and `aarch64-darwin` with
> byte-identical content; the replacement evaluates to **one value for all four** default systems.

Its **content** is two copied files and is identical on every system. Its **store path** is a `runCommand` output, so it carries the builder's `system` and differs between `x86_64-linux` and any darwin. The comment above it claims invariance across the full/minimal variants, across `packages:` lib-farm images, and across every Go change — all true, and all about *inputs*. Nobody wrote down that it is **not** invariant across the host doing the evaluating, which is the axis this job lives on.

`integration/imageskew_test.go` already knows, and says so when it downgrades itself on darwin: *"on darwin the image may have been built on a Linux runner, whose imageIdentity legitimately differs from a local eval."* **That downgrade is the existing workaround for L1, applied at one of the two places that needs it.** The launcher's own rebuild decision never got the same treatment.

### 3.1 L2 was not caused by L1 (measured on run `34753694060`)

The sentence above — *"the launcher's own rebuild decision never got the same treatment"* — is the
error in this document, and it is the wrong kind: **the launcher had no rebuild decision to treat.**

Run `34753694060` (2026-09-13, the first nightly after [OQ-IP1](#OQ-IP1) shipped) failed on all four
shards with the identical chain, and the same logs carry the proof that link 1 is fixed:

```
imageskew_test.go:385: source tree wants sha256:816a3ee9…04fb20; yolo-jail:latest has sha256:816a3ee9…04fb20
[integration] yolo-jail:latest matches this source tree (sha256:816a3ee9…04fb20)
```

The darwin host and the ubuntu runner agree on the identity, exactly as [`OQ-IP1`](#OQ-IP1) promised — and every
launch still built. The cause, read off the code rather than inferred:

| Claim | Evidence |
| :--- | :--- |
| `AutoLoadImage`'s first act was `BuildStorePath`, gated only by `SkipBuild` | `internal/image/autoload.go`, the `if !o.SkipBuild` block — nothing is probed before it |
| The run path hardcodes `SkipBuild: false` and calls it "a dormant seam" | `internal/cli/run/imageload.go`, `autoLoadImage` |
| The build is how the content ref is computed, so the presence probe *cannot* come first | `contentRef := JailImageRef(o.Runtime, currentPath)`, after the build |
| `YOLO_ALLOW_STALE_IMAGE=1` WAS reached and did work | the job's own output: `YOLO_ALLOW_STALE_IMAGE is set — CONTINUING ON A STALE IMAGE.` then `Using existing localhost/yolo-jail:latest image.` |
| …and the harness failed anyway, as designed (P2) | `image.BuildFailedMarker` is printed on both report branches; `failIfImageBuildFailed` matches the marker, not the outcome |

So the hatch was never the problem and neither was the skew check. **L2's cause is that the build
was unconditional**, and it would have been unconditional whatever the identity said.

Two facts about the darwin half are worth stating plainly, because they make this a capability
gap rather than a CI quirk: the image closure contains derivations **no public cache serves**
(`nix-ld` is an `overrideAttrs` of nixpkgs', and this project's own cachix is unconfigured —
`publish.yml` is a no-op without `CACHIX_AUTH_TOKEN`), so a darwin `nix build .#ociImage` must
build an `x86_64-linux` derivation *every time the image is not already in the local store*. Any
Mac without a working Linux builder therefore could not launch a jail at all, nightly or not.

> [!NOTE]
> **The `exit code: 125` beside every failure is a SECOND, unexplained symptom, and this doc does
> not close it.** 125 is podman's code for its own failures, so the container did not start. The
> launch got past the image (`Using existing …`) and past the prefix
> (`Jail binaries: /nix/store/h9wr…-yolo-jail-install-prefix/opt/yolo-jail/bin (built from the
> flake source)` — so `.#installPrefix` builds fine on darwin, as `internal/image/prefix.go`
> says it must), and then died with nothing quoted: the harness caps its report at 60 lines
> measured from the marker, and podman's own words fell past the cap.
> `integration/imagebuildfailure_test.go` now also quotes the END of a truncated run, so the next
> red nightly says what 125 was. Expect this one to survive [OQ-IP4](#OQ-IP4).

## 4. The three candidate resolutions

| # | Approach | Fixes the cause? | Weakens a ruling? | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **A** | Content-address the identity — the oracle becomes a hash of `flake.nix` + `flake.lock`, identical on every system | **Yes — L1** | No | ✅ **BUILT 2026-09-12** |
| **B** | Get the Linux builder running on the macOS runner | No — L3 only | No | Worth doing anyway, but not this |
| **C** | Wire a "CI supplied this image, trust it" signal (the dormant `SkipBuild` seam, or a new env var) | No — L4 only | **Yes, P1 or P2** | Rejected |

**A — content-address the identity.** Replace the store-path comparison with one over the bytes that actually define the image's inputs. A darwin host can then compute the same value a Linux builder did, so the loaded image's identity is *verifiable* rather than merely *asserted*. L2 never fires, so L3 and L4 are unreachable, and both `YOLO_ALLOW_STALE_IMAGE` and the skew check's darwin downgrade can go back to meaning what they say.

**B — fix the builder.** The workflow already runs `podman machine init` + `podman machine start`; the builder container behind `containerbuilder.BuilderHostPort` (31022) is still unreachable from the macOS host. This is a real gap and probably worth closing on its own merits — a mac that cannot offload a Linux build is a mac that cannot build an image at all. But as a fix for *this* it is the wrong link: it makes the job spend 3.3 GB and many minutes rebuilding an image it already downloaded, every shard, every night. **Rejected as the primary fix; kept as its own item.**

**C — a CI-trusts-this-image signal.** Either wire the `SkipBuild` seam (`internal/cli/run/imageload.go` hardcodes it `false` and calls it "a dormant seam") to an env var, or teach the harness to distinguish a continued-stale run from a refused one. The first re-opens exactly the silent-stale-image defect `buildfailure.go` was written to close. The second converts P2 from a rule into a rule-with-an-exception, on the axis where an exception is least safe. **Rejected:** it makes a wrong answer tolerable instead of making the question answerable.

## 5. A separate finding: the nightly does not test `macos-user` at all

Worth stating plainly because it changes what a green nightly would even mean. The job is named `Integration tests (macOS + Podman)` and runs with `YOLO_RUNTIME: podman`. It exercises **the podman backend running on macOS** — the container path — and never the `macos-user` backend.

So the roadmap's *"every runtime claim in both macos-user designs is NOT MEASURED"* is not merely a backlog item awaiting a green nightly. **The instrument that exists points at a different backend.** Fixing this doc's chain gets the container-on-macOS signal back; it gets `macos-user` nothing, and nothing in CI currently would.

## 6. What this does NOT propose

- **Not** removing `YOLO_ALLOW_STALE_IMAGE`. It is correct for the case it was built for — an offline or disk-starved machine with a good cached image — and this design stops that hatch from being mis-recruited into CI, which is the opposite of deleting it.
- **Not** relaxing the harness (P2). The fix's whole point is that the harness never has to see a failed build.
- **Not** building `macos-user` CI coverage. [§5](#5-a-separate-finding-the-nightly-does-not-test-macos-user-at-all) names that gap; sizing it is separate work and it does not block anything here.
- **Not** changing what the image contains. `imageIdentity`'s *inputs* are right; only its addressing is wrong for a cross-system reader.

## 7. Open Questions

1. ✅ <a id="OQ-IP1"></a>**OQ-IP1: Is the identity's cross-system invariance a requirement or an accident?** — **RESOLVED (2026-09-12), BUILT** Resolution A rests on a claim the tree has never stated: that two hosts of different systems evaluating the same `flake.nix` + `flake.lock` *should* agree on the image's identity. Today they provably do not, and one of the two consumers papers over it with a darwin downgrade. Making it a stated invariant is what licenses deleting that downgrade — and it is a promise about every future reader of the oracle, not just this job.

   <!-- vantage: oq id=OQ-IP1 leaning="Yes, a requirement. An identity a second host cannot compute is not an identity; it is a local cache key wearing one." -->

   _Leaning:_ **A requirement.** An identity that only its own builder can compute is not an identity — it is a local cache key wearing one. Stating the invariant also makes the existing darwin downgrade explicable as a workaround with an expiry rather than a permanent carve-out.

   **Answer:**
   > **A requirement.** *"An identity a second host cannot compute is not an identity; it is a local cache key wearing one."*
   >
   > **Built as A.** `imageIdentity` is now `"sha256:" + builtins.hashString "sha256" (…hashFile flake.nix… hashFile flake.lock…)`, declared in the outputs' top-level `let` — **outside `eachDefaultSystem`**, where neither `system` nor `pkgs` is in scope. That is what makes the invariant structural rather than a promise: there is nothing per-host left to reach for.
   >
   > Three consequences the build had to settle. **(1) A store path could not be kept.** Every store path that can hold a directory is a derivation output, and every derivation output carries `system`; a fixed-output derivation escapes that but needs the NAR hash of a directory nix cannot compute before building it, so an FOD here is either a build or a hardcoded lie. The identity had to become the content itself. **(2) The image carries the value, not a path to it.** `mkOciImage`'s `postBuild` writes `/etc/yolo-jail-image-identity` as a plain file beside `/etc/passwd`, so it is image content no `symlinkJoin`, tier change or closure can indirect — the read-back is `cat`, and `imageIdentity` left `corePackages` entirely. **(3) The eval got cheaper, not dearer.** `nix eval --raw .#imageIdentity` touches no nixpkgs at all: 0.096 s measured, against 0.395 s for the store-path oracle it replaces, so the constraint that the skew check must never become a build is met with room.
   >
   > **The darwin downgrade is deleted** (`effectiveSkewMode` and its test are gone) — the ruling's whole point, and the reason for stating the invariance as a requirement rather than noting it as a convenience.

2. ✅ <a id="OQ-IP2"></a>**OQ-IP2: Does the Linux builder on macOS get fixed too, or only filed?** — **RESOLVED (2026-09-12)** Resolution B is not the fix for this failure, but a mac that cannot offload a Linux build cannot build an image at all — which is a real capability gap for any developer on that platform, independent of CI. The question is whether it rides along with this work or becomes its own thread. It decides whether the nightly's recovery depends on one change or two.

   <!-- vantage: oq id=OQ-IP2 leaning="File it separately. Coupling them means the nightly stays red until both land, and only one of them is on the critical path." -->

   _Leaning:_ **File it separately.** Coupling them keeps the nightly red until both land, and only A is on the critical path. B is also the harder one to verify from here — this jail cannot reproduce a macOS runner's podman machine.

   **Answer:**
   > **File it separately.** Coupling them means the nightly stays red until both land, and only one of them is on the critical path. This work touches no builder code, no `containerbuilder`, and nothing in the nightly's `podman machine` setup.
   >
   > It is still a real gap, and this change narrows rather than closes it: a mac that cannot offload a Linux build still cannot **build** an image — it can now **verify** one it was handed, which is all the nightly ever needed.

3. ✅ <a id="OQ-IP3"></a>**OQ-IP3: What happens to the images already out there?** — **RESOLVED (2026-09-12)** Changing the oracle moves every image's recorded identity exactly once, on the commit that lands it. Every currently-loaded image then mismatches and every launch demands one rebuild. That is the correct behavior for a genuine input change and a needless 3.3 GB for a change that alters no image content. The options are to accept the one-time rebuild, or to have the comparison accept either spelling for one release.

   <!-- vantage: oq id=OQ-IP3 leaning="Accept the one-time rebuild. A dual-spelling window is a second code path guarding a cost users pay once." -->

   _Leaning:_ **Accept the one-time rebuild.** A compatibility window is a second code path that exists to save a cost paid once, and this repo's standing preference is to delete the second path rather than maintain it.

   **Answer:**
   > **Accept the one-time rebuild.** A dual-spelling window is a second code path guarding a cost users pay once, and this repo's standing preference is to delete the second path rather than maintain it. The comparison therefore knows exactly one spelling.
   >
   > **What was added instead is a diagnostic, and the distinction is the whole of it.** An image built before this commit has `/etc/yolo-jail-image-identity` as a symlink to a directory, so `cat` fails on it — which would have surfaced as a *failed probe*, and a failed probe is reported as a degraded harness and the check is SKIPPED. That is the wrong answer on the one commit where every image mismatches. So the in-image read falls back to `readlink`, the old store path comes back as a plain string, it is rejected like any other non-identity, and `identityHint` names it: *"That is a STORE PATH, not an identity. This image predates the identity becoming content-addressed (2026-09-12)… EVERY image built before that commit mismatches exactly once, and the rebuild below is the whole fix."*
   >
   > Measured 2026-09-12 against a real pre-cutover image in this jail's podman: the probe returns `/nix/store/8r4ypm7z9qxmxvfhba7nxhkyhxm2qkzn-yolo-jail-image-identity`, which is exactly the shape the hint fires on. It recognises the old spelling and never accepts it, and it expires on its own — nothing can produce that shape again.

4. ✅ <a id="OQ-IP4"></a>**OQ-IP4: What actually demands the rebuild, once the identity agrees?** — **RESOLVED (2026-09-13), BUILT** Run `34753694060` is the counter-example to this doc's own chain: both sides of the identity agree, and every launch still built. The question the resolution had to answer is not "how does the launcher learn the image is current" but "why does the launcher not ask" — and the answer is that it had nowhere to ask *from*, because the store path it would have compared is the build's output.

   <!-- vantage: oq id=OQ-IP4 leaning="Nothing about identity. The build is unconditional, and the launcher has to be given a question it can answer before building." -->

   _Leaning:_ **Nothing about identity.** The build is unconditional and always was. Any fix has to give the launcher a question it can answer *before* the build, out of values that survive crossing hosts.

   **Answer:**
   > **A stock tag, written only by code that knows it is looking at a stock image.**
   >
   > **The term.** *Stock image* — the jail image a flake describes on its own: the default `.#ociImage` variant, no `packages:` extras. Coined in `internal/image/stockimage.go`, where the rest of this lives.
   >
   > **Why the identity is the right key here and nowhere else.** flake.nix + flake.lock are the stock image's entire input set, which is exactly what `imageIdentity` hashes — so two stock images with the same identity are the same image, whichever host built them. It is also *deliberately* invariant across the full/minimal/lean trio and across every `packages:` list (flake.nix says so where it declares the label). A matching identity on an arbitrary image therefore proves nothing about the variant, which is why the check is keyed on a **tag**: only `AutoLoadImage` after delivering a default-attr, no-extras image, and the nightly's `Load jail image` step after loading what `nix build .#ociImage` produced, ever write it. Accepting a lean image for a stock launch would be `buildfailure.go`'s own defect in a new costume.
   >
   > **Not resolution C.** C was rejected as *"a CI-trusts-this-image signal"* — an assertion that suppresses a check. Nothing is suppressed: the tag carries a claim (*this image's identity is X*), and the launch VERIFIES it against its own `nix eval` of its own checkout. An image from another commit carries that commit's identity and matches nothing. There is no new environment variable, and one existing one is removed.
   >
   > **`YOLO_ALLOW_STALE_IMAGE` leaves the nightly**, per this repo's escape-hatch rule (hatches are for broken user config, never for yolo bugs). It never stopped the build; it let a FAILED one proceed, and the harness fails on the report either way — so it bought nothing and hid that a build was running at all.
   >
   > **What a match gives up, stated rather than discovered.** A stock-matched launch built nothing and has no store path: `LoadResult.StorePath` is empty, no GC root is registered, no load-sentinel entry is appended. All of that is cache bookkeeping ([`the-load-sentinel-is-not-a-liveness-oracle.md`](./the-load-sentinel-is-not-a-liveness-oracle.md) [§4](./the-load-sentinel-is-not-a-liveness-oracle.md#4-two-consumers-two-different-questions), Consumer A), and the workspace's current-image pointer keeps naming the store path the launch that first loaded this image recorded. One sentence elsewhere goes stale by it: `internal/prune/imageroots.go` justifies its age cutoff with *"AutoLoadImage re-registers the root on every success rather than only on a load"*, which is now true only of launches that built.
   >
   > **Measured, and the boundary of what was measured.** In this jail: `nix eval --impure --raw .#imageIdentity` → `sha256:816a3ee9…04fb20` in 0.49 s, equal to what run `34753694060`'s darwin runner computed; the loaded image's `org.yolo-jail.image-identity` label carries the same value; `podman tag` accepts the 70-character stock tag and `podman image inspect` answers rc 0 on it. The decision itself is pinned on Linux by ten unit tests driving the real `AutoLoadImage`, and deleting either call site was measured to fail exactly the two tests that pin them. **What is NOT measured is the macOS runner.** Nobody here has a Mac, a nested jail is blind to this by construction, and the next scheduled nightly is the only instrument that can report it green.

## 8. What the roadmap needs

Another agent holds [`roadmap.md`](../plans/roadmap.md) as this is written, so this is the hand-off rather than the edit:

- A **💬 row** for this doc, carrying [OQ-IP1](#OQ-IP1)–[OQ-IP3](#OQ-IP3), noting that the macOS nightly has been red since at least 2026-09-09 on one cause.
- A **separate row** for the Linux-builder-on-macOS gap ([OQ-IP2](#OQ-IP2)), which is not blocked by this doc.
- An amendment wherever the roadmap says macOS claims are unmeasured **pending CI**: per [§5](#5-a-separate-finding-the-nightly-does-not-test-macos-user-at-all), no CI job currently exercises `macos-user` at all, so a green nightly would not move those claims.
