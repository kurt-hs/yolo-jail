package run

// patchfailures_test.go pins A JAIL LAUNCH'S PATCH FAILURE (patchfailures.go; docs/design/
// patched-forks.md PF-D81, PF-D83) at its call site, from Run's own entry point: a selected patched
// fork or a loaded patched extension whose series does not apply refuses a fresh launch even when an
// intact admitted build of it would serve, its error block first and copyable; the patch failure's
// own bypass runs that build for the launch, said; the missing-program hatch does not waive it.

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const pfCommit = "c5c0f6bd0123456789abcdef0123456789abcdef"

func pfFailure() *packsrc.PatchFailure {
	return &packsrc.PatchFailure{Owner: "forkpack/tool", Target: packsrc.ListEntry{Commit: pfCommit, Tag: "v1.2.0"},
		Kind: "conflict", Member: "0001-ten.patch", Paths: []string{"f.txt"}}
}

// withEnv sets one variable in o's environment, as the user's shell would.
func withEnv(o *Options, key, value string) {
	prev := o.Getenv
	o.Getenv = func(k string) string {
		if k == key {
			return value
		}
		return prev(k)
	}
}

// servingForkWithFailure is the slot's answer for a patched fork whose newer upstream does not take
// the series while an intact admitted build serves.
func servingForkWithFailure(o *Options) {
	o.BuildForks = func(ForkBuildRequest) map[string]entrypoint.ForkDelivery {
		return map[string]entrypoint.ForkDelivery{"tool": {Key: "k-good", Runs: "v1.1.0 (0a1b2c3d) + 1 patch",
			PatchFailure: pfFailure()}}
	}
}

// THE REFUSAL, BEFORE THE IMAGE, WHATEVER THE COMMAND: an older build that serves does not turn a
// patch failure into a warning (PF-D81). Red with Run's refusePatchFailures call deleted, which boots
// the jail on the older build.
func TestAPatchFailureWithABuildToRunRefusesTheLaunch(t *testing.T) {
	for _, args := range [][]string{{"tool"}, {"bash"}, nil} {
		patchedLaunchHome(t)
		imaged := false
		argv, printed := fakePodmanLaunch(t, func(o *Options) {
			o.Args = args
			servingForkWithFailure(o)
			o.autoLoad = func(image.AutoLoadOptions) image.LoadResult {
				imaged = true
				return image.LoadResult{OK: true, Ref: goldenImageRef}
			}
		})
		if argv != nil || imaged {
			t.Fatalf("args %q: the launch went on to the image (%v) or the container (%v):\n%s", args, imaged, argv != nil, printed)
		}
		for _, w := range []string{
			"ERROR: fork forkpack/tool: patch series does not apply to upstream v1.2.0 (c5c0f6bd)\n",
			"  Patch: 0001-ten.patch\n  Conflict: f.txt\n\nRepair:\n",
			"    git rebase " + pfCommit + "\n",
			"\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES=forkpack/tool yolo    run v1.1.0 (0a1b2c3d) + 1 patch\n",
		} {
			if !strings.Contains(printed, w) {
				t.Errorf("args %q: the launch lacks %q:\n%s", args, w, printed)
			}
		}
		if strings.Contains(printed, "Continuing with") {
			t.Errorf("args %q: the refused launch says it continues:\n%s", args, printed)
		}
	}
}

// THE BYPASS runs the intact admitted build for this one launch and says so in one line under the
// block, which then lists no hatch; it takes `1`, the build's key, its pack or its bin. Red with the
// bypass's read deleted, or its Continuing line.
func TestThePatchFailureBypassRunsTheAdmittedBuild(t *testing.T) {
	for _, value := range []string{"1", "forkpack/tool", "forkpack", "tool", "other, tool"} {
		patchedLaunchHome(t)
		argv, printed := fakePodmanLaunch(t, func(o *Options) {
			servingForkWithFailure(o)
			withEnv(o, paths.AllowPatchFailuresEnv, value)
		})
		if argv == nil {
			t.Fatalf("%s=%s did not start the jail:\n%s", paths.AllowPatchFailuresEnv, value, printed)
		}
		if !strings.Contains(printed, "ERROR: fork forkpack/tool: patch series does not apply") ||
			!strings.Contains(printed, "\nContinuing with v1.1.0 (0a1b2c3d) + 1 patch (YOLO_ALLOW_PATCH_FAILURES)\n") ||
			strings.Contains(printed, "Start anyway:") {
			t.Errorf("%s: the bypass does not say the failure and what it runs:\n%s", value, printed)
		}
		if strings.Index(printed, "ERROR: fork forkpack/tool") > strings.Index(printed, "Continuing with") {
			t.Errorf("%s: the Continuing line precedes the error it continues past:\n%s", value, printed)
		}
	}
	// A name that covers another build does not continue past this one.
	patchedLaunchHome(t)
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		servingForkWithFailure(o)
		withEnv(o, paths.AllowPatchFailuresEnv, "otherpack")
	})
	if argv != nil || !strings.Contains(printed, "Start anyway:") {
		t.Errorf("a name covering no failed build continued the launch:\n%s", printed)
	}
}

// THE MISSING-PROGRAM HATCH DOES NOT WAIVE IT (PF-D83): a build of it is here, so leaving it out is not
// what the block offers; its hatch list names the patch failure's own hatch alone. Red with the
// refusal reading that hatch.
func TestTheMissingProgramHatchDoesNotWaiveAPatchFailureWithABuild(t *testing.T) {
	patchedLaunchHome(t)
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		servingForkWithFailure(o)
		allowMissingPrograms(o)
	})
	if argv != nil {
		t.Fatalf("the missing-program hatch started a launch whose patch series does not apply:\n%s", printed)
	}
	if !strings.Contains(printed, "\nStart anyway:\n  "+paths.AllowPatchFailuresEnv+"=forkpack/tool yolo") ||
		strings.Contains(printed, paths.AllowMissingProgramsEnv+"=1 yolo") {
		t.Errorf("the refusal does not offer the patch failure's own hatch alone:\n%s", printed)
	}
}

// AN OLDER RECORDED GOOD BUILD, of a recipe the series no longer is, is offered through cached-good
// recovery (PF-D82), not the patch failure's bypass, which has no build of the series to run. Red
// with forkPatchBypass ignoring CachedGood.
func TestAPatchFailureWithOnlyAnOlderGoodBuildOffersCachedGood(t *testing.T) {
	patchedLaunchHome(t)
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		o.BuildForks = func(ForkBuildRequest) map[string]entrypoint.ForkDelivery {
			return map[string]entrypoint.ForkDelivery{"tool": {Reason: pfFailure().Error(), PatchFailure: pfFailure(),
				CachedGood: "v1.0.0 (99aa88bb) + 1 patch"}}
		}
	})
	if argv != nil {
		t.Fatalf("a launch with no build of its series started its jail:\n%s", printed)
	}
	if !strings.Contains(printed, "\nStart anyway:\n  YOLO_USE_CACHED_GOOD=forkpack/tool yolo    run the older build v1.0.0 (99aa88bb) + 1 patch\n") ||
		strings.Contains(printed, "YOLO_ALLOW_PATCH_FAILURES=") {
		t.Errorf("the block does not offer cached-good recovery alone:\n%s", printed)
	}
}

// A PATCHED EXTENSION a selected agent pack loads is refused the same way; one no agent pack loads in
// a jail stops nothing, its block saying so. Red with launchPatchFailures' tree half deleted.
func TestAPatchedExtensionsPatchFailureRefusesOnlyWhereItIsLoaded(t *testing.T) {
	for _, listed := range []bool{true, false} {
		treeLaunchHome(t, listed)
		argv, printed := fakePodmanLaunch(t, func(o *Options) {
			o.BuildTrees = func(r TreeBuildRequest) map[string]TreeDelivery {
				dir := PatchedTreeCopyDir(r.CopyRoot, treeKey)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				pf := pfFailure()
				pf.Owner = treeKey
				return map[string]TreeDelivery{treeKey: {Dir: dir, Entry: "e1", Commit: strings.Repeat("a", 40), Patches: 1,
					PatchFailure: pf}}
			}
		})
		if refused := argv == nil; refused != listed {
			t.Errorf("listed %v: refused %v:\n%s", listed, refused, printed)
		}
		if !strings.Contains(printed, "ERROR: extension "+treeKey+": patch series does not apply to upstream v1.2.0") {
			t.Errorf("listed %v: no error block:\n%s", listed, printed)
		}
		if !listed && !strings.Contains(printed, "\nNo selected agent pack loads it here; this launch goes on.\n") {
			t.Errorf("an extension nothing loads does not say the launch goes on:\n%s", printed)
		}
		if listed && !strings.Contains(printed, "\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES="+treeKey+" yolo ") {
			t.Errorf("a loaded extension's block does not offer its bypass:\n%s", printed)
		}
	}
}

// BELOW APPLE CONTAINER'S READ-ONLY FLOOR the slot builds nothing new but still copies in a patched
// extension's good build, and its advance hands the patch failure beside it and prints no block, as
// in every slot: so the launch says the block and refuses there too. Red with launchPatchFailures
// gated on whether this runtime builds, which mounted the older build in silence.
func TestAPatchFailureBelowAppleContainersReadOnlyFloorStillRefuses(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	o := goldenOptions("/ws", t.TempDir())
	var stderr bytes.Buffer
	o.Stderr = &stderr
	o.Getenv = func(string) string { return "" }
	o.acVersion = &acVersionProbe{v: "0.1.0", ok: true}
	if o.roBindsUnsupported("container") == "" {
		t.Fatal("the fixture's Apple Container is not below the read-only floor")
	}
	pf := pfFailure()
	pf.Owner = treeKey
	o.patchedTrees = []packload.Fork{{Pack: "treepack", Bin: "tree-ext", Into: treeInto, Owner: "agentpack", ListedInJail: true}}
	o.treeDelivered = map[string]TreeDelivery{treeKey: {Dir: "/copies/tree-ext", Entry: "e1", Commit: strings.Repeat("a", 40),
		Patches: 1, PatchFailure: pf}}
	if !o.refusePatchFailures("container") {
		t.Fatalf("below the floor the launch mounted the older build of a series that does not apply:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "ERROR: extension "+treeKey+": patch series does not apply to upstream v1.2.0") ||
		!strings.Contains(stderr.String(), "\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES="+treeKey+" yolo ") {
		t.Errorf("below the floor the block is missing or offers no bypass:\n%s", stderr.String())
	}
}

// A PATCH BRANCH THAT DOES NOT MERGE refuses in its own words (docs/reference/patched-forks.md §3.5):
// the ERROR names the branch and its Repair the merge that repairs it, never a rebuilt series.
func TestAPatchBranchThatDoesNotMergeRefusesInItsOwnWords(t *testing.T) {
	fork := patchedLaunchHome(t)
	if err := os.WriteFile(fork+"/pack.json", []byte(`{"contributes":[{"kind":"program","bin":"tool",`+
		`"via":"source","fork_of":"basepack","source":"`+patchedSource+`",`+
		`"patch_branch":"git+https://github.com/you/tool?ref=patches","build":"make install",`+
		`"produces":[".local/bin/tool"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, printed := fakePodmanLaunch(t, func(o *Options) { servingForkWithFailure(o) })
	for _, w := range []string{
		"ERROR: fork forkpack/tool: patch branch does not merge into upstream v1.2.0 (c5c0f6bd)\n",
		"  Patch branch: 0001-ten.patch\n",
		"\nRepair:\n", "git merge " + pfCommit, "git push origin patches\n",
		"\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES=forkpack/tool yolo ",
	} {
		if !strings.Contains(printed, w) {
			t.Errorf("the refused launch lacks %q:\n%s", w, printed)
		}
	}
	if strings.Contains(printed, "repaired series") {
		t.Errorf("the refusal tells a patch branch's user a launch builds a repaired series:\n%s", printed)
	}
}
