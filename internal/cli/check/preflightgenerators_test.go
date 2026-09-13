package check

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE PREFLIGHT'S LIST OF GENERATORS IS HAND-MAINTAINED, AND A SHORTER ONE CANNOT FAIL.
//
// `yolo check`'s entrypoint preflight claims to run "the Go entrypoint generators … with the
// same YOLO_* environment the real jail boot uses". That claim is only worth something if the
// list is COMPLETE: a generator the boot runs and the preflight does not makes `yolo check`
// report green on a jail that boots differently, and nothing announces the gap — the preflight
// simply does less work and still passes.
//
// It had been wrong twice over. `GeneratePackageManagerLaunchers` was missing for as long as it
// has existed, and `DeliverLaunchFlags` from the day it landed (2026-09-13, the third launch-flag
// carrier). Both were found by a human reading the two lists side by side, which is exactly the
// review nobody performs twice.
//
// So the list is DERIVED here rather than restated: this test reads boot.go's `genStep` calls —
// the authority on what a boot generates — and requires every one of them to be either in the
// preflight's list or in `deliberatelyNotPreflighted` below, with a reason. A generator added to
// the boot and nowhere else fails this test until somebody makes that choice explicitly.
//
// Both sides are read from SOURCE because the preflight's list is a local variable inside
// runEntrypointPreflight and cannot be reached from a test any other way. That makes this a
// text-matching test, with the usual weakness: it is pinned to the shape of those two
// expressions. The shape is stable (a genStep line and a composite literal of function values),
// and a wrong answer here fails loudly rather than silently, which is the trade being made.
func TestThePreflightRunsEveryBootGenerator(t *testing.T) {
	// Boot steps the preflight deliberately skips. A generator belongs here only with a
	// reason that survives review — "it was already missing" is not one.
	deliberatelyNotPreflighted := map[string]string{
		"GenerateStorePackages": "opt-in per launch (YOLO_STORE_PACKAGES) and writes a symlink " +
			"farm under /run, which a temp-home dry run has nowhere to put",
		"ConfigureHostFiles": "renders HOST files, which a preflight must never touch — the " +
			"whole point of the temp home is that the dry run cannot reach them",
		"RemoveStaleGeneratedClients": "a REMOVER, not a generator: it deletes retired clients " +
			"from a real home, and there is nothing stale in a fresh temp dir",
	}

	bootGens := generatorsNamedIn(t, "../../entrypoint/boot.go",
		regexp.MustCompile(`genStep\(e, "[a-z_]+", func\(\) error \{ return ([A-Z]\w+)\(e\) \}\)`))
	if len(bootGens) < 8 {
		t.Fatalf("read only %d genStep generators out of boot.go (%v) — the call shape this "+
			"test matches has changed, so it is no longer reading the authority it claims to",
			len(bootGens), bootGens)
	}

	preflightGens := generatorsNamedIn(t, "entrypoint.go",
		regexp.MustCompile(`\n\t\tentrypoint\.([A-Z]\w+),`))
	if len(preflightGens) < 5 {
		t.Fatalf("read only %d generators out of the preflight list (%v) — the literal this "+
			"test matches has changed", len(preflightGens), preflightGens)
	}

	inPreflight := map[string]bool{}
	for _, g := range preflightGens {
		inPreflight[g] = true
	}

	var missing []string
	for _, g := range bootGens {
		if inPreflight[g] {
			continue
		}
		if _, ok := deliberatelyNotPreflighted[g]; ok {
			continue
		}
		missing = append(missing, g)
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("the boot runs %v and the `yolo check` preflight does not.\n\n"+
			"A preflight that skips a generator reports on a DIFFERENT jail than the one that "+
			"boots, and it passes while doing so — a shorter list cannot fail.\n\n"+
			"Add each to the `generators` slice in runEntrypointPreflight (in boot's order), or "+
			"to deliberatelyNotPreflighted in this test WITH the reason it cannot run in a temp "+
			"home.", missing)
	}

	// The other direction: a name in the preflight that the boot no longer runs is a preflight
	// testing something no jail does.
	inBoot := map[string]bool{}
	for _, g := range bootGens {
		inBoot[g] = true
	}
	for _, g := range preflightGens {
		if !inBoot[g] {
			t.Errorf("the preflight runs entrypoint.%s and boot.go does not — the preflight is "+
				"exercising something no jail boot performs", g)
		}
	}
}

// generatorsNamedIn returns the first capture group of every match of re in the named file,
// deduplicated and sorted. Paths are relative to this package's directory.
func generatorsNamedIn(t *testing.T, path string, re *regexp.Regexp) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, which this test derives its answer from: %v", path, err)
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		name := strings.TrimSpace(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
