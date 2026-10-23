---
status: current
verified: 2026-10-18
verified_commit: 37ea6bca3
covers:
  - internal/loopholedecl/binaries.go
  - internal/loopholedecl/officialbinaries_test.go
  - internal/loopholes/binaries.go
  - internal/packbin/
  - internal/cli/packbinaries.go
  - integration/packbinary_test.go
tags: [loopholes, packs, binaries]
summary: "How a loophole ships a compiled program: the `binaries` manifest key and its two tokens, and the digest-keyed cache that only `yolo pack install` fetches into. A third-party pack names a download URL and its sha256. An official pack declares no binary; its programs ship as yolo subcommands, and a census refuses an official `binaries` entry."
---

# Pack-shipped binaries — the `binaries` key and the cache

**Status:** Verified 2026-10-18 against `37ea6bca3`. UNMEASURED: no shipped pack declares
`binaries`, and by [OQ-BP8](#OQ-BP8) none may, so the mechanism runs only for a pack someone
else publishes. [What is not measured](#what-is-not-measured) lists the rest.

A loophole that runs a compiled program it does not bake declares the program as a
**download**: one build per platform, each with an https `url` and a mandatory `sha256`, in
the loophole's own manifest under `binaries`. `yolo pack install` fetches each build this
machine needs, verifies it, and caches it by digest with its exec bit; a launch runs it from
that cache and never fetches. This is for a pack yolo does not ship. An **official** pack
declares no binary: every program yolo's own loopholes run is a subcommand of a binary yolo
already ships, and yolo's build neither builds nor pins a pack program
([OQ-BP8](#OQ-BP8)).

| Component | Lives in |
| :--- | :--- |
| The `binaries` key, its two tokens, the wrong-half refusals | `internal/loopholedecl` (`binaries.go`, `JailBinaryRoot`) |
| Token resolution, the jail file bind, the inert reasons | `internal/loopholes` (`binaries.go`, `runtime.go`, `inertreport.go`) |
| The digest-keyed cache: download, verify, admit | `internal/packbin` (`Path`, `Present`, `Fetcher.Ensure`) |
| The install step | `internal/cli` (`packbinaries.go`) |
| The census refusing an official `binaries` entry | `internal/loopholedecl` (`TestNoOfficialPackDeclaresABinary`) |

**Reads with:** [`loophole-system.md`](loophole-system.md#a-program-the-loophole-downloads),
the loophole-side summary of this mechanism and the manifest it lives in;
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
digest makes a reinstall of the same pin offline. A pack-tree program's `prebuilt` artifact shares
this cache and its checks but is the one exception: a launch fetches it, because a program is
installed at launch ([BG-D7](../design/pack-buildable-programs.md#BG-D7)).

**A digest mismatch is an integrity failure, not a fetch failure.** It refuses the install,
names both digests, caches nothing, and never falls back to a cached copy, since the bytes are
the pin.

**yolo's own programs are built in, not downloaded.** An official loophole runs
`yolo internal daemon <name>` on the host or `yolo-jaild <name>` in the jail, so its program is
built, shipped and versioned with yolo itself and needs no download, digest or release file. See
[Official packs declare no binary](#official-packs-declare-no-binary).

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
in every `yolo` binary. That is what `binaries` exists for, for a pack yolo does not ship: one
verified download per platform, cached with its exec bit.

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

The pack's author decides which builds to declare, and should declare only the ones a file is
published for: a declared build with no file behind it reads as "supported, missing", so
`yolo pack install` fails on it instead of the loophole reading as unsupported on that platform.

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

**`yolo pack install` fills the cache, and nothing else does** (`yolo pack update` runs it;
[BP-D5](#BP-D5)). It walks every selected pack and fetches each build this machine needs whatever
the loophole's switch says, and nothing for a loophole whose `platforms` leaves this machine out.
A reinstall re-hashes the cached copy and fetches again when it no longer matches.

**What a missing piece produces**, everywhere a loophole is reported
([BP-D6](#BP-D6)):

| Missing | What happens |
| :--- | :--- |
| No build declared for where a token runs | Unsupported on the platform axis; nothing can be installed |
| A declared build not yet fetched | The loophole is inactive; the launch's inert report, `yolo loopholes list` and `yolo check` name `yolo pack install` |
| A `doctor_cmd` whose build is missing | Not run, with the reason |
| A digest mismatch at install | Refused, both digests named, nothing cached |

Inside a jail, a jail build counts once the outer launch mounted it, and a nested launch binds
that mounted copy on when its own cache lacks it. A host build is the host's business in what
the jail reports. A nested launch runs its host daemons in the jail from the jail's own cache, so
it starts none whose build that cache lacks and says so.

On **macos-user**, a host binary runs as anywhere. The guest declines a jail daemon whose argv
names a `{jail_binary:}` path: that path exists only in a container, and a manifest's jail build
is a Linux one. Placing a darwin jail build in the guest is **not built**. **Apple Container**
needs nothing: every pack loophole is inert there.

## Official packs declare no binary

yolo's own build does not produce, publish, pin or seed a pack program, by the maintainer's ruling
([`OQ-BP8`](#OQ-BP8)). Every official loophole program is a subcommand: a host daemon is
`yolo internal daemon <name>`, a jail daemon is `yolo-jaild <name>`, and each ships, versions and
updates with the binary it is part of. So an official pack never needs `binaries`, and an
official `binaries` entry would name a file nothing produces: `yolo pack install` would fail on
it for every user of the pack.

**A census refuses one** ([BP-D32](#BP-D32)). `TestNoOfficialPackDeclaresABinary`, in the short
suite and so in `just check-ci`, decodes every loophole manifest the packs embed carries and fails
on any that declares `binaries`, naming the manifest and each binary. Its message says the
program becomes a `yolo internal daemon <name>` or `yolo-jaild <name>` subcommand, and that a real
reason to ship a separate binary is a question for the maintainer, reopening [`OQ-BP8`](#OQ-BP8), rather than
a manifest edit.

**Third-party packs are unaffected.** A pack yolo does not ship may declare a remote binary with
its checksum, exactly as the sections above describe.

## What this does not license

- **Not a declared build step.** A build is arbitrary code execution and is not bit-reproducible
  in general, so it would lose P4 ([OQ-BP5](#OQ-BP5)). A pack that needs a built program declares
  it `via: "pack"`, built in the sealed build jail
  ([programs built from a pack's tree](pack-system.md#program-via-pack)).
- **Not an artifact or dependency system.** One verified file per platform per loophole, fetched
  at install and keyed by digest. It should not grow into a package manager.
- **Not a trust gate.** The digest is integrity and reproducibility. A fetched pack's host-side
  binary is honored and disclosed like its `host_daemon.cmd` ([OQ-BP6](#OQ-BP6)); see
  [the loophole system's trust section](loophole-system.md#trust-what-is-gated-and-what-is-not).

## What is not measured

- **No pack in the wild declares `binaries` that this repository knows of.** The unit tests serve
  builds from a local TLS server; no real publisher's release has been installed.
- **The jail side**: `TestAJailRunsAPackBinaryItDownloaded` runs a mounted build in a real podman
  jail. A real ELF under nix-ld, and every backend but podman on Linux, are not exercised.

## Current values

Verified at `37ea6bca3`. The prose above explains what each is for; this table is the only place
the values are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Cache | `~/.local/share/yolo-jail/pack-binaries/<sha256>/<name>`, mode `0555` | `paths.PackBinariesDir`, `internal/packbin` |
| Jail path of a jail build | `/etc/yolo-jail/loophole-binaries/<loophole>/<name>` | `loopholedecl.JailBinaryRoot` |
| Download size bound | 512 MiB | `packbin.DefaultMaxBytes` |

## Why it's this way

The rulings (OQ ids) and the implementation decisions under them (BP-D ids) a maintainer would
otherwise undo. BP-D16 to BP-D20 belong to [`backend-parity.md`](../design/backend-parity.md),
which numbered its own decisions in the gap.

| ID | Ruling / decision | Why |
| :--- | :--- | :--- |
| <a id="OQ-BP5"></a>[`OQ-BP5`](#OQ-BP5) | Download-with-digest only; no declared build step (as [BP-D1](#BP-D1)), ruled 2026-09-30 | A build step is arbitrary code and loses P4. A pack that needs a built binary takes the source-build route in a throwaway capture jail, never a second build path here |
| <a id="OQ-BP6"></a>[`OQ-BP6`](#OQ-BP6) | A fetched pack may ship a host-side binary, honored and disclosed like its `host_daemon.cmd`, with no approval and no origin refusal, ruled 2026-09-30 | [`OQ-TP9`](../design/trust-paths.md#decision-ledger) deleted every origin refusal. A binary is no sharper than the host argv `host_daemon.cmd` already names, and refusing it would be the only origin refusal in the tree |
| <a id="OQ-BP7"></a>[`OQ-BP7`](#OQ-BP7) | Between releases main pinned its own build of each official program, built and installed with the tree, ruled 2026-10-05. **Superseded by [OQ-BP8](#OQ-BP8)** on 2026-10-18 | *"We need freedom to just fork, change things, install, and have it all work … It has to be built all together, installed all together."* [`OQ-BP8`](#OQ-BP8) keeps that freedom by making every official program part of the binaries the tree already builds |
| <a id="OQ-BP8"></a>[`OQ-BP8`](#OQ-BP8) | **yolo's build does not build, pin, publish or seed a pack binary; an official pack declares none.** The `binaries` key stays for third-party packs. Ruled 2026-10-18 | *"So for our case, I don't want to have this involved in our build. Until the point where we have a real good reason to ship a binary that's not built in like this, let's not revisit this."* And on third-party packs: *"they can specify remote binary with a checksum. That's totally valid."* No official pack had ever declared one: every official loophole program is a yolo subcommand |
| <a id="BP-D1"></a>BP-D1 | A binary is a declared download with a mandatory `sha256`, cached by digest with its exec bit | Per-arch selection and download were asked for, and the cached file carries the exec bit an embedded pack could not then (it now can, [the exec bit](pack-system.md#the-exec-bit)) |
| <a id="BP-D2"></a>BP-D2 | Declared in the loophole manifest, named by `{binary:}` and `{jail_binary:}`, each refused in the other half | The argv that runs the program is in that manifest, so a reference to nothing and a download nothing runs are both refusable at decode |
| <a id="BP-D3"></a>BP-D3 | Each reference takes the build for where it runs; a missing build is unsupported on the platform axis | A compiled file runs on one architecture, and the jail is Linux on the machine's architecture |
| <a id="BP-D4"></a>BP-D4 | Digest-keyed cache outside every jail mount, admitted only by verified rename; one read-only file bind per build; https at every hop, a size bound | A host daemon's build runs from the cache with the user's authority, and a jail should see only what its daemon runs |
| <a id="BP-D5"></a>BP-D5 | `yolo pack install` is the only fetch, over every selected pack; a launch never fetches | Offline launches, and an embedded pack has no fetch step of its own. The cost: a newly selected pack's loophole runs only after `yolo pack install` |
| <a id="BP-D6"></a>BP-D6 | Where a build cannot run, the part that would run it says so (guest decline, doctor "not run", nested launch skip) | Each is the existing rule for the thing beside it, so no surface runs an argv naming a file it does not have. A darwin jail build in the macos-user guest stays deferred |
| <a id="BP-D15"></a>BP-D7 to BP-D15, BP-D21 to BP-D31 | **Retired 2026-10-18 by [OQ-BP8](#OQ-BP8)**, with the official half they decided: the release matrix, the reproducible build recipe and pinned toolchain, the pin tool and its `pin`, `check`, `stage` and `seed` verbs, the three release gates, main's own pin between releases, and `just install`'s seed | Each decided how yolo's build makes or pins an official pack program, which it no longer does. Git history holds their text; the IDs stay allocated so they are not reused |
| <a id="BP-D32"></a>BP-D32 | *Implementation decision under [`OQ-BP8`](#OQ-BP8).* An official `binaries` entry is refused by a short-suite census, `TestNoOfficialPackDeclaresABinary`, whose message names the subcommand route and [`OQ-BP8`](#OQ-BP8). `yolo pack lint` does not refuse the key | Without a refusal, an official manifest could reintroduce a file nothing builds, and every user's `yolo pack install` would fail on it. Lint reads every pack, and for a third-party pack the key is valid, so the census over the official embed is where the rule belongs |
