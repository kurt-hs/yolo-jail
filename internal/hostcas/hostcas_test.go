package hostcas

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// facts is a Facts value that PASSES every gate, so each test below can fail
// exactly one thing and see exactly one Code. Built around a temp tree so the
// happy path is real rather than stubbed.
func facts(t *testing.T) (Facts, string, string) {
	t.Helper()
	root := t.TempDir()
	hostCache := filepath.Join(root, "host-cache")
	jailCache := filepath.Join(root, "yolo-cache")
	src := lmdbHost(hostCache)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-empty host store: the cold-start gate is about emptiness, and a
	// fixture with no state in it would pass the happy-path assertions for the
	// wrong reason (the vacuous-fixture trap AGENTS.md names).
	if err := os.WriteFile(filepath.Join(src, "data.mdb"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Facts{
		Runtime:      "podman",
		IsMacOS:      false,
		HostPlatform: "linux/amd64",
		JailPlatform: "linux/amd64",
		// INJECTED, because DefaultProbe is deliberately a stub off Linux
		// (probe_other.go returns the zero Presence — the feature is never-macOS).
		// Plan's gate logic is pure and platform-independent, so it should be tested
		// on every platform; leaving this nil made every store read "absent" on
		// darwin and turned four gate tests into check-macos failures.
		Probe:         linuxLikeProbe,
		HostCacheRoot: hostCache,
		JailCacheHost: jailCache,
	}, hostCache, jailCache
}

// linuxLikeProbe is DefaultProbe's Linux behaviour expressed in portable Go, so a
// gate test means the same thing on every platform. It is deliberately NOT a
// hand-written fake: a probe that disagreed with the real one would let a gate
// test pass against a Presence the real prober never produces.
func linuxLikeProbe(path string) Presence {
	st, err := os.Stat(path)
	if err != nil {
		return Presence{}
	}
	p := Presence{Exists: true, IsDir: st.IsDir()}
	if !p.IsDir {
		return p
	}
	p.Writable = st.Mode().Perm()&0o300 == 0o300
	entries, err := os.ReadDir(path)
	p.Empty = err == nil && len(entries) == 0
	return p
}

// lmdbHost is the content-addressed store's host path under a cache root.
func lmdbHost(hostCache string) string { return Stores[0].Host(HostEnv{CacheRoot: hostCache}) }

func one(t *testing.T, f Facts) Disposition {
	t.Helper()
	ds := Plan(f)
	if len(ds) != len(Stores) {
		t.Fatalf("Plan returned %d dispositions, want one per recognised store (%d)", len(ds), len(Stores))
	}
	return ds[0]
}

// The happy path, so every negative test below has something to be a deviation
// from. An alias that never fires makes every gate test vacuous.
func TestPlanAliasesARecognisedWritableHostStore(t *testing.T) {
	f, hostCache, jailCache := facts(t)
	d := one(t, f)
	if !d.Aliased || d.Code != CodeAliased {
		t.Fatalf("aliased=%v code=%q reason=%q — want the happy path", d.Aliased, d.Code, d.Reason)
	}
	if want := lmdbHost(hostCache); d.Source != want {
		t.Errorf("Source = %q, want %q", d.Source, want)
	}
	if want := filepath.Join(jailCache, Stores[0].JailRel); d.Stranded != want {
		t.Errorf("Stranded = %q, want %q — the private copy the alias replaces", d.Stranded, want)
	}
	if !strings.HasPrefix(d.Dest, jailCacheDir+"/") {
		t.Errorf("Dest = %q, want it nested inside the jail's cache mount %q", d.Dest, jailCacheDir)
	}
	if d.Reason != "" {
		t.Errorf("Reason = %q on the happy path, want empty", d.Reason)
	}
}

// One case per gate. Each mutates the passing Facts in exactly one way, so a
// deleted gate shows up as one failing row rather than as a whole red package.
func TestPlanGates(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Code
		mut  func(t *testing.T, f *Facts, hostCache, jailCache string)
	}{{
		name: "apple container",
		want: CodeBackend,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.Runtime = "container" },
	}, {
		name: "macos-user",
		want: CodeBackend,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.Runtime = "macos-user" },
	}, {
		// NEVER ON macOS, as its own rule. A podman-on-macOS launch reaches this.
		name: "macos host",
		want: CodeMacOS,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.IsMacOS = true },
	}, {
		name: "arch mismatch",
		want: CodePlatform,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.JailPlatform = "linux/arm64" },
	}, {
		name: "os mismatch",
		want: CodePlatform,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.HostPlatform = "darwin/amd64" },
	}, {
		name: "unknown platform",
		want: CodePlatform,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.HostPlatform = "" },
	}, {
		name: "no host cache root",
		want: CodeNoCacheRoot,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.HostCacheRoot = "" },
	}, {
		name: "relative host cache root",
		want: CodeNoCacheRoot,
		mut:  func(_ *testing.T, f *Facts, _, _ string) { f.HostCacheRoot = "relative/cache" },
	}, {
		// XDG_CACHE_HOME pointing at yolo's own cache: the source and the
		// destination's backing would be one directory, so there is no second copy
		// to alias away and the mount would be a directory bound over itself.
		name: "host store inside the jail's own cache",
		want: CodeSameTree,
		mut: func(t *testing.T, f *Facts, _, jailCache string) {
			f.HostCacheRoot = jailCache
			if err := os.MkdirAll(filepath.Join(jailCache, Stores[0].JailRel), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}, {
		// AN EXPLICIT CONFIG DECISION OUTRANKS AN AUTOMATIC OPTIMISATION. Without
		// this gate both mounts apply and the deeper alias wins for its own
		// subtree, so a user who relocated `pants` to get 40 G off their home disk
		// silently gets 27 G of it back — the exact failure cache_relocations
		// exists to prevent, caused by the feature meant to save space.
		name: "the user relocated this cache subdir",
		want: CodeRelocated,
		mut: func(_ *testing.T, f *Facts, _, _ string) {
			f.RelocatedSegments = []string{"other", Stores[0].CacheSegment()}
		},
	}, {
		name: "host store absent",
		want: CodeAbsent,
		mut: func(t *testing.T, f *Facts, hostCache, _ string) {
			if err := os.RemoveAll(lmdbHost(hostCache)); err != nil {
				t.Fatal(err)
			}
		},
	}, {
		name: "host store is a file",
		want: CodeNotDir,
		mut: func(t *testing.T, f *Facts, hostCache, _ string) {
			p := lmdbHost(hostCache)
			if err := os.RemoveAll(p); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("not a store"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}, {
		// UNWRITABLE IS DRIVEN THROUGH THE PROBE SEAM, not through chmod: this
		// project's own suite runs as root, where access(2) always reports write
		// permission, so an unwritable directory is not constructible here. A gate
		// whose failure branch no test can reach is a gate that works until the day
		// it matters.
		name: "host store not writable",
		want: CodeUnwritable,
		mut: func(_ *testing.T, f *Facts, _, _ string) {
			f.Probe = func(string) Presence {
				return Presence{Exists: true, IsDir: true, Writable: false}
			}
		},
	}, {
		// The cold-start gate: an empty host store aliased over a warm private copy
		// hides a cache and buys nothing.
		name: "empty host store over a warm private copy",
		want: CodeWouldColdStart,
		mut: func(t *testing.T, f *Facts, hostCache, jailCache string) {
			if err := os.Remove(filepath.Join(lmdbHost(hostCache), "data.mdb")); err != nil {
				t.Fatal(err)
			}
			priv := filepath.Join(jailCache, Stores[0].JailRel)
			if err := os.MkdirAll(priv, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(priv, "data.mdb"), []byte("warm"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			f, hostCache, jailCache := facts(t)
			tc.mut(t, &f, hostCache, jailCache)
			d := one(t, f)
			if d.Aliased {
				t.Fatalf("aliased anyway — %q must decline", tc.name)
			}
			if d.Code != tc.want {
				t.Fatalf("code = %q, want %q (reason: %q)", d.Code, tc.want, d.Reason)
			}
			if strings.TrimSpace(d.Reason) == "" {
				t.Error("no reason — a decline is disclosed, never silent (§5.4)")
			}
		})
	}
}

// The relocation gate is keyed on the store's TOP-LEVEL segment, which is the
// granularity cache_relocations works at — so an unrelated relocation must not
// suppress the alias, or one moved cache would switch the feature off wholesale.
func TestAnUnrelatedRelocationDoesNotBlockTheAlias(t *testing.T) {
	f, _, _ := facts(t)
	f.RelocatedSegments = []string{"huggingface", "uv", ""}
	d := one(t, f)
	if !d.Aliased {
		t.Fatalf("declined with %q (%s) — only a relocation of THIS store's segment "+
			"(%q) may block it", d.Code, d.Reason, Stores[0].CacheSegment())
	}
}

func TestFirstSegment(t *testing.T) {
	for in, want := range map[string]string{
		"pants/lmdb_store": "pants", "pants": "pants", "a/b/c": "a", "": "",
	} {
		if got := firstSegment(in); got != want {
			t.Errorf("firstSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// The cold-start gate must not fire when there is nothing warm to lose: a fresh
// machine with an empty host store and no private copy SHOULD alias, so the
// first jail fills the shared store instead of a private one.
func TestEmptyHostStoreStillAliasesWhenNothingIsStranded(t *testing.T) {
	f, hostCache, _ := facts(t)
	if err := os.Remove(filepath.Join(lmdbHost(hostCache), "data.mdb")); err != nil {
		t.Fatal(err)
	}
	d := one(t, f)
	if !d.Aliased {
		t.Fatalf("declined with code %q (%s) — an empty host store with no warm private copy "+
			"has nothing to cold-start", d.Code, d.Reason)
	}
}

// Every entry is completely declared, and its destination is a relative path
// inside a known jail root.
func TestEveryStoreIsCompletelyDeclared(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Stores {
		if s.Name == "" || s.Tool == "" || s.JailRel == "" || s.Host == nil || s.Root.Dir() == "" {
			t.Errorf("Stores[%q] is incompletely declared: %+v", s.Name, s)
		}
		if seen[s.Name] {
			t.Errorf("Stores has two entries named %q — the name is the report key", s.Name)
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.Evidence) == "" {
			t.Errorf("Stores[%q] has no Evidence — what the directory holds is recorded, not assumed", s.Name)
		}
		if filepath.IsAbs(s.JailRel) || strings.Contains(s.JailRel, "..") {
			t.Errorf("Stores[%q].JailRel = %q must be a relative path inside its jail root", s.Name, s.JailRel)
		}
	}
}

// OQ-BF10's content-addressed store keeps behaving exactly as before: still the
// first entry, still content-keyed (so the ownership gate never reaches it), at
// the same paths on both sides.
func TestTheContentAddressedStoreIsUnchanged(t *testing.T) {
	s := Stores[0]
	if s.Name != "pants-lmdb-store" || s.Keying != KeyContent {
		t.Fatalf("Stores[0] = %q keying %d, want pants-lmdb-store, content-addressed", s.Name, s.Keying)
	}
	if got := s.Host(HostEnv{CacheRoot: "/h/.cache"}); got != "/h/.cache/pants/lmdb_store" {
		t.Errorf("host path = %q", got)
	}
	if s.Dest() != "/home/agent/.cache/pants/lmdb_store" || s.Label() != "pants/lmdb_store" {
		t.Errorf("dest = %q label = %q", s.Dest(), s.Label())
	}
	for _, s := range Stores[1:] {
		if s.Keying != KeyPath {
			t.Errorf("Stores[%q] is not path-keyed — only pants' lmdb_store has the CAS evidence", s.Name)
		}
	}
}

// THE PER-TOOL PATHS, pinned. The host side is each tool's documented Linux
// default (and the variable the tool honors); the jail side is where the jail's
// own copy writes, given how the jail sets HOME, NPM_CONFIG_CACHE, GOPATH,
// CARGO_HOME and MISE_DATA_DIR.
func TestStorePaths(t *testing.T) {
	defaults := HostEnv{CacheRoot: "/h/.cache", Getenv: envOf(map[string]string{"HOME": "/h"})}
	overridden := HostEnv{CacheRoot: "/x/cache", Getenv: envOf(map[string]string{
		"HOME": "/h", "XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data",
		"npm_config_cache": "/o/npm", "PNPM_HOME": "/o/pnpm", "YARN_CACHE_FOLDER": "/o/yarn",
		"PIP_CACHE_DIR": "/o/pip", "UV_CACHE_DIR": "/o/uv", "GOCACHE": "/o/gocache",
		"GOMODCACHE": "/o/gomod", "CARGO_HOME": "/o/cargo", "MISE_DATA_DIR": "/o/mise",
	})}
	for _, tc := range []struct {
		name, host, hostOverridden, dest string
	}{
		{"pants-lmdb-store", "/h/.cache/pants/lmdb_store", "/x/cache/pants/lmdb_store", "/home/agent/.cache/pants/lmdb_store"},
		{"npm-cacache", "/h/.npm/_cacache", "/o/npm/_cacache", "/home/agent/.cache/npm/_cacache"},
		{"pnpm-store", "/h/.local/share/pnpm/store", "/o/pnpm/store", "/home/agent/.local/share/pnpm/store"},
		{"pnpm-cache", "/h/.cache/pnpm", "/x/cache/pnpm", "/home/agent/.cache/pnpm"},
		{"yarn-cache", "/h/.cache/yarn", "/o/yarn", "/home/agent/.cache/yarn"},
		{"pip-cache", "/h/.cache/pip", "/o/pip", "/home/agent/.cache/pip"},
		{"uv-cache", "/h/.cache/uv", "/o/uv", "/home/agent/.cache/uv"},
		{"go-build-cache", "/h/.cache/go-build", "/o/gocache", "/home/agent/.cache/go-build"},
		{"go-mod-cache", "/h/go/pkg/mod", "/o/gomod", "/home/agent/go/pkg/mod"},
		{"cargo-registry", "/h/.cargo/registry", "/o/cargo/registry", "/mise/cargo/registry"},
		{"mise-downloads", "/h/.local/share/mise/downloads", "/o/mise/downloads", "/mise/downloads"},
	} {
		s := storeNamed(t, tc.name)
		if got := s.Host(defaults); got != tc.host {
			t.Errorf("%s: default host path = %q, want %q", tc.name, got, tc.host)
		}
		if got := s.Host(overridden); got != tc.hostOverridden {
			t.Errorf("%s: overridden host path = %q, want %q", tc.name, got, tc.hostOverridden)
		}
		if got := s.Dest(); got != tc.dest {
			t.Errorf("%s: jail path = %q, want %q", tc.name, got, tc.dest)
		}
	}
	if len(Stores) != 11 {
		t.Errorf("Stores has %d entries and this table pins 11 — pin the new entry's paths here", len(Stores))
	}
	// A GOPATH list takes its first entry, as go does; a relative one resolves nothing.
	gm := storeNamed(t, "go-mod-cache")
	if got := gm.Host(HostEnv{Getenv: envOf(map[string]string{"HOME": "/h", "GOPATH": "/g1:/g2"})}); got != "/g1/pkg/mod" {
		t.Errorf("GOPATH list: %q", got)
	}
	if got := gm.Host(HostEnv{Getenv: envOf(map[string]string{"HOME": "/h", "GOPATH": "rel"})}); got != "" {
		t.Errorf("relative GOPATH resolved to %q, want nothing", got)
	}
	// GOCACHE=off is not a path.
	if got := storeNamed(t, "go-build-cache").Host(HostEnv{CacheRoot: "/h/.cache",
		Getenv: envOf(map[string]string{"GOCACHE": "off"})}); got != "/h/.cache/go-build" {
		t.Errorf("GOCACHE=off: %q", got)
	}
	// No environment at all resolves nothing, rather than a relative path.
	for _, s := range Stores {
		if got := s.Host(HostEnv{}); got != "" {
			t.Errorf("%s resolved %q from an empty environment", s.Name, got)
		}
	}
}

// CACHES ONLY, NEVER A TOOL'S CONFIG OR CREDENTIALS (OQ-BF11). Each entry's host
// and jail path, under default and overridden environments, must not BE or
// CONTAIN a directory that holds a tool's config or credentials, and must not
// name such a file. A new entry that aliases ~/.cargo whole, ~/.config/<tool>, or
// a .npmrc fails here.
func TestNoStoreNamesAConfigOrCredentialPath(t *testing.T) {
	forbiddenNames := map[string]bool{
		".npmrc": true, ".pypirc": true, ".yarnrc": true, ".yarnrc.yml": true, ".netrc": true,
		"credentials": true, "credentials.toml": true, "config": true, "config.toml": true,
		".config": true, "pip.conf": true, "uv.toml": true, "env": true, ".ssh": true,
		".gitconfig": true, "auth.json": true, ".docker": true,
	}
	// Directories that hold config or credentials beside a cache: aliasing one of
	// these — or anything above one — would carry them across.
	holders := func(home string) []string {
		return []string{
			home, home + "/.cargo", home + "/.config", home + "/.local", home + "/.local/share",
			home + "/.local/share/pnpm", home + "/.local/share/mise", home + "/.npm", home + "/go",
			home + "/.cache", "/o/cargo", "/o/mise", "/o/pnpm", "/o/npm", "/x/cache", "/x/data",
			"/home/agent", "/home/agent/.cache", "/home/agent/.local", "/home/agent/.local/share",
			"/home/agent/go", "/mise", "/mise/cargo", "/home/agent/.config",
		}
	}
	envs := []HostEnv{
		{CacheRoot: "/h/.cache", Getenv: envOf(map[string]string{"HOME": "/h"})},
		{CacheRoot: "/x/cache", Getenv: envOf(map[string]string{
			"HOME": "/h", "XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data",
			"npm_config_cache": "/o/npm", "PNPM_HOME": "/o/pnpm", "CARGO_HOME": "/o/cargo",
			"MISE_DATA_DIR": "/o/mise", "GOPATH": "/h/go",
		})},
	}
	for _, s := range Stores {
		var paths []string
		for _, e := range envs {
			paths = append(paths, s.Host(e))
		}
		paths = append(paths, s.Dest())
		for _, p := range paths {
			if p == "" {
				t.Errorf("%s: resolved no path under a complete environment", s.Name)
				continue
			}
			for _, part := range strings.Split(p, "/") {
				if forbiddenNames[part] {
					t.Errorf("%s: path %q names %q, a config or credential location — only cache "+
						"directories cross (OQ-BF11)", s.Name, p, part)
				}
			}
			for _, h := range holders("/h") {
				if under(h, p) {
					t.Errorf("%s: path %q is or contains %q, which holds config or credentials "+
						"beside its cache — alias the cache subdirectory only", s.Name, p, h)
				}
			}
		}
	}
}

func storeNamed(t *testing.T, name string) Store {
	t.Helper()
	for _, s := range Stores {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no store named %q", name)
	return Store{}
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// pathKeyedFacts passes every gate for the npm store, rootless.
func pathKeyedFacts(t *testing.T) (Facts, Store) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	s := storeNamed(t, "npm-cacache")
	src := filepath.Join(home, ".npm", "_cacache")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Facts{
		Runtime: "podman", HostPlatform: "linux/amd64", JailPlatform: "linux/amd64",
		Ownership:     OwnershipYours,
		Probe:         linuxLikeProbe,
		HostCacheRoot: filepath.Join(home, ".cache"),
		HostGetenv:    envOf(map[string]string{"HOME": home}),
		JailCacheHost: filepath.Join(root, "yolo-cache"),
	}, s
}

func dispositionOf(t *testing.T, ds []Disposition, name string) Disposition {
	t.Helper()
	for _, d := range ds {
		if d.Store.Name == name {
			return d
		}
	}
	t.Fatalf("no disposition for %q", name)
	return Disposition{}
}

// THE OWNERSHIP GATE (OQ-BF11): a path-keyed store aliases only on a podman that
// ANSWERED rootless. Rootful refuses, and unknown — "could not ask" — refuses
// too. The content-addressed store is unaffected either way.
func TestPathKeyedStoresNeedRootlessPodman(t *testing.T) {
	f, s := pathKeyedFacts(t)
	if d := dispositionOf(t, Plan(f), s.Name); !d.Aliased {
		t.Fatalf("rootless baseline not aliased (%q: %s) — the cases below would pass for the "+
			"wrong reason", d.Code, d.Reason)
	} else if d.Source != filepath.Join(f.HostGetenv("HOME"), ".npm", "_cacache") ||
		d.Stranded != filepath.Join(f.JailCacheHost, "npm", "_cacache") {
		t.Errorf("source %q stranded %q", d.Source, d.Stranded)
	}
	for _, tc := range []struct {
		name string
		own  Ownership
		want Code
	}{
		{"rootful", OwnershipNotYours, CodeNotYours},
		{"unknown", OwnershipUnknown, CodeOwnershipUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s := pathKeyedFacts(t)
			f.Ownership = tc.own
			ds := Plan(f)
			for _, d := range ds {
				if d.Store.Keying != KeyPath {
					continue
				}
				if d.Aliased || d.Code != tc.want {
					t.Errorf("%s: aliased=%v code=%q, want declined with %q", d.Store.Name, d.Aliased, d.Code, tc.want)
				}
				if strings.TrimSpace(d.Reason) == "" {
					t.Errorf("%s: decline without a reason", d.Store.Name)
				}
			}
			_ = s
		})
	}
}

// The content-addressed store never consults ownership, so a rootful or
// unknown podman aliases it exactly as before.
func TestTheContentAddressedStoreIgnoresOwnership(t *testing.T) {
	for _, own := range []Ownership{OwnershipUnknown, OwnershipNotYours, OwnershipYours} {
		f, _, _ := facts(t)
		f.Ownership = own
		if d := one(t, f); !d.Aliased {
			t.Errorf("ownership %d: lmdb_store declined with %q (%s)", own, d.Code, d.Reason)
		}
	}
}

// The existing gates hold for path-keyed stores too: backend, macOS, platform,
// relocation (for a ~/.cache store), and absence.
func TestPathKeyedStoresKeepEveryExistingGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Code
		mut  func(f *Facts)
	}{
		{"macos-user", CodeBackend, func(f *Facts) { f.Runtime = "macos-user" }},
		{"apple container", CodeBackend, func(f *Facts) { f.Runtime = "container" }},
		{"macos host", CodeMacOS, func(f *Facts) { f.IsMacOS = true }},
		{"arch mismatch", CodePlatform, func(f *Facts) { f.JailPlatform = "linux/arm64" }},
		{"relocated npm", CodeRelocated, func(f *Facts) { f.RelocatedSegments = []string{"npm"} }},
		{"no HOME", CodeNoCacheRoot, func(f *Facts) { f.HostGetenv = envOf(nil) }},
		{"absent", CodeAbsent, func(f *Facts) { f.HostGetenv = envOf(map[string]string{"HOME": "/nonexistent-home"}) }},
		{"inside yolo's tree", CodeSameTree, func(f *Facts) {
			f.HostGetenv = envOf(map[string]string{"npm_config_cache": filepath.Join(f.JailCacheHost, "npm")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s := pathKeyedFacts(t)
			tc.mut(&f)
			d := dispositionOf(t, Plan(f), s.Name)
			if d.Aliased || d.Code != tc.want {
				t.Errorf("aliased=%v code=%q (%s), want %q", d.Aliased, d.Code, d.Reason, tc.want)
			}
		})
	}
	// A store outside ~/.cache has no cache segment, so no relocation reaches it.
	f, _ := pathKeyedFacts(t)
	f.RelocatedSegments = []string{"go", "mod", "pkg", "cargo", "mise", "pnpm"}
	for _, name := range []string{"go-mod-cache", "cargo-registry", "pnpm-store", "mise-downloads"} {
		if d := dispositionOf(t, Plan(f), name); d.Code == CodeRelocated {
			t.Errorf("%s declined as relocated, but cache_relocations only moves ~/.cache subdirs", name)
		}
	}
}

// Each jail root's stranded copy is under the host side of that root.
// NEVER RECOGNISED, by name: pants' named_caches records interpreters under the
// jail's home and nce is the download store those records point into, so sharing
// either breaks the host's pants (OQ-BF10, kept by OQ-BF11). An entry whose host
// or jail path reaches them fails here.
func TestNamedCachesAndNceAreNeverRecognised(t *testing.T) {
	e := HostEnv{CacheRoot: "/h/.cache", Getenv: envOf(map[string]string{"HOME": "/h"})}
	for _, s := range Stores {
		for _, p := range []string{s.Host(e), s.Dest()} {
			for _, bad := range []string{"pants/named_caches", "nce"} {
				for _, root := range []string{"/h/.cache/", "/home/agent/.cache/"} {
					if under(p, root+bad) || under(root+bad, p) {
						t.Errorf("%s: path %q reaches %s, which is excluded by name", s.Name, p, bad)
					}
				}
			}
		}
	}
}

// AN ENVIRONMENT-DERIVED HOST PATH IS GUARDED: a variable pointing a store at the
// home, an ancestor of it, the config directory, or a directory yolo keeps out of
// every jail declines structurally (silent at launch), and a normal override still
// aliases.
func TestEnvDerivedHostPathsAreGuarded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pip    func(home string) string
		breach bool
		want   Code
	}{
		{"the home itself", func(h string) string { return h }, false, CodeUnsafeSource},
		{"an ancestor of the home", func(h string) string { return filepath.Dir(h) }, false, CodeUnsafeSource},
		{"under ~/.config", func(h string) string { return filepath.Join(h, ".config", "npm") }, false, CodeUnsafeSource},
		{"under $XDG_CONFIG_HOME", func(h string) string { return filepath.Join(filepath.Dir(h), "xdgconf", "npm") }, false, CodeUnsafeSource},
		{"a scope breach", func(h string) string { return filepath.Join(filepath.Dir(h), "elsewhere") }, true, CodeUnsafeSource},
		{"an ordinary override", func(h string) string { return filepath.Join(filepath.Dir(h), "elsewhere") }, false, CodeAliased},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := pathKeyedFacts(t)
			home := f.HostGetenv("HOME")
			pipCache := tc.pip(home)
			if err := os.MkdirAll(pipCache, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pipCache, "x"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			xdgConf := filepath.Join(filepath.Dir(home), "xdgconf")
			f.HostGetenv = envOf(map[string]string{"HOME": home, "PIP_CACHE_DIR": pipCache, "XDG_CONFIG_HOME": xdgConf})
			var asked []string
			f.ScopeBreach = func(p string) bool { asked = append(asked, p); return tc.breach }
			d := dispositionOf(t, Plan(f), "pip-cache")
			if d.Code != tc.want {
				t.Errorf("code = %q (%s), want %q", d.Code, d.Reason, tc.want)
			}
			if tc.want == CodeUnsafeSource && (d.Aliased || strings.TrimSpace(d.Reason) == "") {
				t.Errorf("aliased=%v reason=%q", d.Aliased, d.Reason)
			}
			if tc.breach && len(asked) == 0 {
				t.Error("the scope-breach predicate was never consulted")
			}
		})
	}
	// The guard reads each store's resolved path, so it covers every store that
	// takes its variable whole — GOMODCACHE=$HOME here.
	f, _ := pathKeyedFacts(t)
	home := f.HostGetenv("HOME")
	f.HostGetenv = envOf(map[string]string{"HOME": home, "GOMODCACHE": home})
	if d := dispositionOf(t, Plan(f), "go-mod-cache"); d.Code != CodeUnsafeSource {
		t.Errorf("GOMODCACHE=$HOME: code %q, want %q", d.Code, CodeUnsafeSource)
	}
}

func TestStrandedFollowsTheJailRoot(t *testing.T) {
	f := Facts{JailCacheHost: "/s/cache", JailLocalHost: "/w/local", JailGoPathHost: "/w/go", JailMiseHost: "/s/mise"}
	for name, want := range map[string]string{
		"npm-cacache":    "/s/cache/npm/_cacache",
		"pnpm-store":     "/w/local/share/pnpm/store",
		"go-mod-cache":   "/w/go/pkg/mod",
		"cargo-registry": "/s/mise/cargo/registry",
		"mise-downloads": "/s/mise/downloads",
	} {
		s := storeNamed(t, name)
		if got := s.Stranded(f.rootHost(s.Root)); got != want {
			t.Errorf("%s stranded = %q, want %q", name, got, want)
		}
	}
}

// FORBIDDEN BEHAVIOUR: Plan decides, it does not provision. Creating a missing
// host store would alias an empty directory over whatever the jail has — the one
// outcome strictly worse than doing nothing — so this pins that no path in the
// decision touches the disk.
func TestPlanMutatesNothing(t *testing.T) {
	f, hostCache, jailCache := facts(t)
	root := filepath.Dir(hostCache)
	before := treeOf(t, root)
	// Every branch, including the ones that stat a missing path.
	Plan(f)
	f.HostCacheRoot = filepath.Join(hostCache, "does-not-exist")
	Plan(f)
	f.HostCacheRoot = jailCache
	Plan(f)
	if after := treeOf(t, root); !equal(before, after) {
		t.Errorf("Plan changed the filesystem\nbefore: %v\nafter:  %v", before, after)
	}
}

func treeOf(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CacheRoot's whole reason to exist is the seam, so the seam is what is tested:
// one implementation answering for two callers with different environments.
func TestCacheRoot(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	for _, tc := range []struct {
		name string
		in   map[string]string
		want string
	}{
		{"xdg wins", map[string]string{"XDG_CACHE_HOME": "/x/cache", "HOME": "/h"}, "/x/cache"},
		{"home fallback", map[string]string{"HOME": "/h"}, "/h/.cache"},
		{"relative xdg is ignored", map[string]string{"XDG_CACHE_HOME": "rel", "HOME": "/h"}, "/h/.cache"},
		{"nothing set", map[string]string{}, ""},
		{"relative home", map[string]string{"HOME": "rel"}, ""},
		{"trailing slash cleaned", map[string]string{"XDG_CACHE_HOME": "/x/cache/"}, "/x/cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CacheRoot(env(tc.in)); got != tc.want {
				t.Errorf("CacheRoot(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if got := CacheRoot(nil); got != "" {
		t.Errorf("CacheRoot(nil) = %q, want empty", got)
	}
}

// The jail is Linux whatever the host is, which is the fact the platform gate
// compares against. Stated as a test because the whole macOS refusal rests on
// it.
func TestJailPlatformIsAlwaysLinux(t *testing.T) {
	if !strings.HasPrefix(JailPlatform(), "linux/") {
		t.Errorf("JailPlatform() = %q, want a linux/<arch> value", JailPlatform())
	}
	if JailPlatform() == "" || HostPlatform() == "" {
		t.Error("platform helpers must never return an empty string — the gate reads " +
			"an empty value as a mismatch, which would silently disable the feature")
	}
}

func TestAliasedFiltersToTheEmittedMounts(t *testing.T) {
	ds := []Disposition{
		{Store: Store{Name: "a"}, Aliased: false},
		{Store: Store{Name: "b"}, Aliased: true},
		{Store: Store{Name: "c"}, Aliased: false},
	}
	got := Aliased(ds)
	if len(got) != 1 || got[0].Store.Name != "b" {
		t.Errorf("Aliased() = %+v, want only the aliased entry", got)
	}
	if len(Aliased(nil)) != 0 {
		t.Error("Aliased(nil) must be empty")
	}
}
