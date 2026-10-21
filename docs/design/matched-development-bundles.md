---
title: "Build a matched launcher bundle without changing the install"
status: accepted
stage: DECIDED
next: "Implement the completed plan's paired build, source selection, and no-launch-build tests"
tags: [development, builds, launch]
summary: "A build-only development artifact pairs the host launcher with every helper and flake it will run."
---

# Build a matched launcher bundle without changing the install

**Status:** 2026-10-21. Owner direction accepted and grounded against `e355367`; implementation remains unbuilt.

> **In short.** A launcher and the jail helpers it starts are one build artifact. Developers need a build-only path that produces that pair from one source snapshot and can be invoked directly, without changing the installed launcher, `PATH`, or the source selected by any other launcher.

**Why it matters.** A checkout without prebuilt helpers can take a launch path that invokes Nix to compile them. On a Mac Podman Machine that cannot see the selected `/nix/store` path, this source-visible fallback can end in an unreachable-prefix refusal unless the store is shared. That is the observed code limitation; it does not establish the cause of any particular reported command or failure.

**The shape.** A shared build-and-package pipeline emits immutable, executable-relative bundles; launchers discover only their neighboring bundle, and launches never compile helper binaries.

**Cost.** `YOLO_REPO_ROOT` ceases to select launch sources. CI, update checks, configuration guidance, and the integration harness must use the matched artifact or installed bundle instead.

**Start at [§2](#2-the-matched-artifact-contract)** — every later behavior follows from it.

**Needs your ruling:** None; owner rulings recorded below.

**Reads with:** [`matched-development-bundles-plan.md`](matched-development-bundles-plan.md) (completed implementation map and gates), [`image-staging-vs-baking.md`](../reference/image-staging-vs-baking.md) (the existing image and mounted-prefix contract).

---

## 1. Current behavior and the gap

[`reporoot.go`](../../internal/reporoot/reporoot.go) currently resolves `YOLO_REPO_ROOT` first, then a bundle beside the running executable, then the installed state-directory bundle. It deliberately ignores the working directory. A live checkout usually has no prebuilt helper directories.

[`jailprefix.go`](../../internal/cli/run/jailprefix.go) mounts prebuilt Linux helpers when the selected flake has them; otherwise it calls [`BuildJailPrefix`](../../internal/image/prefix.go), which builds `.#installPrefix` in the Nix store during launch. On macOS Podman, [`prefixUnreachableFromVM`](../../internal/cli/run/jailprefix.go) refuses store-backed mounts unless `/nix` was explicitly shared. This is independent of the normal Nix image build and load.

The macos-user backend has the analogous fallback: [`resolveGuestBinaries`](../../internal/cli/commands.go) calls `.#guestPrefix` when the selected flake lacks its Darwin helper set. The successful path stages executable files into the backend's scoped guest-bin directory with sealed permissions; it does not install them into the account or workspace.

[`just install`](../../Justfile) currently installs the host launcher first, stages Linux helpers (and Darwin guest helpers on macOS) into a fresh flake generation, then activates that generation. [`stage-source-bundle.sh`](../../scripts/stage-source-bundle.sh) packages `flake.nix`, `flake.lock`, a build stamp and the selected helper binaries—not a source checkout. This machinery already supplies the distribution bundle format, but independently reading a mutable checkout in separate build steps does not guarantee the host launcher and helper set came from one source snapshot.

## 2. The matched-artifact contract

A **matched bundle** is a self-contained development output containing a native host `yolo` executable and the flake plus all helper binaries that this host's supported jail backends can invoke. The build stamps and every byte-producing step come from one frozen snapshot of the intended source files. It may be built from a dirty checkout: the frozen snapshot captures tracked edits and intended non-ignored new source, but excludes `.git`, ignored output, credentials, unrelated host files, and symlinks whose targets escape the snapshot.

The snapshot is described by a bounded input manifest, not a per-file hash inventory: the flake's Go source inputs (`go.mod`, optional `go.sum`, `vendor/`, `cmd/`, `internal/`, and `packs/`), `flake.nix`, `flake.lock`, and the build/package entry points and their required inputs (`Justfile`, `scripts/build-go.sh`, and `scripts/stage-source-bundle.sh`). For each declared source root, include tracked files with their worktree contents and intended untracked, non-ignored source files; do not recursively copy every file beneath a directory merely because it is in the flake fileset. Exclude ignored files and external symlink targets; if a required input is an excluded symlink, fail before building rather than read outside the snapshot. Freeze this bounded file set first, then execute the copied build scripts and flake operations from that snapshot, never from the live checkout. Compute one snapshot identity from the sorted paths and contents and embed it in the launcher and bundle metadata; commit/version strings alone cannot distinguish dirty builds from the same commit.

The bundle is a distinct, immutable output for each build. It is not an activation of the installed bundle, a mutation of a running jail's mount source, or a symlink to the live checkout. A failed build publishes no usable launcher. Concurrent builds reserve different output generations; neither overwrites a bundle another process may be running. The owner of a development output removes it explicitly when finished; this design adds no age-based reaper or retention policy.

### Platform contents

| Build host and backend | Host launcher | Target helpers in the bundle |
| :--- | :--- | :--- |
| Linux | Native Linux `yolo` | `bin/linux-<host-arch>` shipped helper set |
| macOS with container backend | Native Darwin `yolo` | `bin/linux-<host-arch>` container helper set |
| macOS with macos-user enabled | Native Darwin `yolo` | Linux helper set plus `bin/darwin-<host-arch>` guest set |
| Release/distribution build | Existing distribution launcher layout | Existing declared OS/architecture sets; no local-only set or second ship list |

The executable and `share/yolo-jail` use the existing executable-relative discovery shape: `bin/yolo` beside `share/yolo-jail/`, with the flake and architecture-keyed helpers inside the latter. A developer runs that exact launcher by path. No PATH change, active-bundle swap, configuration knob, or checkout-path override selects the artifact.

### Launch selection and failure

A complete executable-relative bundle is authoritative for its launcher, regardless of whether it came from Homebrew, a release archive, a mounted image prefix, a system package, a sealed layout, or a development build. When a recognized adjacent bundle directory exists but is incomplete, refuse; do not hide the broken pair by falling back to installed state. A binary-only launcher with no adjacent bundle may use installed state only when it is an installed launcher carrying an embedded installation identity: resolve the active generation to one canonical absolute directory once, compare its bundle identity with the launcher's embedded identity, and use that pinned directory for the flake and every helper. A mismatch or unmarked binary-only layout refuses with the build/install remedy. Thus a later activation cannot switch generations between helper and flake resolution, and same-commit dirty builds remain distinguishable by their snapshot identity.

`YOLO_REPO_ROOT` is retired as a launcher source selector, not given compatibility precedence. Every production, test, CI and diagnostic consumer must move to the right replacement: invoke a matched bundle by its executable path for development; use the complete installed/distribution bundle for normal launches; and pass source directories only to build/package operations that explicitly compile from source. Inherited `YOLO_REPO_ROOT` must not silently pair one launcher with another tree's helpers. A refusal should name the obsolete variable and offer the build-only command or `just install`, whichever fixes the context.

Development artifacts also carry an explicit development classification in build metadata, distinct from an installed/release channel. The updater must recognize it before archive classification and suppress automatic network checks, disclosure/notices, and update prompts for that artifact; explicit update commands retain their existing behavior. Omitting install source stamps alone is insufficient because a complete adjacent bundle otherwise looks like an archive.

### Backend delivery

- **Container backends:** mount the selected prebuilt helper directory and flake bundle read-only, following the current mounted-prefix contract. Preserve the existing image realization, image load, and `packages:` paths. A helper bundle is not an image derivation and does not remove the ordinary Nix image build.
- **macos-user:** select the bundled Darwin guest set and stage it into a new immutable guest-bin child under the existing scoped staging root, keyed by bundle identity plus a unique launch/session token. Each running session's staged PATH names only its own child; never replace or remove those executables while that session runs. Build the child privately, seal its files and directory, then publish it with fresh-inode atomic rename. Remove it only after its session exits. Keep the existing Seatbelt rules, root, host grants, account home and workspace boundary unchanged; do not add a new writable location or broaden access. A launch of bundle B in the same workspace must not change bundle A's guest executables or PATH.
- **Default macOS Podman Machine:** place the development output in a user-home location the default VM shares, not `/nix/store`, the checkout, or a VM-only path. The user needs no `/nix` share, machine recreation, or installed-binary replacement for the development loop. The Nix image build/load and image-identity mechanisms remain unchanged.

## 3. Build and use sequence

The build-only counterpart to `just install` shares the same source snapshot, stamp calculation, binary ship sets, cross-compilation, completeness checks and bundle assembly. It differs only at publication: it prints the generated launcher's full path and neither installs it nor activates a state-directory bundle. A proposed command is:

```bash
just build-dev-bundle
# Run the exact path printed by the build:
~/.local/share/yolo-jail/dev-bundles/<generation>/bin/yolo -- bash
```

The ordinary workflow remains: an installed launcher starts the outer jail; an agent edits the live checkout, builds a matched development bundle, then explicitly invokes that bundle's launcher for nested tests. The checkout itself is never selected by an older installed launcher. `yolo -- bash` against the installed launcher continues to use the installed generation.

Build/package publication happens only after every required binary and flake file is present and validated. For `just install`, prepare the complete matching generation before making it active and install/activate the corresponding launcher-bundle pair with a verifiable identity. Resolve the active installed symlink once to its canonical generation and pin that root for the full launch; no helper or flake consumer resolves the mutable active link again. A launch that races publication must see one complete old pair or one complete new pair, or refuse clearly; it must never mutate bytes mounted by an existing jail. This pairing check also makes a partially failed install detectable on the next invocation.

## 4. Alternatives and costs

| Choice | Verdict |
| :--- | :--- |
| Keep compiling `.#installPrefix` or `.#guestPrefix` during launch | Rejected. It leaves launch behavior dependent on a Go/Nix build and preserves the inaccessible-store failure on default macOS Podman. |
| Add a wrapper that builds a Nix helper derivation | Rejected. It duplicates the Go/package path and keeps compilation in launch orchestration. `scripts/build-go.sh` remains the helper compiler. |
| Add a dev environment variable selecting a checkout or bundle | Rejected. An override can select a flake whose helper binaries do not match the running launcher. The explicit launcher path is the selection. |
| Copy the checkout into a jail or use it as the mounted flake | Rejected. It exposes ignored/local files and allows the live source to move independently of mounted binaries. |
| Replace the image/load or package delivery flow | Out of scope. The helper bundle is separate from the Nix image and `packages:` contracts. |

The cost is a breaking change for scripts that rely on `YOLO_REPO_ROOT` selecting runtime source, and a locally built development artifact occupies user-state storage until its owner removes it. In exchange, installed operation remains unchanged and a development invocation states its exact launcher path.

## 5. Acceptance

The design is built when all of the following hold:

1. One build-only command emits a complete, self-contained launcher bundle from clean or dirty source without changing PATH, the installed launcher, or the active installed bundle.
2. The native launcher and every Linux/Darwin helper set it may start are built from the same immutable source snapshot and stamp; the artifact's executable-relative discovery selects that bundle ahead of any installed generation.
3. A missing helper never starts a launch-time Go/Nix helper build. It refuses with a next step naming the build-only command or install path.
4. The complete artifact works when invoked by path, including nested container launches on a default Mac Podman VM without `/nix` sharing or VM reconfiguration. Container helper mounts remain read-only; macos-user delivery remains scoped and sealed.
5. Two macos-user launches using bundles A and B in the same workspace retain distinct, immutable guest-bin paths for their full lifetimes; ending either session does not alter or remove the other's staged executables, and no Seatbelt root, grant, account, home or workspace boundary expands.
6. A later build cannot change an output a running jail has mounted. Failed and concurrent builds cannot expose a partial or cross-paired generation.
7. `just install`, release packaging, image realization/load, and `packages:` delivery still provide their existing supported behavior using the shared package machinery. The standalone `installPrefix` package remains available for mounted-prefix packaging; removing `guestPrefix` changes the content hash input `imageIdentity` derives from `flake.nix`, but does not change the identity mechanism or image/load path.

## 6. Owner rulings (2026-10-21)

| Ruling | Recorded contract |
| :--- | :--- |
| Remove launch-time helper compilation | Container and macos-user launch paths consume prebuilt target helpers; missing helpers refuse instead of invoking a helper build. |
| Add a build-only counterpart to install | It emits a matched native launcher, target helpers and flake bundle from one source snapshot, including dirty development edits. |
| Keep installed use and development use separate | The normal installed launcher and PATH remain unchanged; developers invoke the generated launcher explicitly. |
| Make default Mac Podman viable | Deliver the artifact in the VM-shared home path; do not require `/nix` sharing, VM setup, or an installed replacement. |
| Preserve trust and runtime boundaries | No broader credential/home grants, workspace mutation, writable helper injection, alternate Go implementation, or image/load/package redesign. Keep the standalone mounted-prefix `installPrefix` package; `imageIdentity` continues to hash `flake.nix` and `flake.lock`, so its value naturally follows a flake edit without changing that mechanism. |

## 7. Terms

**Matched bundle** *(coined here)* — one immutable build output pairing the host launcher with the helper binaries and flake bundle that launcher selects. Not merely binaries with equal Git commit strings: dirty builds and concurrent edits require one frozen source snapshot.