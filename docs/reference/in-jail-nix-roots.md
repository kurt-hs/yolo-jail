---
status: accepted
stage: CURRENT
verified: 2026-10-16
verified_commit: 8e65f3444
covers:
  - internal/nixroots/
  - internal/cli/nixroots.go
  - internal/cli/run/translatedroots.go
  - internal/entrypoint/nixrootwatcher.go
  - internal/image/gcroot.go
  - internal/image/prefixroot.go
tags: [nix, gc-roots, podman, mount-namespaces, storage]
summary: "A podman jail's nix roots are recorded under the jail's spelling, which the host deletes as stale. yolo's own in-jail roots are registered under the host's spelling of their link through a launcher-stated host path map, and an in-jail root watcher keeps each link a user's nix made as a yolo-owned managed root under the host's spelling, with a 7-day lease, a 64-root workspace cap, inspection and release (`yolo nix-roots`)."
---

# In-jail nix roots — translated roots and the root watcher

**Status:** Verified 2026-10-16 against `8e65f3444`. MEASURED against the host daemon (Nix 2.35.2,
untrusted client): a kept link is a root the host lists under its own spelling, and releasing it
removes that root; yolo's own image, prefix and copier roots register under the host's spelling.
MEASURED in a running jail: a fresh `nix-build` output link appeared in `yolo nix-roots list` as
`by watch`, with the watcher log recording the same link. UNMEASURED: that the host's root query
lists a link the watcher admitted, and the handoff fence against a concurrent GC
([owed host observation](#owed-host-observation)).

A podman jail reaches the host's nix daemon through the bind-mounted socket. The daemon records
an indirect root as the path **string** the client sends and resolves it later on the host,
where a jail path such as `/workspace/result` does not exist, so it deletes the root as stale.
Two mechanisms fix that:

- **yolo's own roots** (the image, the prefix, the image extras, the store-delivered packages'
  profile, the image copier) are registered as **translated roots**, under the host's spelling of
  their link.
- **A user's roots** — any `result` link, profile generation or nix-direnv cache a jail's nix makes
  — are seen by the **root watcher**, which keeps each one as a **managed root**: a yolo-owned
  link to the same store path, registered under the host's spelling, with a lease, a cap and a
  release.

Without them, a link made in a jail is not a root the host honors, and a host collection can
delete its target between two commands. Keeping every such link forever would instead let a
forgotten workspace pin host disk.

| Component | Lives in |
| :--- | :--- |
| The host path map, and the launch's mounts it is read from | `internal/nixroots` (`hostmap.go`, `MapEnv`), `internal/cli/run` (`translatedroots.go`) |
| The daemon client: handshake, `AddTempRoot`, `AddIndirectRoot` | `internal/nixroots` (`daemon.go`, `Conn`) |
| The registrar and the pinned handoff | `internal/nixroots` (`register.go`, `Registrar.Root`, `Registrar.Pin`) |
| The managed-root registry, its lease, cap and release | `internal/nixroots` (`registry.go`) |
| The root watcher | `internal/nixroots` (`watch.go`, `watch_linux.go`), started by `internal/entrypoint` (`nixrootwatcher.go`) as `yolo-jaild nix-roots` |
| The command | `internal/cli` (`nixroots.go`): `yolo nix-roots list|keep|release|prune` |
| yolo's own root consumers | `internal/cli/run` (`gcRooter`), `internal/image` (`RegisterImageRoot`, `RegisterPrefixRoot`) |

**Reads with:** [`image-retention.md`](image-retention.md#a-gc-is-safe-at-any-moment) (why a
running jail's closure must be rooted), [`workspace-path-mirroring.md`](workspace-path-mirroring.md)
(why the workspace is not mounted at the host's path instead),
[`macos-user-nix-and-features.md`](macos-user-nix-and-features.md#how-nix-works-here) (the backend
that needs none of this).

---

## Why a jail-spelled root is lost

<a id="why-a-jail-spelled-root-is-lost"></a>

**Who resolves the path.** An indirect root is two symlinks: `gcroots/auto/<hash>` pointing at the
out-link, which points at the store path. At collection time nix walks `gcroots/`, reads each auto
entry's out-link path and `lstat`s it; an absent path is logged as `removing stale link` and the
entry is deleted. The walker is always in the host's namespace (root's `nix-collect-garbage` or a
daemon worker). **A root query deletes too**: `nix-store --query --roots` runs the same walk.
Exactly two hops root anything; a third symlink, or a relative second target, roots nothing. The
auto entry is named by the path string's hash alone, so every jail's `/workspace/result` shares
one entry.

**What the client sends.** `nix build --out-link X` and `nix-store --add-root X` run on the client:
`AddTempRoot(P)`, create the link locally, then `AddIndirectRoot(absPath(X))`. The daemon takes the
string verbatim, with no trust check. `absPath` does not resolve symlinks. `nix profile`,
`nix develop --profile` and nix-direnv all root through this call. An untrusted remote client has
no supported way to make a daemon-side permanent root (`AddPermRoot` requires a trusted client;
upstream [NixOS/nix#11812](https://github.com/NixOS/nix/issues/11812) and
[#7138](https://github.com/NixOS/nix/issues/7138) are open).

### What already protects a jail

<a id="what-already-protects-a-jail"></a>

- **Runtime roots.** The collector scans every host `/proc/<pid>` (`exe`, `cwd`, `fd`, `maps`,
  `environ`), which includes the jail's processes, so a running `nix shell` or `nix develop` is
  protected while it runs. This stops working under the unprivileged-daemon mode
  (`use-roots-daemon`), which cannot read other processes' `/proc`.
- **Temp roots** last exactly as long as the client's daemon connection.

So the gap is **between commands**: a link no live process is using right now.

> [!WARNING]
> **A Nix 2.36 client will stop sending temp roots to a 2.35 daemon**
> ([NixOS/nix#16113](https://github.com/NixOS/nix/pull/16113): client temp roots only when the
> daemon advertises `addTempRoots`, with no fallback). The jail's nix comes from the image and the
> daemon from the host, so an image bump can open a window *during* a build. Whoever bumps the
> image's nix owns this.

### The handoff fence

<a id="the-handoff-fence"></a>

A new permanent root does not update a collection already running: nix scans permanent roots
before temporary ones, and `AddIndirectRoot` notifies no collection holding an earlier snapshot.
Temporary roots do take part in the collector's synchronization. So a temp pin taken at discovery
protects only from that point, and an earlier pin alone is not a fence: a collection can start
after it, miss the later permanent link, and find the connection closed. **The pin is reasserted
after the permanent registration is acknowledged, before the connection closes** ([NR-D7](#NR-D7)).
Its behavior against a concurrent collection is argued from the nix source, not measured.

## Translated roots and the host path map

<a id="translated-roots"></a>

A **translated root** *(coined in the design doc this reference replaces)* is an ordinary indirect
root sent with the host's absolute spelling of a link the jail created. It is not a new kind of nix
root.

**The host path map.** A podman launch that mounts the host nix daemon, and is not sealed, sets
`YOLO_HOST_PATH_MAP` in the jail: a JSON object from each mount's destination to the host
directory it is, read off the argv the launch is about to run. The jail does not derive it (a
`mountinfo` root is relative to a filesystem, not a host path).

- **A read-write bind of an absolute source translates.** A read-only bind, a volume and a tmpfs
  are in the map with an empty value, a **mask** *(coined in the same design doc)*: mounts nest,
  and without masks longest-prefix matching would translate a path under one through the mount
  above it.
- **Longest destination prefix wins**, because the home binds nest.
- **A nested launcher composes**: it passes each source through its own jail's map first; a
  source that map cannot spell becomes a mask.
- **A map in which nothing translates is not set.**
- **A path under no translating mount is not translated** and stays as dead as it was: the
  container root filesystem, the per-launch `/tmp` and `/var/tmp` volumes ([NR-D1](#NR-D1)), and
  `/nix/store` itself.

A translated root is sent only for a link whose target is a literal `/nix/store/…` path, and is
idempotent: the auto entry is named by the path's hash.

### yolo's own roots

<a id="yolos-own-roots"></a>

`gcRooter` gives a host launch `nix-store --add-root` and an in-jail launch the translated root,
for the image's root, the prefix's, the image extras' and the store-delivered packages' profile;
and in a jail only, the image copier's out-link is registered again as a translated root after an
in-jail build ([NR-D2](#NR-D2)). A link the registrar cannot translate is left uncreated and the
daemon is not dialled. A jail whose launcher stated no map gets none of these. In a jail two
failures print a warning on the launch stream (the daemon refusing the root, and a roots
directory that cannot be made); every other failure is silent, and none fails the launch.

**Reaping stays with the existing reapers, run where the links live**: a nested launcher's
`yolo prune` sweeps its own `build/roots`, and its housekeeping slot and `yolo prune` both sweep
its `build/prefix-roots` and `build/package-roots`, under the host's rules ([`image-retention.md`](image-retention.md),
[build-state links](disk-footprint.md#build-state-links)). A translated root dies with its link.

> [!NOTE]
> **Not translated: the prefix's own `--out-link`** (`build/jail-prefix-*`), which registered
> under the jail's spelling while launches still built a prefix. No yolo writes one now; the
> leftovers are removed by liveness as retired out-links. **`build/package-roots` holds one
> root per distinct image-extras closure and one per distinct `packages:` list**, not two
> profile roots: the slot keeps each profile a running jail was started with, plus the newest
> root of each kind, and ages out the rest after a week, on the host and in a jail alike.

## Managed roots

<a id="managed-roots"></a>

A **managed root** *(coined in the design doc this reference replaces)* is a link yolo owns under
the workspace's state directory, pointing at the same store path as a link a jail asked nix to
root, and registered as a translated root. The user's own link is never touched. Releasing one
deletes only yolo's link; the next host collection may then take the store path, exactly as after
a host user deleted `result`. Managed links are used, rather than registering the user's own link
under the host's spelling, because the daemon cannot drop an indirect root: only a link yolo owns
can expire, be capped or be released ([NR-D3](#NR-D3)).

- **Where.** `<workspace>/.yolo/nix-roots/`: `roots.json` (the records), `links/<id>` (the
  managed links) and `.lock`. The id is a prefix of the SHA-256 of the jail link's path. Host and
  jail open the same directory.
- **One record per jail link**: both spellings of the jail link, the store path, the managed
  link's host spelling, and when it was admitted and last renewed. A repointed link moves its
  managed link to the new target.
- **Renewal** is a fresh daemon entry for the same link (a rebuild) or a `keep`. A scan that finds
  an unchanged, already-kept link does not renew it.
- **A release sticks** ([NR-D8](#NR-D8)). The jail-spelled auto entry outlives a release until the
  host's next collection or root query, so the registry remembers each release, and a scan skips
  an entry no newer than it. A rebuild is newer and is kept again.
- **Release** is explicit (`release <id|link>…` or `--all`) or by lifecycle: the lease lapsed, the
  cap was exceeded, or the jail's link was deleted or no longer points into the store. A managed
  link no record names (a crash between link and record) is removed by `prune`.

> [!IMPORTANT]
> **The host deletes in a jail-writable directory**, so every access goes through an `os.Root` on
> a directory checked not to be a link. The only names it deletes are ids, and only symlinks. A
> record is data: no path in it is one the host deletes. A `roots.json` yolo cannot parse is
> reported and never overwritten. On the host, `prune` checks a record's link only when its host
> spelling lies in the workspace, so a record cannot make the host stat a path of the jail's
> choosing.

### Lease and cap

<a id="lease-and-cap"></a>

A lease from the last renewal and a per-workspace cap, least recently renewed released first
([NR-D4](#NR-D4)). They are constants, not configuration.

- **The cap is a count, not bytes.** It bounds how many closures a forgotten workspace can pin,
  not how large one is; `list` does not price closures.
- **When it applies.** Lease and cap at every admission; the full pass (gone links, stray links)
  when the watcher starts, hourly while it runs, and on `yolo nix-roots prune` on either side.
  **No host process enforces it while no jail of the workspace runs**, so a lease can outlast its
  length on an idle workspace until the next launch or `prune`.
- **A cooperative retention policy, not a hostile-agent quota**: an untrusted daemon client can
  already register permanent roots of its own.

## The root watcher

<a id="the-root-watcher"></a>

The **root watcher** *(coined in the design doc this reference replaces)* is an in-jail process,
`yolo-jaild nix-roots`, watching a read-only bind of the host's `/nix/var/nix/gcroots/auto`. The
daemon writes one entry there for every indirect root any client asks for, so the watcher covers
every root-making tool at once and sits in no nix command's path ([NR-D3](#NR-D3)).

- **The bind.** A podman launch that mounts the host nix daemon, and is not sealed, binds the
  host's `gcroots/auto` read-only at `/run/yolo/nix-gcroots-auto`; a nested launcher binds its own
  view on. A host with no such directory gets no bind. The bind prints no launch line
  ([OQ-NR2](#why-its-this-way)).
- **The start.** The boot step `start_nix_root_watcher` starts it directly, only when the bind and
  a map are both present, not through the composed daemon list ([NR-D6](#NR-D6)). A lock file keeps
  it to one process per jail; one that dies stays down until the next boot, whose scan catches up.
- **The loop.** The inotify watch is placed before the start-up scan, so an entry made during the
  scan arrives as an event; a queue overflow triggers a rescan; a vanished watch ends the process.
- **Admission.** For each entry it reads the recorded path and continues only if that path exists
  in this jail as a symlink straight into the store, lies under a mount the map can spell, and is
  not under the registry itself.
- **The handoff**, on one daemon connection, for an admitted entry and for `keep` alike
  ([NR-D7](#NR-D7)): `AddTempRoot(P)` as soon as the target is read; refuse if P is already gone;
  under the registry's lock make or repoint `links/<id>`; `AddIndirectRoot` with its host
  spelling; `AddTempRoot(P)` again once acknowledged; record and close. A refused registration
  removes the managed link it made and records nothing.

Raw entries do not say which jail made them. Two jails with `/workspace/result` share one entry
and each checks its own link, so a jail can renew its unchanged link when another rebuilds at the
same path; the lease and the cap still bound that.

### Supported producers

<a id="supported-producers"></a>

Every client that roots through `AddIndirectRoot` reaches the watcher: `nix build` and
`nix-build --out-link`, `nix-store --add-root`, profile generations (`nix profile`,
`nix develop --profile`), nix-direnv, the flake-registry cache. Profile generations are kept one by
one; the relative `profile` alias is never itself a root. Not kept: a link under `/tmp` or
`/var/tmp` ([NR-D1](#NR-D1)), and any link under a read-only bind or a volume, which includes every
link in a workspace mounted read-only.

- **`macos-user`** shares host paths, so a root nix makes there is already one the host honors: no
  watcher runs and `keep` says so.
- **Apple Container** mounts no host daemon.
- **A jail started by an older launcher** binds no auto directory and runs no watcher; `keep` still
  works wherever the map exists.

> [!NOTE]
> **yolo's own jail-spelled out-links in a nested launch** (the prefix's `--out-link`, the image
> build's and the copier's) lie under a mapped bind and point into the store, so the watcher
> admits them too: a second root on a store path the translated roots already hold, costing places
> under the cap and holding the nested image closure for the lease after its own roots go.

### Known limit

<a id="known-limit"></a>

**A collection can still take a root in the moment before the watcher registers it** ([NR-D5](#NR-D5)).
While the producing client stays connected its own temp root holds the path, and the watcher pins
it within milliseconds, so usually the two overlap. A root is lost only when a host collection
runs in that gap: the producer disconnected before the watcher's pin landed, or a collection or
root query deleted the jail-spelled entry before the watcher read it.

**What the agent sees:** a dangling `result` or profile link, with no row in `yolo nix-roots list`
and no `kept` line in the watcher's log. **Rebuilding fixes it.** `yolo nix-roots keep <link>`
keeps a link by hand and refuses one whose store path is gone. If the watcher was not running, the
next boot's scan admits entries still in `gcroots/auto`; one removed in the meantime is gone.

## The command and the briefing

<a id="the-command"></a>

`yolo nix-roots list [--format json]` shows each managed root's id, jail link, store path, how it
was admitted, and when it was renewed and expires. `keep <link>` admits a link through the same
fenced handoff. `release` and `prune` work on the host with no jail running; on the host, `keep`
points at `nix-store --add-root`.

A jail with the host daemon is briefed that its links under the workspace and home are kept, with
the lease, the cap and the command, that a link under `/tmp` is not, and that a dangling link is
fixed by rebuilding.

## What this does not license

<a id="what-this-does-not-license"></a>

- **Rooting jail-only paths.** `/tmp` in a jail is scratch ([NR-D1](#NR-D1)).
- **Deleting user state or running a host collection.** Expiry and release remove only validated
  yolo-managed links: no profile-generation deletion, no shared-store collection, no broad root
  scan.
- **A protocol proxy in front of the daemon** that rewrites `AddIndirectRoot` in flight. It is the
  one fully transparent option, but the worker protocol is not self-delimiting, so the proxy would
  frame every operation, NAR streams included, for every protocol version; one bug breaks every
  nix command in every jail.
- **Trusting the jail** (`trusted-users`) for `AddPermRoot`. The root would still need the host's
  spelling, and a trusted client is root-equivalent on the host.
- **A temp-root keeper** as the primary mechanism: it protects only while the jail runs and cannot
  drop one root short of reconnecting.
- **A host-side reconciler** reading only `gcroots/auto`: raw entries carry no producing-jail
  identity, and the host cannot see a jail's paths to check them.
- **A CLI wrapper or a patched nix client.** A wrapper sits in every nix command's path and is
  blind to profiles and nix-direnv; a patched client needs a maintained nix fork in the image.
- **Mirroring the workspace at its host path**, ruled out in
  [`workspace-path-mirroring.md`](workspace-path-mirroring.md#decision-ledger); translated roots
  fix this crossing without it.
- **Apple Container and the container Macs.** In-jail nix there is ruled possible but not planned
  ([`setup-support-gaps.md`](../plans/setup-support-gaps.md#2-ranked-gap-backlog) G21).

## Measurements the design rests on

<a id="measurements"></a>

Run 2026-09-28 from a podman jail on Linux (host daemon Nix 2.35.2, protocol 1.38; jail client Nix
2.34.8; `Trusted: 0`), with a fresh `builtins.toFile` probe. "Dead" means listed by
`nix-store --gc --print-dead`, a dry run.

| # | What the jail did | Host's verdict |
| --- | --- | --- |
| M1 | `nix-store --add-root` at a jail-only path | dead; the next root query logged the stale-link removal |
| M2 | held the path open on an fd | alive, as a runtime root |
| M3 | the path only in an environment variable | alive, as a runtime root |
| M4 | `AddTempRoot` over a held connection | alive while connected, dead once closed |
| M5 | `nix build --out-link` at a path bound at the host's spelling | alive, listed under the host path |
| M6 | a link in the workspace, then `AddIndirectRoot` of its host spelling | alive; dead again once the link was removed |
| M7 | `--add-root` through a symlinked directory | the daemon recorded the path verbatim |
| M8 | a nested read-only container with a bind under a read-only path | the mountpoint was created on the read-only root |

M6 is the proof of host-path translation. The first root query also deleted more than a hundred
stale auto links into jail homes, test temp dirs and worktrees, each a root a jail had asked for
and never had.

> [!CAUTION]
> **A root query against the shared host store prunes stale entries.** Use disposable fixtures for
> new experiments, and do not rerun `nix-store --query --roots` against the shared store without an
> isolated safety plan.

## Owed host observation

<a id="owed-host-observation"></a>

The running watcher's automatic observation is measured: a tiny `nix-build` output link under the
durable dir appeared in `yolo nix-roots list` as `by watch` after a bounded wait, and the watcher
log recorded `kept` for it. What is not measured is that **the host resolves that managed link**:
the one attempted host root query printed stale-link removals for unrelated entries and was not
repeated. To close it, on the host, inspect the specific test managed link in `gcroots/auto`
without a root query and confirm it resolves to the test store path, or run the root query against
a dedicated isolated store. A jail-local link, the watcher log and `yolo nix-roots list` are not
host-daemon evidence.

## Why it's this way

<a id="why-its-this-way"></a>

Owner rulings and the implementation decisions that built them. The `NR-D` ids are cited from
`internal/nixroots`, `internal/cli/run` and `internal/image`; each decision is reversible.

| ID | Ruling / decision | Why it holds |
| :--- | :--- | :--- |
| OQ-NR1 | **Automatic translated-root protection with a mandatory lifecycle, a cap, workspace inspection and explicit release** — never indefinite retention that depends on an agent remembering. Ruled 2026-10-07. | Built as [NR-D3](#NR-D3) to [NR-D8](#NR-D8). |
| OQ-NR2 | **A read-only host `gcroots/auto` bind is permitted, with no routine launch line of its own.** Ruled 2026-10-07. | Unrelated trust banners are unchanged. |
| OQ-NR3 | **Binds only translate**; decided as [NR-D1](#NR-D1). | — |
| OQ-NR4 | **yolo's own in-jail roots are the first consumer of translation**; decided as [NR-D2](#NR-D2). | — |
| <a id="NR-D1"></a>NR-D1 | **The map holds the binds the launcher wrote; the per-launch `/tmp` and `/var/tmp` volumes are masks.** | Those volumes are deleted when the jail exits, so a root there outlives only the launch, and a running process is already kept by runtime roots. A nested launcher cannot learn a volume's host path, so translating volumes would give one link two answers. MEASURED: a nested launch's `/tmp` workspace was a mask. |
| <a id="NR-D2"></a>NR-D2 | **yolo's own in-jail roots are translated roots**: the image, prefix, image-extras and store-profile roots, and the copier's out-link after an in-jail build. Reaping stays with the existing reapers, run where the links live. | All of them root under the build dir, inside a mapped bind. The host's reapers never walk a workspace's home overlay, so the jail that made a link reaps it. MEASURED for the image, prefix and copier roots; the two profile roots are pinned by unit tests only. |
| <a id="NR-D3"></a>NR-D3 | **The root watcher, with `keep` as its hand-run form, over managed roots.** | It reads the daemon's own record of every root request, so it covers every client with nothing in nix's path; only a link yolo owns can expire, be capped or be released. |
| <a id="NR-D4"></a>NR-D4 | **A 7-day lease from the last renewal and a 64-root cap per workspace**, least recently renewed released first; constants. | A week covers a weekly revisit and matches the image roots' age floor ([`OQ-LS1`](image-retention.md#why-its-this-way)); 64 leaves room for a few projects' nix-direnv profiles and recent profile generations. |
| <a id="NR-D5"></a>NR-D5 | **The residual window of the [known limit](#known-limit) is accepted.** | Closing it needs the producing client to wait for yolo, a patched nix. A lost root costs a rebuild. |
| <a id="NR-D6"></a>NR-D6 | **The boot starts the watcher directly, not through `YOLO_JAIL_DAEMONS`.** | That payload carries readiness, orphan and disclosure handling for loophole and service daemons; the watcher serves no endpoint and holds no credential. |
| <a id="NR-D7"></a>NR-D7 | **The handoff is pinned and fenced on one connection**: pin at discovery, register, pin again after acknowledgment, close. | [The handoff fence](#the-handoff-fence)'s order. The pin starts at discovery, so it narrows the window without closing it. |
| <a id="NR-D8"></a>NR-D8 | **A release is remembered, so a scan cannot revive it**: each release records the link and its time, bounded in count and age, and a scan skips an auto entry no newer than that. | The daemon renames a fresh entry into place on every request, so an older entry is the request the release already answered. |

## Current values

Verified at `8e65f3444`. The prose above explains what each of these is for; this table is the
only place the values themselves are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Host path map variable | `YOLO_HOST_PATH_MAP` | `nixroots.MapEnv` |
| Auto-root bind in the jail | `/run/yolo/nix-gcroots-auto` (read-only) | `nixroots.HostAutoDir` |
| Lease | 7 days from the last renewal | `nixroots.DefaultLease` |
| Cap | 64 managed roots per workspace | `nixroots.DefaultCap` |
| Full housekeeping pass while the watcher runs | hourly | `nixroots.DefaultHousekeeping` |
| Release memory | 1,024 links, 30 days | `tombstoneMax`, `tombstoneAge` (`registry.go`) |
| Registry lock wait | 30 s | `lockWait` (`registry.go`) |
| Registry | `<workspace>/.yolo/nix-roots/` (`roots.json`, `links/<id>`, `.lock`); id = 16 hex digits of the jail link's SHA-256 | `internal/nixroots` (`registry.go`) |
| Watcher lock | `/tmp/yolo-nix-roots.lock` | `nixroots.WatchLock` |
| Watcher log | `~/.local/state/yolo-jail-daemons/nix-roots.log`; past 4 MiB it moves to `nix-roots.log.1`, at the boot's open and while the watcher writes | `internal/entrypoint` (`nixrootwatcher.go`); the cap `logcap.MaxBytes`, the running trim `logcap.Writer` through `nixroots.LogEnv` |
| Worker protocol spoken | 1.37 | `internal/nixroots` (`daemon.go`) |
