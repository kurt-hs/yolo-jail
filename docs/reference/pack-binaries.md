---
status: current
verified: 2026-10-16
verified_commit: 8e65f3444
covers:
  - internal/loopholedecl/binaries.go
  - internal/loopholes/binaries.go
  - internal/packbin/
  - internal/cli/packbinaries.go
  - internal/releasematrix/
  - tools/pack-binaries/
  - integration/packbinary_test.go
  - integration/packbinaryseed_test.go
  - internal/selfupdate/apply.go
  - .goreleaser.yaml
  - .github/workflows/release.yml
  - .github/workflows/publish.yml
  - Justfile
tags: [loopholes, packs, binaries, release, reproducible-build]
summary: "How a loophole ships a compiled program: the `binaries` manifest key and its two tokens, the digest-keyed cache that only `yolo pack install` fetches into, the release matrix that builds an official program for the platforms yolo ships to, the pin tool that writes and checks every digest reproducibly, and main's own pin between releases, which `just check-ci` checks and `just install` builds into the cache with no download."
---

# Pack-shipped binaries — the `binaries` key, the cache, and the release matrix

**Status:** Verified 2026-10-16 against `8e65f3444`. UNMEASURED: no shipped pack declares
`binaries` yet, so every gate below runs over nothing in the tree, and the pin tool's tests run
over a fixture checkout. [What is not measured](#what-is-not-measured) lists the rest.

A loophole that runs a compiled program it does not bake declares the program as a
**download**: one build per platform, each with an https `url` and a mandatory `sha256`, in
the loophole's own manifest under `binaries`. `yolo pack install` fetches each build this
machine needs, verifies it, and caches it by digest with its exec bit; a launch runs it from
that cache and never fetches. For an **official** program, one built from this repository's
`cmd/`, yolo's own release builds every build reproducibly, publishes it on the tag's GitHub
release, and commits its digest before the tag. Between two releases main pins its own build,
which `just check-ci` checks and a from-source `just install` builds into the cache.

| Component | Lives in |
| :--- | :--- |
| The `binaries` key, its two tokens, the wrong-half refusals | `internal/loopholedecl` (`binaries.go`, `JailBinaryRoot`) |
| Token resolution, the jail file bind, the inert reasons | `internal/loopholes` (`binaries.go`, `runtime.go`, `inertreport.go`) |
| The digest-keyed cache: download, verify, admit, seed | `internal/packbin` (`Path`, `Present`, `Seed`) |
| The install step and its next-step hints | `internal/cli` (`packbinaries.go`, `fromSourceHint`) |
| The release matrix: platforms, asset name and URL, the import walk | `internal/releasematrix` (`Platforms`, `AssetURL`, `ManifestsTolerant`) |
| The pin tool: `pin`, `check`, `stage`, `seed` | `tools/pack-binaries` (`Toolchain`, the recipe, `effectiveProxy`) |
| The census over every embedded manifest | `internal/loopholedecl` (`TestEveryOfficialBinaryIsOnTheReleaseMatrix`) |
| The release and PyPI gates | `.goreleaser.yaml` (the before-hook, `extra_files`), `.github/workflows/publish.yml` |

**Reads with:** [`loophole-system.md`](loophole-system.md#a-program-the-loophole-downloads),
the loophole-side summary of this mechanism and the manifest it lives in;
[`release-process.md`](release-process.md), the release this hooks into;
[`mise-node-dynamic-linking.md`](mise-node-dynamic-linking.md), how a non-nix program runs in
the jail at all.

---

## Principles

<a id="p4"></a>**P4. A pinned pack pins everything that runs.** A pack's lockfile commit pins the
pack's whole tree. A URL is not in the tree, and a GitHub release asset can be deleted and
re-uploaded, or a tag moved, without changing any commit. A manifest naming a URL with no
digest would turn "pinned pack" into "pinned manifest, unpinned payload", and the unpinned part
would be the executable. A `sha256` written in the manifest restores the property transitively:
the commit pins the manifest, and the manifest pins the bytes.

**Fetch at install, never at launch.** A launch-time fetch would make no network mean no jail,
and would move the moment of trust from installing a pack to every start. A build cached by
digest makes a reinstall of the same pin offline.

**A digest mismatch is an integrity failure, not a fetch failure.** It refuses the install,
names both digests, caches nothing, and never falls back to a cached copy, since the bytes are
the pin.

**Declaring more builds than the release produces is the failure mode to prevent.** It turns
"unsupported here" into "supported, missing": `yolo pack install` fails on a missing file instead
of the loophole reading as unsupported. An official program's build set is therefore derived,
never written by hand ([BP-D7](#BP-D7)), and a census refuses any other.

<a id="a-packs-own-tree-cannot-carry-an-executable"></a>

## A compiled program does not belong in a pack's own tree

The first route anyone reaches for, committing `bin/<goos>-<goarch>/<program>` beside the
manifest, worked at first only for a pack **configured by path**, whose files the stager copies
with their mode. It failed for an **embedded** pack, the shape every official pack has:
`embed.FS` has no exec bit, so an embedded file read back `0444` whatever its mode in the
repository. That was measured with `packs/hello-daemon`, a throwaway pack whose jail daemon is a
two-line `#!/bin/sh` program in its own folder (`internal/packload`'s
`packshippedjailbinary_test.go` pins it). Everything declarative held: `{jail_loophole_dir}`
resolves in `jail_daemon.cmd`, the module directory is mounted read-only without `noexec`, and a
jail-daemon-only manifest is accepted. Only delivery failed.

The exec bit now reaches the jail by both routes: `packs/executables.txt` records it for an
embedded pack ([the exec bit](pack-system.md#the-exec-bit)). A committed **compiled** program is
still the wrong shape: it needs one build per platform, and in an official pack every build rides
in every `yolo` binary. That is what
`binaries` exists for: one verified download per platform, cached with its exec bit.

Two consequences of that experiment are still in force:

- **A spawn failure is reported.** The in-jail supervisor used to discard a `permission denied`
  from starting a daemon and retry silently for the life of the jail, leaving an empty log. It
  now logs the error and obeys the daemon's restart policy.
- **A shipped program is a disclosure exception.** Shipped executables are enumerated as a claim
  but not review-worthy, on the argument that running a script never needs its exec bit. A jail
  daemon does: yolo spawns it by absolute path at boot, so the bit is load-bearing. The blast
  radius is still the jail's.

## The manifest key and its two tokens

`binaries` maps a program name to its builds, one per `<goos>/<goarch>`, each an https `url` and
a `sha256`. Two tokens name a binary in an argv:

- `{binary:<name>}`, legal in `host_daemon.cmd` and `doctor_cmd`, resolves to the cached build
  for this machine's platform;
- `{jail_binary:<name>}`, legal in `jail_daemon.cmd`, resolves to the file the launch binds
  read-only into the jail at its container path, the `linux/<arch>` build.

Each token is refused in the other half and in every other field, must name a declared binary,
and every declared binary must be named by one, since a build nothing runs would be fetched for
nothing ([BP-D2](#BP-D2)). Each reference takes the build for where it runs ([BP-D3](#BP-D3)): a
compiled file runs on one architecture, and the jail is Linux on the machine's architecture
whatever the host OS is. A binary with no build for where it runs makes the loophole unsupported
on the **platform** axis, saying nothing can be installed.

> [!WARNING]
> **A yolo older than the key reads a manifest with `binaries` tolerantly**: it skips the key with
> a skew note and the tokens reach the daemon literally, so the spawn fails naming the token. Only
> a fetched pack can be newer than the binary reading it.

## The cache, and who fills it

The cache is a directory keyed by digest, under yolo's state tree and outside every jail mount,
because a host daemon's build runs from it with the user's authority. Each file keeps its
declared name, so a program reading its own `argv[0]` sees it, and is admitted only by renaming
a verified file into place ([BP-D4](#BP-D4)). A jail receives each build as **one read-only file
bind**, never the directory, so it sees only what its daemon runs.

A download must be https at every hop, redirects included, carry no credentials in its URL, and
stop at a size bound, because the digest can be checked only after the last byte.

**Two things fill the cache, and nothing else does:**

1. **`yolo pack install`** (and `yolo pack update`, which runs it) is the only fetch
   ([BP-D5](#BP-D5)). It walks every selected pack, embedded ones included, and fetches each
   build this machine needs whatever the loophole's switch says, and nothing for a loophole whose
   `platforms` leaves this machine out. A reinstall re-hashes the cached copy and fetches again
   when it no longer matches.
2. **A from-source `just install`** builds this machine's official builds from its tree and
   admits each through `packbin.Seed`, the same verified rename a download takes
   ([BP-D22](#BP-D22)). No program is downloaded. See
   [What main pins between releases](#what-main-pins-between-releases).

**What a missing piece produces**, everywhere a loophole is reported
([BP-D6](#BP-D6)):

| Missing | What happens |
| :--- | :--- |
| No build declared for where a token runs | Unsupported on the platform axis; nothing can be installed |
| A declared build not yet fetched | The loophole is inactive; the launch's inert report, `yolo loopholes list` and `yolo check` name `yolo pack install` |
| A `doctor_cmd` whose build is missing | Not run, with the reason |
| A digest mismatch at install | Refused, both digests named, nothing cached |
| A failed fetch of an official build, on a `yolo` installed from a checkout | The refusal adds `just install` in that checkout ([BP-D24](#BP-D24), [BP-D29](#BP-D29)) |

Inside a jail, a jail build counts once the outer launch mounted it, and a nested launch binds
that mounted copy on when its own cache lacks it. A host build is the host's business in what
the jail reports. A nested launch runs its host daemons in the jail from the jail's own cache, so
it starts none whose build that cache lacks and says so.

On **macos-user**, a host binary runs as anywhere. The guest declines a jail daemon whose argv
names a `{jail_binary:}` path: that path exists only in a container, and the build is a Linux
one. Placing a darwin jail build in the guest is **not built**: it would need a darwin jail build
the release matrix does not make. **Apple Container** needs nothing: every pack loophole is
inert there.

## The release matrix

The **release matrix** *(coined in the design this graduated from)* is what yolo's own release
does for the binaries official packs declare: which platforms it builds each one for, where it
publishes them, and how each digest reaches the manifest the same release embeds. It covers
programs yolo builds. An official pack that pins a program someone else releases declares that
upstream's builds, narrows `platforms` to them, and none of this applies to it.

### Platforms

An official binary has a build for **exactly** the platforms it runs on among those the release
ships `yolo` to ([BP-D7](#BP-D7)):

- a `{binary:}` reference gets one build for each `<goos>/<goarch>` of goreleaser's `yolo` build
  that the loophole's `platforms` admits;
- a `{jail_binary:}` reference gets `linux/<arch>` for each of those architectures;
- nothing gets a darwin jail build.

The set is read from `.goreleaser.yaml`, never copied, so a platform the release drops fails the
census instead of leaving a stale build. The program is pure Go from this module, cgo off, at
`cmd/<name>` for the `binaries` key `<name>`, because the release cross-compiles every platform
on one Linux runner. A program that needs cgo takes an upstream's own release, or the
source-build route [`forked-programs-as-packs.md`](../design/forked-programs-as-packs.md) designs.

An official program is **downloaded, never mounted**: it is exempt from the jail's ship set
beside `goprobe`, and `TestAnOfficialPackBinaryIsDownloadedNeverMounted`
(`internal/entrypoint`) keeps it out of both ship lists. The flake and the bundle compile every
`cmd/` directory and copy only the ship set, so the program costs each one more compile and
reaches no jail that way.

### Publication

Each build is a **bare executable** on the tag's own GitHub release, named
`<name>_<version>_<goos>_<goarch>` and listed in `checksums.txt` ([BP-D8](#BP-D8)). Bare, because
the cache stores the downloaded bytes as the program and has no unpack step. **Every release
uploads every official build, moved or not**, so each version's manifests name files on that
version's release and a release deleted later breaks only its own installs. The asset URL
redirects over https, which the cache's redirect rule requires, and an asset re-uploaded under
the same URL is refused by its digest (P4).

### The digest, and the recipe

Each build's `url` and `sha256` are **committed in the tree the tag names**, written before the
tag ([BP-D9](#BP-D9)). They have to be: the Homebrew formula builds `yolo` from the tag's source
tarball, and goreleaser refuses a dirty tree, so a digest written at release time would give the
archives and Homebrew different manifests. Knowing the bytes before the tag needs a
reproducible build, and every build uses one **recipe**:

- **the toolchain**: Go's own distribution of one pinned version, fetched as the
  `golang.org/toolchain` module and checked against the checksum database, never the `go` on the
  machine's PATH ([BP-D12](#BP-D12));
- **the flags**: cgo off, `-trimpath`, `-buildvcs=false`, `-mod=vendor`, and no `-ldflags`, so
  nothing is stamped;
- **the environment**: an allowlist. Every `GO*` and `CGO_*` variable is dropped except the four
  that name a cache or scratch directory, and the go env file is off; the platform and Go's
  default architecture levels are set explicitly ([BP-D13](#BP-D13)).

Each item removes a measured cause of drift: the VCS stamp embeds the commit that holds the
digest, so no build could match it; a version stamp would move every digest on every release;
the toolchain's version is in the bytes; an architecture level changes the code emitted. The
checkout's path and the build cache were measured not to move the bytes, across all four
platforms.

**A program that links the packs embed is refused**, naming the import: it would carry its own
digest, and no build of it could match. The check is a static walk of this module's imports
under each platform's build constraints, compiling nothing ([BP-D14](#BP-D14)).

### The pin tool and the three gates

The **pin tool** *(coined in the design this graduated from: the one program that builds every
official build, writes its `url` and `sha256`, and checks them)* is `tools/pack-binaries`. It
shares the matrix with the census through `internal/releasematrix`, because a test cannot import
a `main` package and the two must not disagree ([BP-D10](#BP-D10)). It lists the manifests the
packs **embed** carries, compiled from the checkout it runs in, so an untracked pack on disk
changes nothing ([BP-D25](#BP-D25)). Its verbs:

| Verb | What it does |
| :--- | :--- |
| `pin [<version>]` | Writes each build's values in place, editing two string values so the file's comments survive; never adds a key ([BP-D11](#BP-D11)). With a version it writes `url` and `sha256`; with none, only the digests ([BP-D21](#BP-D21)) |
| `check [<version>]` | Builds and compares, writes nothing, refuses every disagreement: a digest (naming binary, platform and both digests), a URL of the wrong form, a platform outside the matrix or missing from it, a program linking the embed, a key with no `cmd/<name>`. With no version, the digests alone |
| `stage <version> <dir>` | Runs `check`, then writes each verified build into an empty directory for the release to upload |
| `seed [--repin] [<dir>]` | Builds this machine's builds and admits each whose digest is the pin into the cache ([BP-D22](#BP-D22)) |

With no official binary declared, each verb says so and succeeds without fetching the toolchain.

**Three gates rebuild and compare**, the changelog gate's shape: the same check refuses locally
before the tag and again in CI.

1. `just release` runs `check <version>` after its changelog gate, so a release goes: rename the
   changelog section, `just pin-pack-binaries <version>`, commit both, `just release`.
2. The release's goreleaser before-hook runs `stage`; goreleaser uploads that directory and lists
   it in `checksums.txt`. A refusal fails the hook, so nothing is published and the Homebrew job
   does not run.
3. `publish.yml`'s gate runs `check <version>`, so PyPI never ships a `yolo` whose pins the
   release refused.

The census, `TestEveryOfficialBinaryIsOnTheReleaseMatrix`, holds every embedded manifest to the
matrix in the short suite, rebuilding nothing. Each release-time call site runs only when a
release is cut, so `tools/pack-binaries`' `callsites_test.go` reads those files and fails when
one stops running the tool.

> [!WARNING]
> **A refusal in CI spends the tag.** A tag is never moved, so the fix is the next version, as
> with a changelog refusal.

> [!WARNING]
> **PyPI can be ahead of the files.** `publish.yml` runs beside `release.yml`, not after it, so
> for the minutes between PyPI publishing and goreleaser uploading, a pip-installed `yolo` of that
> version fails `yolo pack install` on a missing file, and succeeds when run again. Ordering PyPI
> after the release, as `tap-install.yml` waits for it, would close the window; it is left open.

## What main pins between releases

**Main pins its own build** of each official program, built and installed with the tree
([OQ-BP7](#OQ-BP7), [BP-D15](#BP-D15)). If an official program changed on main together with the
host half it talks to, a jail whose pins moved only at a release would run the last release's
program against the new host `yolo`, the one skew `version.SourceSkew` refuses for every other
binary yolo puts in a jail. There is no per-push prerelease.

- **`just check-ci` and `just check`** depend on `just check-pack-binaries`, the digest-only
  `check`: each url still names the last release, so only the digests are compared, and a stale
  one is refused naming `just pin-pack-binaries`, which with no version re-pins the digests and
  keeps the urls ([BP-D21](#BP-D21)). `just check` too, because `just done` runs it.
- **`just install`** runs `seed --repin` after its in-jail refusal and before anything is
  installed, so a failure installs nothing ([BP-D23](#BP-D23)). A program the tree changed is
  re-pinned **for the platforms this machine built and no others**; the tool then names the
  manifest to commit, the platforms still pinned to the old build, and `just pin-pack-binaries`
  for them, and says to re-run `just install` after committing, because the commit moves HEAD
  through `packs/` and the skew gate would refuse the install that re-pinned
  ([BP-D28](#BP-D28), [BP-D30](#BP-D30)).
- **When the pinned toolchain is unreachable** (offline, a cleaned module cache, a `Toolchain`
  bump), `seed` builds with the go on PATH if it reports exactly the pinned version, and refuses
  a re-pin from those builds. The gates have no such fallback ([BP-D26](#BP-D26)).
- **The toolchain download honors the user's proxy**: the tool asks the go on PATH for its
  effective `GOPROXY` and `GOSUMDB`, which may come from the go env file, and passes only those
  two ([BP-D27](#BP-D27)).
- **`yolo update` deploys the pulled tree exactly as pulled** ([BP-D31](#BP-D31)). Its deploy sets
  `YOLO_INSTALL_KEEP_TREE`, under which `just install` seeds without `--repin`; a build it cannot
  seed is reported with its next step and that loophole stays off, rather than failing the
  deploy or dirtying the checkout.
- **The integration harness** seeds without `--repin` once per run, into the cache under the
  run's own state directory, reporting a stale pin as DEGRADED. A test run never writes the tree
  it tests.

> [!WARNING]
> **An upgrade can leave a loophole off until `yolo pack install`.** A launch never fetches, so a
> release that moved a digest leaves that loophole inactive on every upgraded machine, and the
> launch names the command. Because nothing is stamped, a digest moves only when the program's
> own inputs do: its source, a package it imports, `vendor/`, or the pinned toolchain.

## What this does not license

- **Not a declared build step.** A build is arbitrary code execution and is not bit-reproducible
  in general, so it would lose P4 ([OQ-BP5](#OQ-BP5)).
- **Not an artifact or dependency system.** One verified file per platform per loophole, fetched
  at install and keyed by digest. It should not grow into a package manager.
- **Not a trust gate.** The digest is integrity and reproducibility. A fetched pack's host-side
  binary is honored and disclosed like its `host_daemon.cmd` ([OQ-BP6](#OQ-BP6)); see
  [the loophole system's trust section](loophole-system.md#trust-what-is-gated-and-what-is-not).

## What is not measured

- **No shipped pack declares `binaries`.** The first release that carries one is the first check
  that a release shows one bare file per build beside the archives, each in `checksums.txt` and
  matching the tagged manifest, and that a Homebrew-installed and a pip-installed `yolo` of that
  version fetch the same bytes.
- **Cross-machine reproducibility**, a maintainer's machine against a GitHub runner, and a
  macOS-native build against Linux-made pins. The release's own rebuild before upload is the
  instrument, and it refuses rather than publishes wrong.
- **goreleaser itself** is not installed in the dev jail: the hook and the two `extra_files` globs
  were checked against goreleaser's source, not run.
- **A real toolchain download** was run by hand over a fixture checkout; no test runs one.
- **`just install`'s seed** refuses inside a jail, so it is pinned only through the tool's verb
  and the recipe's text; a full integration run's seed is likewise unobserved.
- **The jail side**: `TestAJailRunsAPackBinaryItDownloaded` runs a mounted build in a real podman
  jail. A real ELF under nix-ld, and every backend but podman on Linux, are not exercised.

## Current values

Verified at `8e65f3444`. The prose above explains what each is for; this table is the only place
the values are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Cache | `~/.local/share/yolo-jail/pack-binaries/<sha256>/<name>`, mode `0555` | `paths.PackBinariesDir`, `internal/packbin` |
| Jail path of a jail build | `/etc/yolo-jail/loophole-binaries/<loophole>/<name>` | `loopholedecl.JailBinaryRoot` |
| Download size bound | 512 MiB | `packbin.DefaultMaxBytes` |
| Pinned toolchain | `go1.26.7` | `Toolchain`, `tools/pack-binaries/toolchain.go` |
| Release platforms | `linux` and `darwin`, each `amd64` and `arm64` | `.goreleaser.yaml`, read by `releasematrix.Platforms` |
| Asset URL | `https://github.com/mschulkind-oss/yolo-jail/releases/download/v<version>/<name>_<version>_<goos>_<goarch>` | `releasematrix.AssetURL` |
| Architecture levels | `GOAMD64=v1`, `GOARM64=v8.0` | the recipe, `tools/pack-binaries` |
| Release staging directory | `bundle/pack-binaries` | `.goreleaser.yaml` |
| Keep-the-tree switch for `yolo update` | `YOLO_INSTALL_KEEP_TREE` | `selfupdate.InstallKeepTreeEnv` |
| Recipes | `just pin-pack-binaries [version]`, `just check-pack-binaries` | `Justfile` |

## Why it's this way

The rulings (OQ ids) and the implementation decisions under them (BP-D ids) a maintainer would
otherwise undo. BP-D16 to BP-D20 belong to [`backend-parity.md`](../design/backend-parity.md),
which numbered its own decisions in the gap.

| ID | Ruling / decision | Why |
| :--- | :--- | :--- |
| <a id="OQ-BP5"></a>[`OQ-BP5`](#OQ-BP5) | Download-with-digest only; no declared build step (as [BP-D1](#BP-D1)), ruled 2026-09-30 | A build step is arbitrary code and loses P4. A pack that needs a built binary takes the source-build route in a throwaway capture jail, never a second build path here |
| <a id="OQ-BP6"></a>[`OQ-BP6`](#OQ-BP6) | A fetched pack may ship a host-side binary, honored and disclosed like its `host_daemon.cmd`, with no approval and no origin refusal, ruled 2026-09-30 | [`OQ-TP9`](../design/trust-paths.md#decision-ledger) deleted every origin refusal. A binary is no sharper than the host argv `host_daemon.cmd` already names, and refusing it would be the only origin refusal in the tree |
| <a id="OQ-BP7"></a>[`OQ-BP7`](#OQ-BP7) | Between releases main pins its own build, built and installed with the tree; no per-push prerelease, ruled 2026-10-05 | *"We need freedom to just fork, change things, install, and have it all work … It has to be built all together, installed all together."* |
| <a id="BP-D1"></a>BP-D1 | A binary is a declared download with a mandatory `sha256`, cached by digest with its exec bit | Per-arch selection and download were asked for, and the cached file carries the exec bit an embedded pack could not then (it now can, [the exec bit](pack-system.md#the-exec-bit)) |
| <a id="BP-D2"></a>BP-D2 | Declared in the loophole manifest, named by `{binary:}` and `{jail_binary:}`, each refused in the other half | The argv that runs the program is in that manifest, so a reference to nothing and a download nothing runs are both refusable at decode |
| <a id="BP-D3"></a>BP-D3 | Each reference takes the build for where it runs; a missing build is unsupported on the platform axis | A compiled file runs on one architecture, and the jail is Linux on the machine's architecture |
| <a id="BP-D4"></a>BP-D4 | Digest-keyed cache outside every jail mount, admitted only by verified rename; one read-only file bind per build; https at every hop, a size bound | A host daemon's build runs from the cache with the user's authority, and a jail should see only what its daemon runs |
| <a id="BP-D5"></a>BP-D5 | `yolo pack install` is the only fetch, over every selected pack; a launch never fetches | Offline launches, and an embedded pack has no fetch step of its own. The cost: a newly selected pack's loophole runs only after `yolo pack install` |
| <a id="BP-D6"></a>BP-D6 | Where a build cannot run, the part that would run it says so (guest decline, doctor "not run", nested launch skip) | Each is the existing rule for the thing beside it, so no surface runs an argv naming a file it does not have. A darwin jail build in the macos-user guest stays deferred |
| <a id="BP-D7"></a>BP-D7 | An official binary's builds are exactly the release's platforms its `platforms` admits; pure Go at `cmd/<name>` | Exactly makes the more-than-built failure unrepresentable; one runner cross-compiles everything |
| <a id="BP-D8"></a>BP-D8 | Bare executables on the tag's own release, every build every release | The one place the release already publishes plain https files, and each version stays self-contained |
| <a id="BP-D9"></a>BP-D9 | Digests committed in the tagged tree by the pin tool, rebuilt at three gates with one reproducible recipe; a program linking the embed is refused | Homebrew builds from the tag's tarball and goreleaser refuses a dirty tree. Rejected: pinning the build an earlier release published, which ships every change one release late |
| <a id="BP-D10"></a>BP-D10 | The pin tool is under `tools/`; its shared half is `internal/releasematrix` | Under `tools/` it reaches no jail or image; a `main` package cannot be imported by the census |
| <a id="BP-D11"></a>BP-D11 | `pin` writes values, never keys; a build set that is not the matrix is refused before building | The entry is the author's declaration of what ships, in a file a person formats |
| <a id="BP-D12"></a>BP-D12 | The toolchain is fetched as the `golang.org/toolchain` module, checksum-verified, with the go env file off | Setting `GOTOOLCHAIN` would not switch when the PATH's go is already that version; the env file could otherwise skip the checksum database |
| <a id="BP-D13"></a>BP-D13 | The recipe's environment is an allowlist | A drop-list would miss a variable Go adds later (`GOFIPS140` was one the design did not name) |
| <a id="BP-D14"></a>BP-D14 | `stage` writes an empty, git-ignored directory outside `dist/`, named under both goreleaser `extra_files` lists; the embed check is a static import walk | Everything in the directory is uploaded; goreleaser computes `checksums.txt` from its own list only |
| <a id="BP-D15"></a>BP-D15 | Main pins its own build; `just install` builds it from the tree with no download ([OQ-BP7](#OQ-BP7)) | See [OQ-BP7](#OQ-BP7) |
| <a id="BP-D21"></a>BP-D21 | Between releases, `check` and `pin` take no version and work on digests alone; `just check` and `just check-ci` run the check | Every commit between releases carries the last release's urls, and the refusal must name a command that needs no version |
| <a id="BP-D22"></a>BP-D22 | `seed` builds only this machine's builds with the release recipe and admits them through `packbin.Seed` | One admission path keeps "a file under a digest was verified against it" true for both arrivals |
| <a id="BP-D23"></a>BP-D23 | `just install` re-pins rather than refuses; the harness never re-pins | *"Fork, change things, install, and have it all work"*: yolo does the fix itself. Amended by BP-D30 |
| <a id="BP-D24"></a>BP-D24 | A digest failure of an official build on a from-source `yolo` names `just install` in the checkout | Between releases a changed program's release file is expected not to match. Widened by BP-D29 |
| <a id="BP-D25"></a>BP-D25 | The pin tool lists the embed's manifests, not the disk; `seed` reads them tolerantly | An untracked half-written pack made `just install` fail for files yolo never loads |
| <a id="BP-D26"></a>BP-D26 | `seed` falls back to a PATH go of exactly the pinned version and refuses a re-pin from it | `just install` should not need the network when the program is already pinned; `packbin.Seed` admits only matching bytes |
| <a id="BP-D27"></a>BP-D27 | The toolchain download honors the effective `GOPROXY` and `GOSUMDB` | The env-file-off rule had hidden a proxy set with `go env -w` |
| <a id="BP-D28"></a>BP-D28 | After a re-pin, commit and re-run `just install` | The commit moves HEAD through `packs/`, which the skew gate compares |
| <a id="BP-D29"></a>BP-D29 | The from-source hint follows any fetch failure of an official build, not only a mismatch | A program added since the last release 404s rather than mismatching |
| <a id="BP-D30"></a>BP-D30 | A re-pin writes only the platforms this machine built and verified | Whether a macOS build reproduces Linux pins is unmeasured; a re-pin writes only what this machine can vouch for |
| <a id="BP-D31"></a>BP-D31 | `yolo update` deploys the pulled tree as pulled: seed without re-pin, report, do not fail | An update is a clean build of upstream's tree; a re-pin there ships a local edit and broke autostash |
