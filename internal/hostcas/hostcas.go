// Package hostcas decides whether a launch ALIASES a host cache into the jail,
// instead of letting the jail pool a second copy of the same bytes — lever L9 of
// docs/reference/disk-footprint.md, ruled there as OQ-BF10 on 2026-09-08 and
// widened by OQ-BF11 on 2026-10-22.
//
// It is a THIRD DISPOSITION, and that is why it lives in its own package rather
// than in internal/prune. Backfill deletes what accumulated; retention bounds
// what accumulates; aliasing makes the store NOT EXIST TWICE. Nothing here
// deletes anything, and nothing here is a reaper.
//
// # TWO KINDS OF STORE, ONE MECHANISM
//
// A **content-addressed** store's keys are digests of their own contents, so the
// worst a jail can do is add a blob under a name that does not match its digest —
// which the reading tool rejects — or fill the disk. pants' `lmdb_store` is the
// one such store, and it was the whole of OQ-BF10.
//
// A **path-keyed** store (npm's index, Go's build cache, pip's HTTP cache, a
// registry checkout) lets a jail write content the host tool later reads because
// of where it sits. OQ-BF10 excluded every such store for that reason. OQ-BF11
// admits a fixed list of PACKAGE CACHES anyway, under the misguided-agent
// principle (docs/reference/misguided-agent-principle.md): poisoning a cache on
// purpose takes intent, and yolo defends against an eager helper, not an
// adversary. What a helpful agent WOULD misuse is a credential, so the line the
// ruling draws is not "content-addressed or not" but "cache or not":
//
//   - CACHES ONLY, NEVER A TOOL'S CONFIG OR CREDENTIALS. `.npmrc`, `.pypirc`,
//     `~/.cargo/credentials*`, `~/.cargo/config*`, `~/.config/<tool>` never
//     cross. Where a tool keeps cache and config in one directory (`~/.cargo`,
//     `~/.npm`), only the cache SUBDIRECTORY is aliased.
//     TestNoStoreNamesAConfigOrCredentialPath fails on an entry that breaks this.
//   - ONLY WHERE THE FILES THE JAIL WRITES END UP OWNED BY YOU. On rootless podman
//     the jail's root is the launching user, so a jail-written cache file is the
//     user's own; on rootful podman it would be host root's, inside the user's
//     cache, and the host tool would then fail on it. "Could not ask podman" is
//     not a yes ([Ownership]).
//   - ONLY ON A MATCHING OPERATING SYSTEM AND ARCHITECTURE, the gate the CAS store
//     already had.
//
// macos-user and Apple Container stay out for now: Apple Container cannot be
// relied on to honor a nested bind's options, and macos-user's run-as-you mode —
// the one where a jail-written file would be the user's — is not built yet
// (docs/design/macos-user-model.md). The backend gate below refuses both.
//
// # THE TRUST STEP
//
// The precedent is the `:ro` nix-store bind (`hostNixStore`,
// internal/cli/run/hostprobes.go): yolo already shares the host's store rather
// than duplicating it. THIS ONE IS WRITABLE, and that widening is the whole risk
// surface — jail code can write into the host user's own cache. A cache is
// useless read-only, so there is no narrower version of this feature; each
// active alias is therefore DISCLOSED at every launch and listed by `yolo
// stores` as not yolo's.
//
// # THE GATES
//
// Aliased if and only if: the runtime is podman; the host is not macOS; the host
// and the jail build for the same platform; for a path-keyed store, podman
// ANSWERED that it is rootless; the host store's path resolves; it is not yolo's
// own tree; its cache segment was not relocated by the user; it exists, is a
// directory and is writable; and aliasing would not replace a warm private copy
// with an empty one. Every failure DEGRADES TO THE STATUS QUO — the jail keeps its
// own copy — and NEVER fails a launch. A jail start must never depend on a host
// store being present.
//
// # ONE PREDICATE, TWO READERS, NO RECORD
//
// The launch emits the mount; `yolo stores` explains the decision and points at
// the stranded private copy. Both call [Plan] over injected facts, so there is
// nothing for yolo to record about the aliasing and therefore no writer to name:
// the decision is a pure function of the host's own filesystem, platform and
// podman, and re-deriving it is cheaper and cannot go stale.
package hostcas

import (
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// HostPlatform is "<goos>/<goarch>" of THIS process — what the host's own copy
// of a tool builds for.
func HostPlatform() string { return goruntime.GOOS + "/" + goruntime.GOARCH }

// JailPlatform is what the JAIL's copy of a tool builds for.
//
// Derived, not probed, on the precedent internal/cli/run's containerJailPlatform
// states for the capture manifest: "the jail is a Linux container on THIS
// machine, so its architecture is a fact about the local one, known without
// asking anything." Those are two spellings of one value with different
// consumers, and TestJailPlatformMatchesTheCaptureManifestsPlatform pins them
// together so neither can drift alone.
func JailPlatform() string { return "linux/" + goruntime.GOARCH }

// jailCacheDir is the container-side XDG cache root — the destination
// `podmanBaseMounts` binds paths.GlobalCache() at.
//
// TestAliasDestinationNestsInsideTheCacheMount compares this against the argv
// the assembler actually emits, so the two cannot drift.
const jailCacheDir = "/home/agent/.cache"

// JailRoot names the writable jail directory an alias destination nests inside.
// Each is a bind the podman launch already emits, so the destination's mountpoint
// — which is also the jail's own private copy, the bytes the alias hides — has a
// known host side that the launch can create before the container starts.
// TestEveryJailRootIsABindThePodmanArgvEmits pins each against the argv.
type JailRoot string

const (
	// RootCache is ~/.cache, bound from paths.GlobalCache() (machine-wide).
	RootCache JailRoot = "cache"
	// RootLocal is ~/.local, bound from the workspace's home state `local/`.
	RootLocal JailRoot = "local"
	// RootGoPath is ~/go — the jail's GOPATH — bound from the workspace's home
	// state `go/`.
	RootGoPath JailRoot = "gopath"
	// RootMise is /mise, the machine-wide mise store, which also holds the jail's
	// CARGO_HOME (/mise/cargo).
	RootMise JailRoot = "mise"
)

// Dir is the container-side directory of the root.
func (r JailRoot) Dir() string {
	switch r {
	case RootCache:
		return jailCacheDir
	case RootLocal:
		return "/home/agent/.local"
	case RootGoPath:
		return "/home/agent/go"
	case RootMise:
		return "/mise"
	}
	return ""
}

// CacheRoot resolves a user's XDG cache root — XDG_CACHE_HOME when it is
// absolute, else $HOME/.cache — through a getenv seam.
//
// THE SEAM IS THE WHOLE REASON THIS IS HERE rather than at each call site. The
// launcher must read the environment through Options.Getenv or a frozen argv
// stops being a function of its inputs (goldenOptions returns "" for everything,
// which is what keeps every golden argv in internal/cli/run free of an alias
// mount); `yolo stores` has no such seam and passes os.Getenv.
//
// A user who has moved a cache somewhere this does not name simply has no store
// found, and the launch degrades to the status quo. That is the right direction
// for a guess: yolo never invents a path and never creates the source.
func CacheRoot(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	if xdg := getenv("XDG_CACHE_HOME"); filepath.IsAbs(xdg) {
		return filepath.Clean(xdg)
	}
	if home := getenv("HOME"); filepath.IsAbs(home) {
		return filepath.Join(home, ".cache")
	}
	return ""
}

// HostEnv is what a store's host-side resolver reads: the XDG cache root the
// launch already resolved, and the host environment for the tools that honor a
// variable of their own (PIP_CACHE_DIR, GOMODCACHE, CARGO_HOME, …).
type HostEnv struct {
	CacheRoot string
	Getenv    func(string) string
}

func (e HostEnv) get(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// abs is the variable's value when it is an absolute path, else "". Every tool
// below ignores (or errors on) a relative override, and yolo does not guess what
// one would be relative to.
func (e HostEnv) abs(k string) string {
	if v := e.get(k); filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	return ""
}

func (e HostEnv) home() string { return e.abs("HOME") }

// underCache is <cache root>/<rel>, or "" with no cache root.
func (e HostEnv) underCache(rel string) string {
	if e.CacheRoot == "" {
		return ""
	}
	return filepath.Join(e.CacheRoot, rel)
}

// dataRoot is XDG_DATA_HOME when absolute, else $HOME/.local/share.
func (e HostEnv) dataRoot() string {
	if d := e.abs("XDG_DATA_HOME"); d != "" {
		return d
	}
	if h := e.home(); h != "" {
		return filepath.Join(h, ".local", "share")
	}
	return ""
}

// orCache is the override variable's value when absolute, else <cache root>/<rel>.
func (e HostEnv) orCache(envVar, rel string) string {
	if v := e.abs(envVar); v != "" {
		return v
	}
	return e.underCache(rel)
}

func joinIf(base string, rel ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, rel...)...)
}

// Keying says how a store's entries are named, which decides the ownership gate.
type Keying int

const (
	// KeyContent — every entry is named by a digest of its own bytes (OQ-BF10).
	KeyContent Keying = iota
	// KeyPath — an entry is named by a URL, a module path or a writer-chosen key
	// (OQ-BF11).
	KeyPath
)

// Store is one recognised host cache yolo may alias.
//
// THE SET IS CODE, NOT CONFIG, and that is a ruling rather than an omission
// (OQ-BF10, restated by OQ-BF11: "a fixed list in code"). yolo picks both sides of
// the mount from this table, so no path is user-supplied, and a config key would
// hand the cache-not-config judgment to a user who cannot see what a directory
// holds.
type Store struct {
	// Name is the stable report key.
	Name string
	// Tool is the program whose cache this is, for a human-readable notice.
	Tool string
	// Keying decides whether the ownership gate applies.
	Keying Keying
	// Host resolves the host-side source from the host's environment, the way the
	// tool itself does on Linux. "" when it cannot be resolved.
	Host func(HostEnv) string
	// Root and JailRel are the destination: <Root.Dir()>/<JailRel>, the path the
	// JAIL's own copy of the tool uses, so the mount lands on top of the jail's
	// private copy rather than beside it.
	Root    JailRoot
	JailRel string
	// Evidence is why this directory is a cache and nothing else (and, for the
	// content-addressed store, why it passes the CAS test). Printed by `yolo
	// stores`, so a reader can check the claim against their own disk.
	Evidence string
}

// Dest is the container-side path the alias mounts at.
func (s Store) Dest() string { return filepath.Join(s.Root.Dir(), s.JailRel) }

// Label is how a report names the store: the destination relative to the jail's
// cache root for a ~/.cache store (pants/lmdb_store, as it always read), else the
// destination path.
func (s Store) Label() string {
	if s.Root == RootCache {
		return s.JailRel
	}
	return s.Dest()
}

// CacheSegment is the store's top-level ~/.cache subdir — the granularity
// `cache_relocations` works at — or "" for a store outside ~/.cache, which no
// relocation can move.
func (s Store) CacheSegment() string {
	if s.Root != RootCache {
		return ""
	}
	return firstSegment(s.JailRel)
}

// Stranded is the HOST side of the jail's own private copy — the bytes the alias
// hides — given the host side of the store's jail root.
func (s Store) Stranded(rootHost string) string {
	if rootHost == "" {
		return ""
	}
	return filepath.Join(rootHost, s.JailRel)
}

// Stores is the recognised set, the content-addressed store first.
//
// HOST PATHS are each tool's documented Linux default, honoring the variable the
// tool itself honors; a path a tool takes from its own CONFIG FILE (an `.npmrc`
// `cache=`, `go env -w GOMODCACHE`, a pnpm `store-dir`) is not read, because
// reading config is exactly what this feature never does — such a host simply has
// no store at the default path, or an unused one, and the alias does no harm.
//
// JAIL PATHS are where the jail's copy of each tool writes. The jail sets HOME
// to /home/agent and leaves XDG_CACHE_HOME and XDG_DATA_HOME unset (so ~/.cache
// and ~/.local/share), exports NPM_CONFIG_CACHE=$HOME/.cache/npm and
// GOPATH=$HOME/go (internal/entrypoint/shell.go), and sets CARGO_HOME=/mise/cargo
// and MISE_DATA_DIR=/mise, and YARN_CACHE_FOLDER=$HOME/.cache/yarn because yarn
// classic takes /usr/local/share as root's home (internal/cli/run/assemble.go).
//
// NOT HERE, and why:
//   - pants' `named_caches` and `nce` — not package caches; `named_caches`'
//     INTERP-INFO records interpreters under the JAIL's home, so sharing it
//     breaks the host's pants. Excluded by OQ-BF10 by name.
//   - yarn berry's global cache (~/.yarn/berry/cache) — the jail's home is a
//     read-only skeleton and ~/.yarn is not one of its writable binds, so berry
//     has no jail-side cache to alias over. Yarn classic's ~/.cache/yarn is here.
//   - mise's cache dir — the jail sets MISE_CACHE_DIR to a per-launch /tmp
//     directory; mise's downloaded archives live in its DATA dir, which is here.
var Stores = []Store{{
	Name:    "pants-lmdb-store",
	Tool:    "pants",
	Keying:  KeyContent,
	Host:    func(e HostEnv) string { return e.underCache("pants/lmdb_store") },
	Root:    RootCache,
	JailRel: "pants/lmdb_store",
	Evidence: "digest-named blobs, verified: immutable/files/<2-hex>/<64-hex> and the 64-hex name " +
		"IS the sha256 of the file's contents (MEASURED 2026-09-09 on a 232 MB blob); the blobs are " +
		"mode r-xr-xr-x. pants' own default is ONE per-user store shared by every repo on the machine, " +
		"so multi-process sharing is the designed norm rather than a workaround",
}, {
	// npm's cache dir is ~/.npm (npm does not follow XDG), overridable by
	// npm_config_cache. It also holds _logs and _npx; only _cacache is the cache.
	Name:   "npm-cacache",
	Tool:   "npm",
	Keying: KeyPath,
	Host: func(e HostEnv) string {
		for _, k := range []string{"npm_config_cache", "NPM_CONFIG_CACHE"} {
			if v := e.abs(k); v != "" {
				return filepath.Join(v, "_cacache")
			}
		}
		return joinIf(e.home(), ".npm", "_cacache")
	},
	Root:     RootCache,
	JailRel:  "npm/_cacache",
	Evidence: "npm's package cache (content-v2 blobs, index-v5 URL index); _logs and _npx beside it are not aliased, and .npmrc lives elsewhere",
}, {
	// pnpm's content store: $PNPM_HOME/store, else $XDG_DATA_HOME/pnpm/store, else
	// ~/.local/share/pnpm/store. ~/.local/share/pnpm itself also holds global
	// installs, so only store/ is aliased.
	Name:   "pnpm-store",
	Tool:   "pnpm",
	Keying: KeyPath,
	Host: func(e HostEnv) string {
		if v := e.abs("PNPM_HOME"); v != "" {
			return filepath.Join(v, "store")
		}
		return joinIf(e.dataRoot(), "pnpm", "store")
	},
	Root:     RootLocal,
	JailRel:  "share/pnpm/store",
	Evidence: "pnpm's package store (files/ by integrity hash, index/ by package); the global-install bin dir beside it is not aliased",
}, {
	Name:     "pnpm-cache",
	Tool:     "pnpm",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return e.underCache("pnpm") },
	Root:     RootCache,
	JailRel:  "pnpm",
	Evidence: "pnpm's registry metadata cache ($XDG_CACHE_HOME/pnpm); pnpm's config is under ~/.config/pnpm",
}, {
	// Yarn classic on Linux: YARN_CACHE_FOLDER, else $XDG_CACHE_HOME/yarn.
	Name:     "yarn-cache",
	Tool:     "yarn",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return e.orCache("YARN_CACHE_FOLDER", "yarn") },
	Root:     RootCache,
	JailRel:  "yarn",
	Evidence: "yarn classic's package cache (v6/ tarball checkouts); .yarnrc is elsewhere",
}, {
	Name:     "pip-cache",
	Tool:     "pip",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return e.orCache("PIP_CACHE_DIR", "pip") },
	Root:     RootCache,
	JailRel:  "pip",
	Evidence: "pip's HTTP and wheel cache (http-v2/, wheels/); pip.conf and .pypirc are elsewhere",
}, {
	Name:     "uv-cache",
	Tool:     "uv",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return e.orCache("UV_CACHE_DIR", "uv") },
	Root:     RootCache,
	JailRel:  "uv",
	Evidence: "uv's cache (archives, wheels, index metadata); uv.toml is under ~/.config/uv",
}, {
	// Go's build cache: GOCACHE (absolute; "off" disables it), else
	// os.UserCacheDir()/go-build.
	Name:     "go-build-cache",
	Tool:     "go",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return e.orCache("GOCACHE", "go-build") },
	Root:     RootCache,
	JailRel:  "go-build",
	Evidence: "Go's build cache (action and output entries; Go trims it itself); go's env file is under ~/.config/go",
}, {
	// Go's module cache: GOMODCACHE, else the first GOPATH entry's pkg/mod, else
	// ~/go/pkg/mod. The jail's GOPATH is ~/go, so its copy is ~/go/pkg/mod.
	Name:   "go-mod-cache",
	Tool:   "go",
	Keying: KeyPath,
	Host: func(e HostEnv) string {
		if v := e.abs("GOMODCACHE"); v != "" {
			return v
		}
		if gp := e.get("GOPATH"); gp != "" {
			first := filepath.SplitList(gp)[0]
			if !filepath.IsAbs(first) {
				return ""
			}
			return filepath.Join(first, "pkg", "mod")
		}
		return joinIf(e.home(), "go", "pkg", "mod")
	},
	Root:     RootGoPath,
	JailRel:  "pkg/mod",
	Evidence: "Go's module cache (extracted modules and cache/download zips, read-only by design); GOPATH/bin beside it is not aliased",
}, {
	// cargo keeps credentials.toml and config.toml in CARGO_HOME itself, so only
	// the registry/ subdirectory is aliased (OQ-BF11 names the registry; git/
	// checkouts are not on the list).
	Name:     "cargo-registry",
	Tool:     "cargo",
	Keying:   KeyPath,
	Host:     func(e HostEnv) string { return joinIf(cargoHome(e), "registry") },
	Root:     RootMise,
	JailRel:  "cargo/registry",
	Evidence: "cargo's registry index, crate archives and extracted sources; credentials.toml and config.toml in the same CARGO_HOME are not aliased",
}, {
	// mise's DATA dir: MISE_DATA_DIR, else $XDG_DATA_HOME/mise. Only downloads/ —
	// installs/ beside it is the installed tools, and mise's config is under
	// ~/.config/mise.
	Name:   "mise-downloads",
	Tool:   "mise",
	Keying: KeyPath,
	Host: func(e HostEnv) string {
		if v := e.abs("MISE_DATA_DIR"); v != "" {
			return filepath.Join(v, "downloads")
		}
		return joinIf(e.dataRoot(), "mise", "downloads")
	},
	Root:     RootMise,
	JailRel:  "downloads",
	Evidence: "mise's downloaded tool archives; installs/ and mise's config are not aliased",
}}

func cargoHome(e HostEnv) string {
	if v := e.abs("CARGO_HOME"); v != "" {
		return v
	}
	return joinIf(e.home(), ".cargo")
}

// Ownership is whether files the jail writes end up owned by the launching user.
// A TRI-STATE, because "podman said rootful" and "yolo could not ask" must not
// collapse — and both decline: an unproven fact emits nothing.
type Ownership int

const (
	// OwnershipUnknown — podman was not asked, or its answer did not say. The zero
	// value, so a caller that forgets the fact declines.
	OwnershipUnknown Ownership = iota
	// OwnershipYours — rootless podman: the jail's root is the launching user.
	OwnershipYours
	// OwnershipNotYours — rootful podman: a jail-written file is host root's.
	OwnershipNotYours
)

// Code is the machine-readable outcome of one store's decision. It exists so the
// launch notice, the inventory row and the tests all name the same outcome.
type Code string

const (
	// CodeAliased — the mount is emitted.
	CodeAliased Code = "aliased"
	// CodeBackend — not podman. Apple Container and macos-user both land here.
	CodeBackend Code = "backend"
	// CodeMacOS — a macOS host, refused as its own rule rather than as a side
	// effect of the platform comparison below.
	CodeMacOS Code = "macos"
	// CodePlatform — the host and the jail build for different platforms.
	CodePlatform Code = "platform-mismatch"
	// CodeNotYours — a path-keyed store on a podman whose jail-written files are
	// not the launching user's (rootful).
	CodeNotYours Code = "jail-writes-not-yours"
	// CodeOwnershipUnknown — a path-keyed store on a podman that did not say
	// whether it is rootless.
	CodeOwnershipUnknown Code = "ownership-unknown"
	// CodeNoCacheRoot — the store's host path could not be resolved from the
	// launching user's environment, so there is no path to look under.
	CodeNoCacheRoot Code = "no-host-cache-root"
	// CodeSameTree — the host store IS (or sits inside) the jail's own tree, so
	// there is no second copy to alias away.
	CodeSameTree Code = "same-tree"
	// CodeAbsent — no host store. The common case on a machine that does not run
	// the tool, and not an error.
	CodeAbsent Code = "absent"
	// CodeNotDir — the host path exists and is not a directory.
	CodeNotDir Code = "not-a-directory"
	// CodeUnwritable — the host store exists but the launching user cannot write
	// it, and a cache the jail can only read is a cache the jail cannot use.
	CodeUnwritable Code = "unwritable"
	// CodeWouldColdStart — the host store is EMPTY while the jail's private copy
	// is not, so aliasing would hide a warm cache behind an empty one.
	CodeWouldColdStart Code = "would-cold-start"
	// CodeRelocated — the user moved this cache subdir somewhere else with
	// `cache_relocations`, which is an EXPLICIT decision about where these bytes
	// live. An automatic optimisation does not overrule one.
	CodeRelocated Code = "relocated-by-config"
	// CodeUnsafeSource — the host path (usually from an environment variable) is the
	// home, an ancestor of it, inside the config directory, or one of yolo's own
	// boundary directories, so binding it writable would carry config or
	// credentials across. Structural: not printed at launch.
	CodeUnsafeSource Code = "unsafe-host-path"
)

// Disposition is what one launch — or one inventory run — decided about one
// store. Source/Dest/Stranded are filled whatever the outcome, because the
// inventory has to be able to say WHICH paths the decision was about even when
// the answer was no.
type Disposition struct {
	Store    Store
	Aliased  bool
	Source   string
	Dest     string
	Stranded string
	Code     Code
	// Reason is the sentence a human reads. Empty when Aliased.
	Reason string
}

// Presence is what the planner needs to know about one directory. It is gathered
// through a seam ([Facts.Probe]) rather than with os.Stat here, and the reason is
// testability of the degenerate cases specifically: this project's own suite runs
// as root, where access(2) always reports write permission, so an UNWRITABLE
// directory is not constructible in a test.
type Presence struct {
	Exists   bool
	IsDir    bool
	Writable bool
	// Empty is true for a directory with no entries. It is only consulted for the
	// cold-start gate, so a probe that cannot cheaply answer it may report false.
	Empty bool
}

// Facts are the launch-independent inputs to a decision. Everything is passed in
// rather than read here, so the launcher and `yolo stores` can be shown to make
// the same decision from the same facts.
type Facts struct {
	// Runtime is the effective container runtime ("podman", "container",
	// "macos-user").
	Runtime string
	// IsMacOS is the host OS check, injected on the run pipeline's own convention.
	IsMacOS bool
	// HostPlatform / JailPlatform are "<goos>/<goarch>" of the host's copy of a
	// tool and the jail's. Spelled as a comparison rather than an OS check because
	// a cached build artifact or downloaded binary is platform-specific.
	HostPlatform string
	JailPlatform string
	// Ownership is podman's answer about who owns jail-written files. Consulted
	// for path-keyed stores only; the content-addressed store never needed it.
	Ownership Ownership
	// HostCacheRoot is the launching user's XDG cache root.
	HostCacheRoot string
	// HostGetenv is the launching user's environment, for the tools that honor a
	// variable of their own. nil reads as an empty environment.
	HostGetenv func(string) string
	// JailCacheHost is paths.GlobalCache() — the HOST side of the directory bound
	// at the jail's ~/.cache, and therefore where a ~/.cache store's stranded
	// private copy lives.
	JailCacheHost string
	// JailLocalHost / JailGoPathHost are the host sides of the jail's ~/.local and
	// ~/go binds (per workspace). JailMiseHost is the host side of /mise. Empty
	// when the caller has no such directory (`yolo stores` has no workspace), in
	// which case the stranded path is unknown and the cold-start gate is skipped.
	JailLocalHost  string
	JailGoPathHost string
	JailMiseHost   string
	// RelocatedSegments are the top-level cache subdirs the user moved elsewhere
	// with `cache_relocations`. A store under one of them is NOT aliased — see
	// CodeRelocated.
	RelocatedSegments []string
	// ScopeBreach reports a host path that is or contains a directory yolo keeps
	// out of every jail (paths.WritableSourceScopeBreach on the launch). nil skips
	// that one check.
	ScopeBreach func(string) bool
	// Probe gathers a Presence. nil => DefaultProbe.
	Probe func(string) Presence
}

// rootHost is the host side of a jail root.
func (f Facts) rootHost(r JailRoot) string {
	switch r {
	case RootCache:
		return f.JailCacheHost
	case RootLocal:
		return f.JailLocalHost
	case RootGoPath:
		return f.JailGoPathHost
	case RootMise:
		return f.JailMiseHost
	}
	return ""
}

// Plan decides every recognised store, in table order. It returns one
// Disposition per entry in [Stores] — never a filtered list — because the
// inventory's job is to explain the stores yolo did NOT alias just as much as the
// ones it did.
//
// It touches the filesystem only through Facts.Probe, and it never creates,
// moves or deletes anything. The destination's backing directory is provisioned
// by the caller (run.prepareHostCASAlias).
func Plan(f Facts) []Disposition {
	probe := f.Probe
	if probe == nil {
		probe = DefaultProbe
	}
	out := make([]Disposition, 0, len(Stores))
	for _, s := range Stores {
		out = append(out, decide(s, f, probe))
	}
	return out
}

// Aliased returns just the dispositions that produced a mount, which is what the
// argv emitter wants. Sorted by table order, so the argv is deterministic.
func Aliased(ds []Disposition) []Disposition {
	var out []Disposition
	for _, d := range ds {
		if d.Aliased {
			out = append(out, d)
		}
	}
	return out
}

func decide(s Store, f Facts, probe func(string) Presence) Disposition {
	rootHost := f.rootHost(s.Root)
	d := Disposition{
		Store:    s,
		Source:   s.Host(HostEnv{CacheRoot: f.HostCacheRoot, Getenv: f.HostGetenv}),
		Dest:     s.Dest(),
		Stranded: s.Stranded(rootHost),
	}

	// The backend gate is first, and it is storePackagesEligible's first clause
	// for the same reason: Apple Container cannot be relied on to honor a nested
	// bind's options, and macos-user has no container to mount into at all — and
	// its run-as-you mode, the one where a jail-written cache file would be the
	// user's, is not built yet (docs/design/macos-user-model.md).
	if f.Runtime != "podman" {
		d.Code, d.Reason = CodeBackend, "it needs podman, and this launch uses the "+
			f.Runtime+" runtime"
		return d
	}
	// NEVER ON macOS. Stated as its own refusal rather than left to fall out of
	// the platform comparison below.
	if f.IsMacOS {
		d.Code, d.Reason = CodeMacOS, "it is never done on macOS: the jail is Linux in a VM, "+
			"so the host's build artifacts and the jail's are not interchangeable"
		return d
	}
	if f.HostPlatform == "" || f.JailPlatform == "" || f.HostPlatform != f.JailPlatform {
		d.Code, d.Reason = CodePlatform, "the host builds for "+orUnknown(f.HostPlatform)+
			" and the jail for "+orUnknown(f.JailPlatform)+
			", and a cached build artifact or download is platform-specific"
		return d
	}
	// THE OWNERSHIP GATE (OQ-BF11), path-keyed stores only. On rootful podman the
	// jail's root is host root, so every file the jail added would land in the
	// user's own cache owned by root, and the host tool would then fail to update
	// or prune it. Unknown declines too: "could not ask" is not "yes".
	if s.Keying == KeyPath {
		switch f.Ownership {
		case OwnershipYours:
		case OwnershipNotYours:
			d.Code, d.Reason = CodeNotYours, "this podman is rootful, so files the jail writes "+
				"into the host's "+s.Tool+" cache would be owned by root rather than by you"
			return d
		default:
			d.Code, d.Reason = CodeOwnershipUnknown, "podman did not say whether it is rootless, "+
				"so yolo cannot tell who would own the files the jail writes into the host's "+
				s.Tool+" cache"
			return d
		}
	}
	if d.Source == "" || !filepath.IsAbs(d.Source) {
		d.Code, d.Reason = CodeNoCacheRoot, "the launching user's "+s.Tool+" cache directory could "+
			"not be resolved, so there is nowhere to look for a host store"
		return d
	}
	// AN ENVIRONMENT-DERIVED PATH IS USED AS GIVEN (PIP_CACHE_DIR, CARGO_HOME, …),
	// so a variable pointing at the home itself, at an ancestor of it, at the
	// user's config directory, or at yolo's own boundary directories would make
	// the alias a writable bind of exactly what OQ-BF11 says never crosses.
	if reason := unsafeSource(d.Source, f); reason != "" {
		d.Code, d.Reason = CodeUnsafeSource, "the host "+s.Tool+" cache path "+d.Source+" "+reason+
			", so it is not shared"
		return d
	}
	// SAME TREE means there is no second copy to alias away — the source and the
	// destination's own backing are one directory. Reachable when XDG_CACHE_HOME
	// (or CARGO_HOME, …) points into yolo's state dir.
	if rootHost != "" && (under(d.Source, rootHost) || under(rootHost, d.Source)) {
		d.Code, d.Reason = CodeSameTree, "the host store is inside the jail's own tree ("+
			rootHost+"), so there is no second copy of it to alias away"
		return d
	}

	// AN EXPLICIT CONFIG DECISION OUTRANKS AN AUTOMATIC OPTIMISATION: a user who
	// relocated `pants` to get 40 G off the disk $HOME is on must not have the
	// alias put `pants/lmdb_store` straight back onto that disk.
	if seg := s.CacheSegment(); seg != "" {
		for _, r := range f.RelocatedSegments {
			if r == "" || r != seg {
				continue
			}
			d.Code, d.Reason = CodeRelocated, "cache_relocations moves "+seg+
				" to storage you chose, and aliasing the host's copy of "+s.JailRel+
				" would put those bytes back under your home cache"
			return d
		}
	}

	src := probe(d.Source)
	switch {
	case !src.Exists:
		// NOT AN ERROR, and NOT CREATED. Creating it would alias an empty
		// directory over whatever the jail has.
		d.Code, d.Reason = CodeAbsent, "this host has no "+s.Tool+" store at "+d.Source
		return d
	case !src.IsDir:
		d.Code, d.Reason = CodeNotDir, d.Source+" exists but is not a directory"
		return d
	case !src.Writable:
		d.Code, d.Reason = CodeUnwritable, d.Source+" is not writable by the launching user, "+
			"and a cache the jail can only read is a cache the jail cannot use"
		return d
	}

	// THE COLD-START GATE: an empty host store aliased over a warm private copy
	// hides a cache and buys nothing.
	if src.Empty && d.Stranded != "" {
		if priv := probe(d.Stranded); priv.Exists && priv.IsDir && !priv.Empty {
			d.Code, d.Reason = CodeWouldColdStart, "the host store at "+d.Source+
				" is empty while this jail's own copy is not, so aliasing it would hide a "+
				"warm cache behind an empty one and force a re-fetch"
			return d
		}
	}

	d.Aliased, d.Code = true, CodeAliased
	return d
}

// firstSegment is a relative path's top-level directory.
func firstSegment(rel string) string {
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

// under reports whether child is base or a descendant of base.
func under(child, base string) bool {
	if child == "" || base == "" {
		return false
	}
	child, base = filepath.Clean(child), filepath.Clean(base)
	if child == base {
		return true
	}
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func orUnknown(s string) string {
	if s == "" {
		return "an unknown platform"
	}
	return s
}

// OwnershipOf reads podman's `host.security.rootless` answer: nil (not asked, or
// not said) is unknown, true is yours, false is not yours.
func OwnershipOf(rootless *bool) Ownership {
	switch {
	case rootless == nil:
		return OwnershipUnknown
	case *rootless:
		return OwnershipYours
	}
	return OwnershipNotYours
}

// unsafeSource is why a resolved host path may not be bound writable into a jail,
// or "" when it may. It is the boundary OQ-BF11 draws (caches, never config or
// credentials) applied to a path that came from the user's environment.
func unsafeSource(src string, f Facts) string {
	env := HostEnv{Getenv: f.HostGetenv}
	if home := env.home(); home != "" {
		if under(home, src) {
			return "is the home directory or contains it"
		}
		if under(src, filepath.Join(home, ".config")) {
			return "is inside ~/.config, where tools keep config and credentials"
		}
	}
	if xdg := env.abs("XDG_CONFIG_HOME"); xdg != "" && under(src, xdg) {
		return "is inside $XDG_CONFIG_HOME, where tools keep config and credentials"
	}
	if f.ScopeBreach != nil && f.ScopeBreach(src) {
		return "is or contains a directory yolo keeps out of every jail"
	}
	return ""
}
