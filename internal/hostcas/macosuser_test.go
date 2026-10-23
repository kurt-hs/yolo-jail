package hostcas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// macosuser_test.go pins the macos-user run-as-you rows and gates
// (docs/design/macos-user-model-plan.md slice 8, MMP-D12).

func TestMacosUserSelfModeAliasesAPathKeyedStore(t *testing.T) {
	home := t.TempDir()
	f := Facts{
		Runtime: "macos-user", MacosUserSelf: true, IsMacOS: true,
		HostPlatform: "darwin/arm64", JailPlatform: "darwin/arm64",
		Ownership:  OwnershipYours,
		HostGetenv: func(k string) string { return map[string]string{"HOME": home}[k] },
	}
	probe := func(string) Presence { return Presence{Exists: true, IsDir: true, Writable: true} }
	d := decide(storeNamed(t, "pip-cache"), f, probe)
	if !d.Aliased {
		t.Fatalf("pip-cache not aliased in self mode: %s (%s)", d.Code, d.Reason)
	}
	if want := filepath.Join(home, "Library", "Caches", "pip"); d.Source != want {
		t.Errorf("source = %q, want the macOS default %q", d.Source, want)
	}
}

func TestMacosUserAccountModeDeclinesEveryStore(t *testing.T) {
	f := Facts{Runtime: "macos-user", IsMacOS: true, HostPlatform: "darwin/arm64", JailPlatform: "darwin/arm64"}
	for _, s := range Stores {
		if d := decide(s, f, func(string) Presence { return Presence{} }); d.Aliased || d.Code != CodeBackend {
			t.Errorf("%s in account mode: aliased=%v code=%s", s.Name, d.Aliased, d.Code)
		}
	}
	// Even with every other fact a self-mode launch carries: the MODE is the gate, so an
	// account-mode caller that set Ownership by mistake still aliases nothing.
	f.Ownership = OwnershipYours
	f.HostGetenv = envOf(map[string]string{"HOME": t.TempDir()})
	f.Probe = func(string) Presence { return Presence{Exists: true, IsDir: true, Writable: true} }
	for _, d := range Plan(f) {
		if d.Aliased || d.Code != CodeBackend {
			t.Errorf("%s in account mode with every other fact set: aliased=%v code=%s", d.Store.Name, d.Aliased, d.Code)
		}
	}
}

// The self-mode flag is macos-user's alone: it opens nothing on another backend, and podman
// keeps its own gates (a podman launch on macOS is still refused as macOS).
func TestMacosUserSelfIsIgnoredOffMacosUser(t *testing.T) {
	f, _, _ := facts(t)
	f.MacosUserSelf = true
	f.Runtime = "container"
	if d := one(t, f); d.Code != CodeBackend {
		t.Errorf("Apple Container with MacosUserSelf: code %q, want %q", d.Code, CodeBackend)
	}
	f.Runtime, f.IsMacOS = "podman", true
	if d := one(t, f); d.Code != CodeMacOS {
		t.Errorf("podman on macOS with MacosUserSelf: code %q, want %q", d.Code, CodeMacOS)
	}
}

// THE macOS ROWS, pinned. DarwinHost is each tool's documented macOS default, honoring the same
// variables as on Linux; DarwinRel is where the SANDBOX's own copy writes relative to its HOME,
// given that macos-user sets NPM_CONFIG_CACHE=$HOME/.cache/npm, GOPATH=$HOME/go and
// MISE_DATA_DIR=$HOME/.yolo/mise and none of the other variables.
func TestDarwinStorePaths(t *testing.T) {
	defaults := HostEnv{CacheRoot: "/h/.cache", Getenv: envOf(map[string]string{"HOME": "/h"})}
	overridden := HostEnv{CacheRoot: "/x/cache", Getenv: envOf(map[string]string{
		"HOME": "/h", "XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data",
		"npm_config_cache": "/o/npm", "PNPM_HOME": "/o/pnpm", "YARN_CACHE_FOLDER": "/o/yarn",
		"PIP_CACHE_DIR": "/o/pip", "UV_CACHE_DIR": "/o/uv", "GOCACHE": "/o/gocache",
		"GOMODCACHE": "/o/gomod", "CARGO_HOME": "/o/cargo", "MISE_DATA_DIR": "/o/mise",
	})}
	xdgOnly := HostEnv{CacheRoot: "/x/cache", Getenv: envOf(map[string]string{
		"HOME": "/h", "XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data",
	})}
	for _, tc := range []struct {
		name, host, hostOverridden, hostXDG, rel string
	}{
		{"pants-lmdb-store", "/h/.cache/pants/lmdb_store", "/x/cache/pants/lmdb_store", "/x/cache/pants/lmdb_store", ".cache/pants/lmdb_store"},
		{"npm-cacache", "/h/.npm/_cacache", "/o/npm/_cacache", "/h/.npm/_cacache", ".cache/npm/_cacache"},
		{"pnpm-store", "/h/Library/pnpm/store", "/o/pnpm/store", "/x/data/pnpm/store", "Library/pnpm/store"},
		{"pnpm-cache", "/h/Library/Caches/pnpm", "/x/cache/pnpm", "/x/cache/pnpm", "Library/Caches/pnpm"},
		{"yarn-cache", "/h/Library/Caches/Yarn", "/o/yarn", "/x/cache/yarn", "Library/Caches/Yarn"},
		{"pip-cache", "/h/Library/Caches/pip", "/o/pip", "/h/Library/Caches/pip", "Library/Caches/pip"},
		{"uv-cache", "/h/.cache/uv", "/o/uv", "/x/cache/uv", ".cache/uv"},
		{"go-build-cache", "/h/Library/Caches/go-build", "/o/gocache", "/h/Library/Caches/go-build", "Library/Caches/go-build"},
		{"go-mod-cache", "/h/go/pkg/mod", "/o/gomod", "/h/go/pkg/mod", "go/pkg/mod"},
		{"cargo-registry", "/h/.cargo/registry", "/o/cargo/registry", "/h/.cargo/registry", ".cargo/registry"},
		{"mise-downloads", "/h/.local/share/mise/downloads", "/o/mise/downloads", "/x/data/mise/downloads", ".yolo/mise/downloads"},
	} {
		s := storeNamed(t, tc.name)
		if s.DarwinHost == nil {
			t.Errorf("%s has no DarwinHost", tc.name)
			continue
		}
		if got := s.DarwinHost(defaults); got != tc.host {
			t.Errorf("%s: default macOS host path = %q, want %q", tc.name, got, tc.host)
		}
		if got := s.DarwinHost(overridden); got != tc.hostOverridden {
			t.Errorf("%s: overridden macOS host path = %q, want %q", tc.name, got, tc.hostOverridden)
		}
		if got := s.DarwinHost(xdgOnly); got != tc.hostXDG {
			t.Errorf("%s: XDG-only macOS host path = %q, want %q", tc.name, got, tc.hostXDG)
		}
		if s.DarwinRel != tc.rel {
			t.Errorf("%s: DarwinRel = %q, want %q", tc.name, s.DarwinRel, tc.rel)
		}
	}
	if len(Stores) != 11 {
		t.Errorf("Stores has %d entries and this table pins 11 — pin the new entry's macOS paths here", len(Stores))
	}
	for _, s := range Stores {
		if s.DarwinHost == nil || s.DarwinRel == "" {
			t.Errorf("%s: a store must declare its macOS row (DarwinHost and DarwinRel)", s.Name)
			continue
		}
		if filepath.IsAbs(s.DarwinRel) || strings.Contains(s.DarwinRel, "..") {
			t.Errorf("%s: DarwinRel %q must be relative to the sandbox HOME", s.Name, s.DarwinRel)
		}
		if got := s.DarwinHost(HostEnv{}); got != "" {
			t.Errorf("%s resolved %q on macOS from an empty environment", s.Name, got)
		}
	}
}

// selfFacts is a self-mode Facts value over a temp tree that passes every gate for pip-cache.
func selfFacts(t *testing.T) (Facts, string, string) {
	t.Helper()
	root := t.TempDir()
	hostHome := filepath.Join(root, "you")
	sandboxHome := filepath.Join(root, "ws", ".yolo", "home")
	mustDir(t, filepath.Join(hostHome, "Library", "Caches", "pip"))
	return Facts{
		Runtime: "macos-user", MacosUserSelf: true, IsMacOS: true,
		HostPlatform: "darwin/arm64", JailPlatform: "darwin/arm64",
		Ownership:     OwnershipYours,
		HostCacheRoot: filepath.Join(hostHome, ".cache"),
		HostGetenv:    envOf(map[string]string{"HOME": hostHome}),
		MacosUserHome: sandboxHome,
		Probe:         linuxLikeProbe,
	}, hostHome, sandboxHome
}

// mustDir makes a non-empty directory (the cold-start gate reads emptiness).
func mustDir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The destination is the path in the SANDBOX's HOME at DarwinRel, and the stranded private copy
// is that same path: the link replaces the sandbox's own directory.
func TestMacosUserSelfModeDestIsTheSandboxHomePath(t *testing.T) {
	f, hostHome, sandboxHome := selfFacts(t)
	d := dispositionOf(t, Plan(f), "pip-cache")
	if !d.Aliased {
		t.Fatalf("not aliased: %s (%s)", d.Code, d.Reason)
	}
	if want := filepath.Join(hostHome, "Library", "Caches", "pip"); d.Source != want {
		t.Errorf("source = %q, want %q", d.Source, want)
	}
	if want := filepath.Join(sandboxHome, "Library", "Caches", "pip"); d.Dest != want || d.Stranded != want {
		t.Errorf("dest = %q stranded = %q, want both %q", d.Dest, d.Stranded, want)
	}
	// With no sandbox home known (an inventory with no workspace) the destination is still
	// named, HOME-relative, and nothing is stranded.
	f.MacosUserHome = ""
	d = dispositionOf(t, Plan(f), "pip-cache")
	if d.Dest != "~/Library/Caches/pip" || d.Stranded != "" {
		t.Errorf("no sandbox home: dest = %q stranded = %q", d.Dest, d.Stranded)
	}
}

// EVERY EXISTING GATE STILL HOLDS in self mode: the platform, the ownership fact, the env-path
// guard (with ~/Library and its config dirs added on macOS), the relocation of a ~/.cache
// segment, absence, the cold start and the same tree.
func TestMacosUserSelfModeKeepsEveryGate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store string
		tweak func(t *testing.T, f *Facts, hostHome, sandboxHome string)
		want  Code
	}{
		{"platform mismatch", "pip-cache", func(_ *testing.T, f *Facts, _, _ string) { f.HostPlatform = "linux/arm64" }, CodePlatform},
		{"ownership unknown", "pip-cache", func(_ *testing.T, f *Facts, _, _ string) { f.Ownership = OwnershipUnknown }, CodeOwnershipUnknown},
		{"ownership not yours", "pip-cache", func(_ *testing.T, f *Facts, _, _ string) { f.Ownership = OwnershipNotYours }, CodeNotYours},
		{"no HOME", "pip-cache", func(_ *testing.T, f *Facts, _, _ string) { f.HostGetenv = envOf(nil) }, CodeNoCacheRoot},
		{"PIP_CACHE_DIR is the home", "pip-cache", func(_ *testing.T, f *Facts, h, _ string) {
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": h})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR is ~/Library", "pip-cache", func(_ *testing.T, f *Facts, h, _ string) {
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": filepath.Join(h, "Library")})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR is ~/Library/Caches", "pip-cache", func(_ *testing.T, f *Facts, h, _ string) {
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": filepath.Join(h, "Library", "Caches")})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR under ~/Library/Preferences", "pip-cache", func(t *testing.T, f *Facts, h, _ string) {
			p := filepath.Join(h, "Library", "Preferences", "pip")
			mustDir(t, p)
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": p})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR under ~/Library/Application Support", "pip-cache", func(t *testing.T, f *Facts, h, _ string) {
			p := filepath.Join(h, "Library", "Application Support", "pip")
			mustDir(t, p)
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": p})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR under ~/Library/Keychains", "pip-cache", func(t *testing.T, f *Facts, h, _ string) {
			p := filepath.Join(h, "Library", "Keychains", "pip")
			mustDir(t, p)
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": p})
		}, CodeUnsafeSource},
		{"PIP_CACHE_DIR under ~/.config", "pip-cache", func(t *testing.T, f *Facts, h, _ string) {
			p := filepath.Join(h, ".config", "pip")
			mustDir(t, p)
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": p})
		}, CodeUnsafeSource},
		{"scope breach", "pip-cache", func(_ *testing.T, f *Facts, _, _ string) {
			f.ScopeBreach = func(string) bool { return true }
		}, CodeUnsafeSource},
		{"source inside the sandbox home", "pip-cache", func(t *testing.T, f *Facts, h, s string) {
			p := filepath.Join(s, "pipc")
			mustDir(t, p)
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": p})
		}, CodeSameTree},
		{"absent", "pip-cache", func(_ *testing.T, f *Facts, h, _ string) {
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": filepath.Join(h, "nope")})
		}, CodeAbsent},
		{"relocated ~/.cache segment", "uv-cache", func(t *testing.T, f *Facts, h, _ string) {
			mustDir(t, filepath.Join(h, ".cache", "uv"))
			f.RelocatedSegments = []string{"uv"}
		}, CodeRelocated},
		{"cold start over a warm private copy", "pip-cache", func(t *testing.T, f *Facts, h, s string) {
			empty := filepath.Join(h, "emptypip")
			if err := os.MkdirAll(empty, 0o755); err != nil {
				t.Fatal(err)
			}
			f.HostGetenv = envOf(map[string]string{"HOME": h, "PIP_CACHE_DIR": empty})
			mustDir(t, filepath.Join(s, "Library", "Caches", "pip"))
		}, CodeWouldColdStart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, hostHome, sandboxHome := selfFacts(t)
			tc.tweak(t, &f, hostHome, sandboxHome)
			if d := dispositionOf(t, Plan(f), tc.store); d.Code != tc.want || d.Aliased {
				t.Errorf("code = %q aliased=%v (%s), want %q", d.Code, d.Aliased, d.Reason, tc.want)
			}
		})
	}
	// The control for the relocation row: uv aliases when nothing relocates it.
	f, hostHome, _ := selfFacts(t)
	mustDir(t, filepath.Join(hostHome, ".cache", "uv"))
	if d := dispositionOf(t, Plan(f), "uv-cache"); !d.Aliased {
		t.Errorf("uv-cache with no relocation: %s (%s)", d.Code, d.Reason)
	}
	// A store under ~/Library/Caches has no ~/.cache segment, so a ~/.cache/pip relocation
	// does not block it.
	f.RelocatedSegments = []string{"pip"}
	if d := dispositionOf(t, Plan(f), "pip-cache"); !d.Aliased {
		t.Errorf("a ~/.cache/pip relocation blocked ~/Library/Caches/pip: %s (%s)", d.Code, d.Reason)
	}
}

// The Linux decision ignores the macOS-only guard: a podman launch whose PIP_CACHE_DIR names a
// directory called Library under the home still aliases, exactly as before.
func TestTheMacosLibraryGuardIsMacosUserOnly(t *testing.T) {
	f, _ := pathKeyedFacts(t)
	home := f.HostGetenv("HOME")
	lib := filepath.Join(home, "Library")
	mustDir(t, lib)
	f.HostGetenv = envOf(map[string]string{"HOME": home, "PIP_CACHE_DIR": lib})
	if d := dispositionOf(t, Plan(f), "pip-cache"); !d.Aliased {
		t.Errorf("podman PIP_CACHE_DIR=~/Library: %s (%s), want aliased as before", d.Code, d.Reason)
	}
}

func TestDarwinJailPlatform(t *testing.T) {
	if p := DarwinJailPlatform(); !strings.HasPrefix(p, "darwin/") || strings.TrimPrefix(p, "darwin/") == "" {
		t.Errorf("DarwinJailPlatform() = %q, want darwin/<arch>", p)
	}
}
