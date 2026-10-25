package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// reaperstore_test.go keeps a launch that runs the automatic reapers away from the machine's
// store.
//
// # Why
//
// The suite keeps the reapers off (applySuiteEnv). A test whose subject is one of them turns them
// back on for its launch (withAutoReapers), and one variable gates every class, so that launch runs
// every class that is due. The run store (runstore_test.go) links the machine's build/, locks/,
// cache/, embedded-packs/ and stores/ back, so before this file such a launch reaped the machine's
// own: on 2026-10-24 one run of TestTheSlotReapsLeftoverScratchVolumes removed about 630 build
// links, 938 prefix roots and 221 embedded pack trees from the developer's store, and stamped the
// classes so they would not run there again that day.
//
// # How
//
// isolateReaperStore gives the test a HOME whose yolo store is a directory of its own with none of
// those links, so a class that reaps the store can only reap the test's copy. Two things do not
// live in the store, and isolation cannot reach them: the container runtime (images, scratch
// volumes) and, on a host, the nix store. A private build/ would even widen the image class,
// whose retention set is the build/ pointers of every workspace. So every debounced class gets a
// fresh stamp in the private build/, which makes it not due, except the classes the test names.
// What still runs is the named classes and the scratch-volume class, which has no debounce.
//
// runLaunch refuses a launch with the reapers on whose HOME is not isolated this way
// (reaperLaunchIsolation), so a new test cannot skip the step.

// reaperStampNames are the debounce stamps, under BuildDir(), of every class the launch slot runs
// on a daily stamp: the image reap's own, then one per classDebounce class.
// TestReaperStampNamesCoverEveryDebouncedClass fails when a class is added to internal/cli/run
// without being listed here.
var reaperStampNames = []string{
	"last-image-reap",
	"last-build-state-reap",
	"last-embedded-packs-reap",
	"last-store-outputs-reap",
	"last-cache-reap",
	"last-mise-versions-reap",
	"last-small-classes-reap",
	"last-orphan-cname-state-reap",
	"last-image-tars-reap",
	"last-bundle-generations-reap",
	"last-dev-bundles-reap",
	"last-loophole-state-reap",
}

// isolateReaperStore points HOME at a fresh home whose yolo store is private and returns that
// store. Every class in reaperStampNames is stamped as having just run, except the classes named
// in due (by their classDebounce name, such as run.LoopholeStateHousekeepingClass).
//
// Two things are carried over so the launch does not redo machine-wide work: the runtime's load
// sentinel, copied (so an image already loaded is not loaded again), and the machine's
// image-copy lock, linked (so a load that does happen still waits for another run's copy into
// the same runtime store).
func isolateReaperStore(t *testing.T, due ...string) string {
	t.Helper()
	machineHome := hostHome
	if machineHome == "" {
		machineHome = os.Getenv("HOME")
	}
	userConfig := "{}"
	if b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail", "config.jsonc")); err == nil {
		userConfig = string(b)
	}
	home := resolvedTempDir(t)
	if err := seedPackHome(home, machineHome, userConfig); err != nil {
		t.Fatalf("seeding the reaper test's HOME: %v", err)
	}
	// The store the launches have been using: the run store, or the machine's when there is none.
	shared := sharedStoreTarget(machineHome, yoloStoreRel)
	state := paths.GlobalStorageUnder(home)
	if err := os.Remove(state); err != nil {
		t.Fatalf("replacing the reaper test's yolo store link: %v", err)
	}
	build := filepath.Join(state, "build")
	locks := filepath.Join(state, "locks")
	for _, d := range []string{build, locks} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fresh := []byte(strconv.FormatInt(time.Now().Unix(), 10) + "\n")
	for _, name := range reaperStampNames {
		if slices.ContainsFunc(due, func(class string) bool { return name == "last-"+class+"-reap" }) {
			continue
		}
		if err := os.WriteFile(filepath.Join(build, name), fresh, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt := detectRuntime()
	if rt != "" {
		sentinel := "last-load-" + rt
		if b, err := os.ReadFile(filepath.Join(shared, "build", sentinel)); err == nil {
			if err := os.WriteFile(filepath.Join(build, sentinel), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	machineLocks := filepath.Join(shared, "locks")
	if err := os.MkdirAll(machineLocks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := userSymlink(filepath.Join(machineLocks, "image-copy.lock"), filepath.Join(locks, "image-copy.lock")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Cleanup(func() { removeWorkspaceTree(t, home) })
	return state
}

// reapersOn reports whether a launch with this environment runs the automatic reapers: the
// last entry for the opt-out wins, and only a non-empty value turns them off.
func reapersOn(env []string) bool {
	on := true
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, autoReapersOffEnv+"="); ok {
			on = v == ""
		}
	}
	return on
}

// reaperLaunchIsolation is the check runLaunch makes before a launch with the reapers on: the
// launch's yolo store must be a real directory, not one of the suite's links to a shared store,
// and none of its children may be a link either, since build/, locks/, embedded-packs/, cache/
// and stores/ are exactly what the run store links back to the machine's. The one link allowed
// is a FILE inside locks/ (the image-copy lock isolateReaperStore links on purpose), which no
// reaper deletes through.
func reaperLaunchIsolation(home string) error {
	state := paths.GlobalStorageUnder(home)
	fi, err := os.Lstat(state)
	if err != nil {
		return fmt.Errorf("its yolo store %s cannot be read (%v)", state, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(state)
		return fmt.Errorf("its yolo store %s is a link to %s, a store other launches use", state, target)
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		return fmt.Errorf("its yolo store %s cannot be listed (%v)", state, err)
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(filepath.Join(state, e.Name()))
			return fmt.Errorf("its yolo store's %s is a link to %s, a store other launches use", e.Name(), target)
		}
	}
	return nil
}

// reaperLaunchRefusal is runLaunch's refusal of a launch whose env turns the reapers on for a HOME
// that is not isolated, or "" when the launch may run.
func reaperLaunchRefusal(env []string) string {
	if !reapersOn(env) {
		return ""
	}
	if err := reaperLaunchIsolation(launchHome(env)); err != nil {
		return fmt.Sprintf("refusing a launch with the automatic reapers on: %v. Its housekeeping slot "+
			"would reap that shared store. Call isolateReaperStore(t, <the class under test>) before "+
			"the launch.", err)
	}
	return ""
}

// TestAReaperLaunchMustHaveAPrivateStore pins the guard and its call site under -short: a launch
// with the reapers on and the suite's linked store is refused before the binary runs, and the
// same launch after isolateReaperStore runs.
func TestAReaperLaunchMustHaveAPrivateStore(t *testing.T) {
	machineHome := resolvedTempDir(t)
	t.Setenv("HOME", machineHome)
	saved := hostHome
	hostHome = ""
	t.Cleanup(func() { hostHome = saved })
	isolateHome(t, "{}")
	if !reapersOn([]string{autoReapersOffEnv + "=1", autoReapersOffEnv + "="}) ||
		reapersOn([]string{autoReapersOffEnv + "=", autoReapersOffEnv + "=1"}) || !reapersOn(nil) {
		t.Fatal("reapersOn does not take the last entry for the opt-out")
	}
	if err := reaperLaunchIsolation(os.Getenv("HOME")); err == nil {
		t.Fatal("an isolated home whose yolo store is the suite's link passed the reaper guard")
	}

	// THE CALL SITE: runLaunch makes the check, before the binary runs.
	dir := resolvedTempDir(t)
	fake := filepath.Join(dir, "fake-yolo")
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	savedBin := yoloBin
	yoloBin = fake
	t.Cleanup(func() { yoloBin = savedBin })
	if _, refused := runLaunch(t, dir, []string{"run", "--", "true"}, withAutoReapers()); refused == nil ||
		!strings.Contains(refused.summary, "isolateReaperStore") {
		t.Errorf("runLaunch did not refuse a launch with the reapers on against the suite's linked store (%v)", refused)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the refused launch ran the yolo binary anyway")
	}

	state := isolateReaperStore(t, "loophole-state")
	if err := reaperLaunchIsolation(os.Getenv("HOME")); err != nil {
		t.Fatalf("isolateReaperStore's home fails the guard: %v", err)
	}
	if r := runCommand(t, dir, []string{"run", "--", "true"}, withAutoReapers()); r.rc != 0 {
		t.Errorf("the isolated launch failed: rc %d %s", r.rc, r.combined())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the isolated launch did not run the yolo binary")
	}
	for _, name := range reaperStampNames {
		_, err := os.Stat(filepath.Join(state, "build", name))
		if name == "last-loophole-state-reap" {
			if err == nil {
				t.Errorf("the class under test was stamped, so its launch would skip it: %s", name)
			}
		} else if err != nil {
			t.Errorf("%s was not stamped, so the launch would run that class: %v", name, err)
		}
	}
}

// TestReaperStampNamesCoverEveryDebouncedClass reads internal/cli/run for every class the slot
// debounces, so a new class cannot be left out of reaperStampNames, and with it run against the
// machine's runtime or nix store from a reaper test's launch.
func TestReaperStampNamesCoverEveryDebouncedClass(t *testing.T) {
	src := filepath.Join("..", "internal", "cli", "run")
	files, err := filepath.Glob(filepath.Join(src, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("listing %s: %v", src, err)
	}
	var all strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(b)
	}
	text := all.String()
	calls := regexp.MustCompile(`classDebounce\(([A-Za-z_][A-Za-z0-9_]*|"[^"]+")\)`).FindAllStringSubmatch(text, -1)
	if len(calls) < 5 {
		t.Fatalf("found only %d classDebounce calls in %s: the pattern no longer matches the code", len(calls), src)
	}
	for _, c := range calls {
		class := c[1]
		if !strings.HasPrefix(class, `"`) {
			m := regexp.MustCompile(`\b` + class + `\s*=\s*"([^"]+)"`).FindStringSubmatch(text)
			if m == nil {
				continue // the parameter in classDebounce's own body, not a call site
			}
			class = m[1]
		} else {
			class = strings.Trim(class, `"`)
		}
		if !slices.Contains(reaperStampNames, "last-"+class+"-reap") {
			t.Errorf("the slot debounces the class %q, which reaperStampNames does not stamp: a reaper "+
				"test's launch would run it", class)
		}
	}
	prune, err := os.ReadFile(filepath.Join("..", "internal", "prune", "autoreap.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prune), `autoReapSentinelName = "last-image-reap"`) {
		t.Error("the image reap's stamp is no longer last-image-reap; update reaperStampNames")
	}
}
