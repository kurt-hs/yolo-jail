package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// packStatusKey is `yolo pack status <pack>/<name>`: the full build line of one selected fork or
// built tree, and its recipe digest. A launch's disclosure block names a build line by that digest
// and this command (packload.Claim.LaunchDisclosureSentence, OQ-RO9 in
// docs/reference/report-tiers.md), so this is where the line is read whole on demand. It reads the
// same selection a host verb does (selectConfiguredHostPacks), and runs nothing.
func packStatusKey(args []string, out, errw io.Writer, color bool) int {
	if len(args) != 1 {
		fmt.Fprintf(errw, "yolo pack status: unexpected argument %q — it takes at most one <pack>/<name> key "+
			"(see `yolo pack --help`)\n", args[1])
		return 2
	}
	key := args[0]
	if pack, name, ok := strings.Cut(key, "/"); !ok || pack == "" || name == "" {
		fmt.Fprintf(errw, "yolo pack status: %q is not a <pack>/<name> key — name a fork or built tree as a "+
			"launch's disclosure does (for example `yolo pack status pi-fork/pi`), or run `yolo pack status` "+
			"with no argument for every pack\n", key)
		return 2
	}
	sel := selectConfiguredHostPacks()
	if sel.loadErr != nil {
		fmt.Fprintf(errw, "yolo pack status: %v\n  fix what it names in %s, then run `yolo pack status %s` again\n",
			sel.loadErr, paths.UserConfigPath(), key)
		return 1
	}
	forks := append(packload.Forks(sel.packs), packload.PatchedTrees(sel.packs)...)
	for _, f := range forks {
		if f.Key() == key {
			rc := printBuildLine(richtext.Printer{W: out, Color: color}, errw, forks, key)
			if rc == 0 {
				printHostPreparationSnapshot(out, errw, key)
			}
			return rc
		}
	}
	// Not in what resolved: before saying nothing selected builds it, say so when its pack is
	// configured but did not resolve HERE, which is the ordinary case in a jail (a host path the
	// jail does not have) and a store miss or manifest problem on the host.
	// Retained inactive/failure evidence stays inspectable after deselection too.
	// It does not make an unselected build line or serving entry admissible.
	printHostPreparationSnapshot(out, errw, key)
	if rc, said := packStatusKeyUnresolved(errw, sel, key); said {
		return rc
	}
	return printBuildLine(richtext.Printer{W: out, Color: color}, errw, forks, key)
}

// packStatusKeyUnresolved reports a key whose pack the user configured but this process could not
// resolve, or a selection whose closure or entries failed, with the reason and the next command.
// said is false when neither applies, and the caller's own refusal stands.
func packStatusKeyUnresolved(errw io.Writer, sel hostPackSet, key string) (rc int, said bool) {
	pack, _, _ := strings.Cut(key, "/")
	where := "fix what it names, then run `yolo pack status " + key + "` again"
	if config.InJail() {
		where = "this jail cannot read it; run `yolo pack status " + key + "` in a terminal on the host, " +
			"where the launch disclosed it"
	}
	for _, u := range sel.unresolved {
		if u.Name == pack {
			fmt.Fprintf(errw, "yolo pack status: the pack %s is configured but did not resolve here: %s — %s\n",
				pack, u.Reason, where)
			return 1, true
		}
	}
	if sel.closureErr != nil {
		fmt.Fprintf(errw, "yolo pack status: the selection is incomplete: %v — %s\n", sel.closureErr, where)
		return 1, true
	}
	if len(sel.entryProblems) > 0 {
		fmt.Fprintf(errw, "yolo pack status: a `packs` entry did not load (%s), so %s may be missing — %s\n",
			strings.Join(sel.entryProblems, "; "), key, where)
		return 1, true
	}
	return 0, false
}

// printBuildLine prints the build line of the fork or built tree forks names key, whole, with its
// recipe digest: the digest a launch shows (packload.BuildLineDigest), over the same line the
// claim quotes (Fork.Build: a fork's `build`, a built tree's TreeBuild). A key nothing selected
// names is refused with the keys that are, so the next command is one of them.
func printBuildLine(pr richtext.Printer, errw io.Writer, forks []packload.Fork, key string) int {
	var keys []string
	for _, f := range forks {
		if f.Key() != key {
			keys = append(keys, f.Key())
			continue
		}
		pr.Printf("[bold]%s[/bold]", richtext.Escape(f.Label()))
		if strings.TrimSpace(f.Build) == "" {
			pr.Printf("  [dim]no build line: its build copies the checkout as it is[/dim]")
			return 0
		}
		pr.Printf("  build line, recipe %s:", packload.BuildLineDigest(f.Build))
		pr.Printf("    %s", richtext.Escape(f.Build))
		return 0
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		fmt.Fprintf(errw, "yolo pack status: no selected pack builds %s — the selection has no fork or built tree; "+
			"`yolo pack ls` lists the selected packs\n", key)
		return 1
	}
	fmt.Fprintf(errw, "yolo pack status: no selected fork or built tree is %s — the selected ones are %s; run "+
		"`yolo pack status <one of them>`\n", key, strings.Join(keys, ", "))
	return 1
}

// Scheduling evidence is diagnostic only: serving readers resolve CheckRecord.Good
// against the current recipe and platform instead of trusting this snapshot.
func printHostPreparationSnapshot(out, errw io.Writer, key string) {
	selected := preparationSubscription{Key: key, Runtime: captureRuntime(), Platform: capture.Platform()}
	snapshot, err := readPreparationSnapshotFor(filepath.Join(paths.BackgroundAdvanceDir(), "scheduler"), selected)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		fmt.Fprintf(errw, "  preparation status unavailable: %v — run `yolo pack prepare status` to inspect and repair scheduler state.\n", err)
		return
	}
	fmt.Fprintf(out, "  preparation %s (%s, %s): %s, generation %d (diagnostic only)\n", snapshot.Key, snapshot.Runtime, snapshot.Platform, snapshot.Phase, snapshot.Generation)
	fmt.Fprintf(out, "    input: %s\n", snapshot.InputIdentity)
	if snapshot.WorkerIdentity != "" {
		fmt.Fprintf(out, "    worker: %s\n", snapshot.WorkerIdentity)
	}
	if !snapshot.At.IsZero() {
		fmt.Fprintf(out, "    at: %s\n", snapshot.At.Format("2006-01-02T15:04:05Z07:00"))
	}
	if snapshot.ServingEntry != "" {
		fmt.Fprintf(out, "    serving entry: %s (diagnostic only; not admission)\n", snapshot.ServingEntry)
	}
	if snapshot.Reason != "" {
		fmt.Fprintf(out, "    %s\n", snapshot.Reason)
	}
	if !snapshot.RetryAt.IsZero() {
		fmt.Fprintf(out, "    retry: %s\n", snapshot.RetryAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	if snapshot.Log != "" {
		fmt.Fprintf(out, "    log: %s\n", snapshot.Log)
	}
}
