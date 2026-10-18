package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/darwinpkg"
)

// THE FIRST TEST BEHIND THE macos-user GATE, and the root of the runbook's dependency
// chain: docs/plans/runbooks/macos-user-manual-checks.md item 6.
//
// WHAT IT SETTLES. `OQ-P1`'s FLOOR — the native darwin closure every macos-user launch
// builds, so the sandbox has mise, node, git, ripgrep and the rest of what the container
// image bakes (darwinpkg.FloorNames(): that image core, minus what darwin cannot build
// and minus the GNU userland OQ-P2 rules out; docs/reference/macos-user-provisioning.md, "The floor")
// — is the single largest unmeasured claim on this backend. Nothing has ever observed it
// reaching a sandbox's PATH. Its other half is `OQ-P2`'s ruling that the floor carries
// NO GNU userland, which is a claim about the shipped article and not about the
// exclusion list a unit test can read.
//
// WHY IT HAS TO BE AN INTEGRATION TEST, in this repo's own terms: the unit suite pins
// darwinpkg.FloorNames() and the flake attribute it mirrors, and would stay green if
// the profile never reached the sandbox at all. Three things compose here and none of
// them is visible from a pure function — nix builds the closure natively, the launch
// puts its bin dir on YOLO_DARWIN_LOGIN_PATH, and the generated login rc files
// re-prepend that AHEAD of macOS's own path_helper. A Homebrew or /usr/bin answer
// below means the re-prepend lost, which is the failure the runbook's item 3 was
// written to catch and the one this repeats every run.
//
// IF THIS FAILS, STOP. The runbook says so and it is a real dependency, not a
// courtesy: no floor means no `mise` and no `npm`, so the provisioning stage's first
// line fails, so `mise_tools` and `lsp_servers` install nothing. Reporting those as
// separate bugs is reporting one bug four times.
func TestMacosUserFloorReachesTheSandboxPath(t *testing.T) {
	requireMacosUser(t)

	r := runMacosUser(t, macosUserWorkspace(t, `{}`), macosUserFloorProbe(macosUserFloorProbeBins()))
	if r.rc != 0 {
		t.Fatalf("the macos-user launch failed (rc %d) before any floor probe could "+
			"answer. Runbook item 6 asks for `yolo --dry-run` output alongside this, "+
			"since it names the profile path the launch built.\nstdout:\n%s\nstderr:\n%s",
			r.rc, r.stdout, r.stderr)
	}
	got := section(r.stdout, "=== FLOOR ===", "=== END ===")
	if strings.TrimSpace(got) == "" {
		t.Fatalf("the sandbox produced no probe output at all, so nothing below can be "+
			"read as a floor result.\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}

	resolved := parseFloorProbe(got)
	home := resolved[floorProbeHomeKey]
	if home == "" {
		t.Fatalf("the probe did not print the sandbox's HOME, so no resolution below can be "+
			"classified:\n%s", got)
	}
	for _, b := range macosUserFloorBins {
		full, seen := resolved[b]
		floor, floorSeen := resolved[b+floorProbeFloorSuffix]
		if !seen || !floorSeen {
			t.Errorf("%s: the probe printed no line for it at all", b)
			continue
		}
		problem, note := floorBinProblem(b, full, floor, home, "/nix/store/")
		if note != "" {
			t.Log(note)
		}
		if problem != "" {
			t.Errorf("%s (runbook item 6; %d floor entries were built)", problem, len(darwinpkg.FloorNames()))
		}
	}
	// THE POLICY HALF, one probe per excluded package rather than one for the whole
	// ruling. `which` alone was the runbook's spelling and it settles `which` alone;
	// OQ-P2 is a claim about NINE packages, and the eight it does not name are the ones
	// whose flags differ most from the Mac's own (`sed -i`, `find -printf`,
	// `tar --wildcards`). "Absent" and "the system's copy" are both passes here — only a
	// store path is a failure, because the exclusion's whole job is to leave macOS's own
	// binary in place. Both resolutions are asked: the agent's, and the one past the sandbox
	// home's own directories, which is the floor profile's alone.
	for pkg, bin := range floorPolicyProbes {
		full, seen := resolved[bin]
		floor, floorSeen := resolved[bin+floorProbeFloorSuffix]
		if !seen || !floorSeen {
			t.Errorf("%s: the probe printed no line for it at all", bin)
			continue
		}
		path := full
		if !strings.HasPrefix(path, "/nix/store/") {
			path = floor
		}
		if strings.HasPrefix(path, "/nix/store/") {
			t.Errorf("%s resolved to %s — the GNU userland the floor is supposed to "+
				"exclude, from the `%s` entry of darwinpkg.FloorExcludedPolicy (OQ-P2). "+
				"The exclusion list is right and the SHIPPED PROFILE does not honour it, "+
				"which is exactly the gap a unit test over the list cannot see: the agent "+
				"is now getting GNU `%s` on somebody's Mac, where every flag that differs "+
				"from the BSD copy is a silent behaviour change.", bin, path, pkg, bin)
		}
	}
}

// macosUserFloorBins is the FLOOR half of item 6's probe: names that must resolve, and
// must resolve into the nix profile.
//
// They are the runbook's, which is the spec, and they are spelled here rather than
// derived from darwinpkg.FloorNames() because a floor ENTRY is not a binary name —
// `nodejs_24` ships `node` and `npm`, `ripgrep` ships `rg`, and `cacert`, `zlib` and
// `tzdata` ship no command at all — so a derived list would need a mapping table that is
// itself the thing nobody checks.
var macosUserFloorBins = []string{"mise", "node", "npm", "git", "rg", "fd", "jq", "gh", "curl"}

// floorPolicyProbes maps each darwinpkg.FloorExcludedPolicy entry to the command it would
// put on the sandbox's PATH if the exclusion failed to take effect.
//
// A MAPPING TABLE IS UNAVOIDABLE HERE for macosUserFloorBins' reason — a package name is
// not a binary name — so the table is written down and then GATED: the Linux-side test
// below fails if it and the exclusion list ever disagree. That is the half a derived list
// could not have, and the half a hand-maintained list normally lacks.
//
// The binary chosen for each is one macOS ships its own copy of, and (deliberately) not
// `ls`: yolo's generated bashrc defines `alias ls=…`, and `command -v` reports an alias
// rather than a path. `stat` stands for coreutils instead — same package, no alias, and
// its BSD and GNU spellings share almost no flags.
var floorPolicyProbes = map[string]string{
	"coreutils-full": "stat",
	"findutils":      "find",
	"gnused":         "sed",
	"gnugrep":        "grep",
	"gawk":           "awk",
	"gnupatch":       "patch",
	"diffutils":      "diff",
	"gnutar":         "tar",
	"which":          "which",
}

// floorPolicyBins returns the probe binaries in a stable order (map iteration is not one,
// and a probe script that reorders itself between runs makes two CI logs undiffable).
func floorPolicyBins() []string {
	out := make([]string, 0, len(floorPolicyProbes))
	for _, bin := range floorPolicyProbes {
		out = append(out, bin)
	}
	sort.Strings(out)
	return out
}

// macosUserFloorProbeBins is EVERY name item 6's one launch asks about: the floor
// binaries that must come from the store, then the policy binaries that must not.
//
// It is a function rather than two lists spliced at the call site because it is what the
// Linux-side test below reaches for. Delete `floorPolicyBins()` from it and the probe
// silently stops asking the policy question while the assertion loop above finds nothing
// to complain about — the "green because it asked nothing" shape again, and the reason
// that test renders the REAL script instead of trusting this to stay wired.
func macosUserFloorProbeBins() []string {
	return append(append([]string{}, macosUserFloorBins...), floorPolicyBins()...)
}

// macosUserFloorProbe renders the probe script: one `name=<resolved path>` line per
// binary, fenced so section() can find it in the launch's stdout.
//
// `command -v` rather than `which --version`: the runbook spells the policy half as
// `which --version` printing nothing useful, which depends on what Apple's `which` does
// with a flag it does not know. The RESOLVED PATH says the same thing and depends on
// nothing (OQ-P2).
//
// TWO LINES PER BINARY. `name=` is what the agent gets, on the sandbox's whole PATH. `name@floor=`
// is the same lookup on that PATH with every directory under the sandbox's $HOME taken out, and
// the HOME it took out is printed first. Those directories — the mise shims among them — head the
// PATH by design and outrank the floor (entrypoint.HomePathDirs, OQ-PD27; on mise, OQ-DM3 in
// docs/design/macos-user-suite-on-a-developer-mac.md, ruled 2026-10-18 for parity with the jail),
// and the sandbox home is ONE account home shared by every workspace and test on the Mac: a mise
// tool another workspace installed leaves a shim there that shadows the floor's `node` whatever
// this workspace configures. So `@floor` is the floor's own reach, which no stale install can
// hide, and `name=` is checked only for a winner outside the home.
func macosUserFloorProbe(bins []string) string {
	var probe strings.Builder
	probe.WriteString(`echo "=== FLOOR ==="` + "\n")
	fmt.Fprintf(&probe, "printf '%s=%%s\\n' \"$HOME\"\n", floorProbeHomeKey)
	probe.WriteString(`floor_path=$(IFS=:; out=; for d in $PATH; do case "$d" in "$HOME"|"$HOME"/*) ;; ` +
		`*) out="${out:+$out:}$d" ;; esac; done; printf '%s' "$out")` + "\n")
	for _, b := range bins {
		fmt.Fprintf(&probe, "printf '%s=%%s\\n' \"$(command -v %s || echo MISSING)\"\n", b, b)
		fmt.Fprintf(&probe, "printf '%s%s=%%s\\n' \"$(PATH=$floor_path; command -v %s || echo MISSING)\"\n",
			b, floorProbeFloorSuffix, b)
	}
	probe.WriteString(`echo "=== END ==="` + "\n")
	return probe.String()
}

// floorProbeHomeKey and floorProbeFloorSuffix name the probe's extra lines: the sandbox's HOME, and
// a binary's resolution past the home's directories.
const (
	floorProbeHomeKey     = "@home"
	floorProbeFloorSuffix = "@floor"
)

// parseFloorProbe reads the probe's `name=path` lines.
func parseFloorProbe(out string) map[string]string {
	resolved := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if name, path, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			resolved[name] = path
		}
	}
	return resolved
}

// floorBinProblem judges one floor binary from its two resolutions: full, on the agent's PATH, and
// floor, past the sandbox home's directories; store is the nix store's prefix. problem is "" when
// the floor reached the PATH; note records a home directory that outranks it, which is by design.
//
//   - floor MISSING: the floor did not reach the PATH.
//   - floor outside the store: something ahead of the floor that is NOT the sandbox home's —
//     macOS's path_helper, a Homebrew prefix — beat the login rc files' re-prepend. The one
//     failure path_helper and Homebrew are named for.
//   - full under the home: a mise shim (or another home directory) outranks the floor, as on the
//     container jail, where /mise/shims sits ahead of /bin. A note, not a failure.
//   - full anywhere else but floor's own path: an outside directory beat the floor on the
//     agent's PATH while the floor lookup did not see it — the re-prepend lost to it.
func floorBinProblem(bin, full, floor, home, store string) (problem, note string) {
	inHome := func(p string) bool { return p == home || strings.HasPrefix(p, home+"/") }
	switch {
	case floor == "MISSING":
		return fmt.Sprintf("%s does not resolve in the sandbox past its home's own directories — the "+
			"floor did not reach this PATH", bin), ""
	case !strings.HasPrefix(floor, store):
		return fmt.Sprintf("%s resolved to %s rather than into the nix profile, with the sandbox home's "+
			"directories (the mise shims among them) taken out of the lookup. The floor is on PATH but "+
			"LOST: macOS's path_helper or a Homebrew prefix is ahead of the login rc files' re-prepend "+
			"of $YOLO_DARWIN_LOGIN_PATH, so the agent gets the host's copy of a tool yolo promised to "+
			"supply.", bin, floor), ""
	case full == floor:
		return "", ""
	case inHome(full):
		kind := "a directory of the sandbox home"
		if strings.Contains(full, "/mise/shims/") {
			kind = "a mise shim"
		}
		return "", fmt.Sprintf("%s: the agent gets %s, %s, which outranks the floor's %s by design "+
			"(mise and the home's directories ahead of the floor, as in the container jail: OQ-PD27, "+
			"OQ-DM3). The sandbox home is shared by every workspace on this Mac, so it may be another "+
			"workspace's install.", bin, full, kind, floor)
	default:
		return fmt.Sprintf("%s: the agent gets %s, outside the sandbox home and not the floor's %s. "+
			"The floor is on PATH but LOST to that directory: macOS's path_helper or a Homebrew prefix "+
			"is ahead of the login rc files' re-prepend of $YOLO_DARWIN_LOGIN_PATH.", bin, full, floor), ""
	}
}

// TestMacosUserFloorProbeAsksEveryQuestionItem6Claims is the ungated half of item 6, and
// it runs HERE — Linux, under -short, on the machine that develops this repo.
//
// It is not behind requireMacosUser and so is not in the vacuity ledger, which is correct:
// it exercises no backend. What it exercises is the TABLE above, whose failure mode is
// silent. Add a package to darwinpkg.FloorExcludedPolicy without a probe and the Mac test
// keeps passing while saying nothing about the new entry — the same "green because it
// asked nothing" this suite's gate exists to stop, one level down.
//
// It carries the TestMacosUser prefix so `-run '^TestMacosUser'` selects it too: a macOS
// job that runs the suite gets this check for free, and an ordinary Linux `just test-fast`
// gets it long before any Mac does.
func TestMacosUserFloorProbeAsksEveryQuestionItem6Claims(t *testing.T) {
	excluded := map[string]struct{}{}
	for _, pkg := range darwinpkg.FloorExcludedPolicy {
		excluded[pkg] = struct{}{}
		if _, ok := floorPolicyProbes[pkg]; !ok {
			t.Errorf("darwinpkg.FloorExcludedPolicy names %q and floorPolicyProbes has no "+
				"probe for it, so the macos-user floor test would pass without ever asking "+
				"whether %q's binaries reached the sandbox. Add the command name it "+
				"installs (OQ-P2, runbook item 6).", pkg, pkg)
		}
	}
	floor := map[string]struct{}{}
	for _, name := range darwinpkg.FloorNames() {
		floor[name] = struct{}{}
	}
	// THE CALL SITE, pinned. Everything above is a fact about a table, and a table
	// nothing reads is not a test (AGENTS.md: "does it fail if I delete the call site?").
	// So the REAL probe script is rendered from the REAL list and every name is required
	// to appear in it — which is what turns "floorPolicyBins() was dropped from
	// macosUserFloorProbeBins" from a silent loss into a red Linux run.
	//
	// ⚠ THE EXPECTED LIST IS SPELLED AGAIN HERE ON PURPOSE. Calling
	// macosUserFloorProbeBins() for both sides would make the two agree by construction
	// and assert nothing — the mutation above would pass. Do not "simplify" it.
	script := macosUserFloorProbe(macosUserFloorProbeBins())
	for _, bin := range append(append([]string{}, macosUserFloorBins...), floorPolicyBins()...) {
		if !strings.Contains(script, "command -v "+bin+" ") {
			t.Errorf("the probe item 6 actually runs never asks about %q. The launch would "+
				"come back green having measured one binary fewer than this file claims "+
				"(macosUserFloorProbeBins).\nscript:\n%s", bin, script)
		}
	}
	for pkg, bin := range floorPolicyProbes {
		if _, ok := excluded[pkg]; !ok {
			t.Errorf("floorPolicyProbes probes %q for %q, but %q is no longer on "+
				"darwinpkg.FloorExcludedPolicy — so the test asserts that a package the "+
				"floor may now SHIP must not come from the store. Drop the probe, or put "+
				"the package back on the exclusion list.", bin, pkg, pkg)
		}
		if _, ok := floor[pkg]; ok {
			t.Errorf("%q is both on the floor and probed as excluded: the two halves of "+
				"the item 6 assertion now contradict each other for %q.", pkg, bin)
		}
	}
}

// TestMacosUserFloorProbeSeesTheFloorPastAMiseShim runs the REAL probe script here, under bash,
// over a stand-in sandbox: a home whose mise shims dir holds a stale `jq` shim ahead of a stand-in
// floor's `jq`, and a floor `git` nothing shadows. The agent's lookup must name the shim and the
// floor lookup the floor's binary, and floorBinProblem must call the shim a note (mise wins, by
// OQ-DM3) and the reach a pass. That is the half a Mac cannot check before it fails: whether the
// probe's PATH filter and its two lines do what the Mac test reads them as.
func TestMacosUserFloorProbeSeesTheFloorPastAMiseShim(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on PATH")
	}
	root := resolvedTempDir(t)
	home := filepath.Join(root, "home")
	shims := filepath.Join(home, ".yolo", "mise", "shims")
	store := filepath.Join(root, "store") + "/"
	floorBin := filepath.Join(store, "profile", "bin")
	for _, d := range []string{shims, floorBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(shims, "jq"), filepath.Join(floorBin, "jq"), filepath.Join(floorBin, "git")} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bash, "--noprofile", "--norc", "-c",
		macosUserFloorProbe([]string{"jq", "git", "yolo-no-such-tool"}))
	cmd.Env = []string{"HOME=" + home, "PATH=" + shims + ":" + floorBin}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	got := parseFloorProbe(section(string(out), "=== FLOOR ===", "=== END ==="))
	want := map[string]string{
		floorProbeHomeKey:             home,
		"jq":                          filepath.Join(shims, "jq"),
		"jq" + floorProbeFloorSuffix:  filepath.Join(floorBin, "jq"),
		"git":                         filepath.Join(floorBin, "git"),
		"git" + floorProbeFloorSuffix: filepath.Join(floorBin, "git"),
		"yolo-no-such-tool":           "MISSING",
		"yolo-no-such-tool@floor":     "MISSING",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("the probe printed %s=%q, want %q:\n%s", k, got[k], v, out)
		}
	}
	if problem, note := floorBinProblem("jq", got["jq"], got["jq@floor"], home, store); problem != "" ||
		!strings.Contains(note, "a mise shim") {
		t.Errorf("a floor jq behind a mise shim: problem %q, note %q; want a pass with a note naming "+
			"the shim", problem, note)
	}
	if problem, note := floorBinProblem("git", got["git"], got["git@floor"], home, store); problem != "" || note != "" {
		t.Errorf("an unshadowed floor git: problem %q, note %q; want a silent pass", problem, note)
	}
	if problem, _ := floorBinProblem("yolo-no-such-tool", "MISSING", "MISSING", home, store); !strings.Contains(problem, "did not reach") {
		t.Errorf("a missing floor binary is not reported as the floor missing: %q", problem)
	}
	// The one case path_helper and Homebrew are blamed for: past the home, the floor still lost.
	problem, _ := floorBinProblem("jq", "/opt/homebrew/bin/jq", "/opt/homebrew/bin/jq", home, store)
	if !strings.Contains(problem, "path_helper") || !strings.Contains(problem, "mise shims among them") {
		t.Errorf("a Homebrew jq ahead of the floor is not reported as the re-prepend losing: %q", problem)
	}
}
