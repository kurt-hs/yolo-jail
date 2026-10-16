package packsrc

import (
	"fmt"
	"os"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// patchfailureblock.go is PF-D81's ERROR BLOCK (docs/reference/patched-forks.md §8): the one text every
// actor prints for a patch series that does not apply — a jail launch, `yolo host`, the host floor,
// `yolo pack update`, `yolo capture`, cached-good recovery — so the copyable shape is written once.
// Its shape, ruled 2026-10-16: one ERROR line naming the subject and the upstream target, the facts
// under it (the patch, the conflict, the log), a blank line and the repair (the rebase recipe,
// rebaserecipe.go), then a blank line and the hatches that would get THIS operation running, each a
// copyable command (patchrecovery.Section). No prose justifying any of it.

// PatchBypass is what the block's hatch list may offer, decided by the caller from what is on this
// machine (PF-D83): a hatch is listed only when it would get that operation running.
type PatchBypass struct {
	// Command is the operation every hatch is put in front of: "yolo", "yolo host -- pi",
	// "yolo pack update".
	Command string
	// Key is the subject's owner key, the value each scoped hatch names; "" derives it from the
	// block's label.
	Key string
	// Hatches is the list, in order. When it is empty the legacy fields below derive it (List).
	Hatches []patchrecovery.Hatch
	// Runs is the label of the intact admitted build paths.AllowPatchFailuresEnv runs in the failed
	// series' place; "" when there is none, and then that variable is not offered.
	Runs string
	// CachedGood is the owner key whose older recorded good build YOLO_USE_CACHED_GOOD runs for one
	// fresh jail launch (PF-D82), with CachedGoodLabel naming it; "" when there is none or Runs is set.
	CachedGood, CachedGoodLabel string
	// Missing says YOLO_ALLOW_MISSING_PROGRAMS starts the operation without the program (a jail
	// launch with no build of it to run, PF-D83).
	Missing bool
	// Unneeded says the operation goes on without stopping for it: a patched extension no selected
	// agent pack loads where this launch runs.
	Unneeded bool
}

// CachedGoodEnv is the jail launch's cached-good recovery selector (PF-D82), spelled here so the
// block can offer it.
const CachedGoodEnv = patchrecovery.UseCachedGoodEnv

// List is the hatches the block lists for key: Hatches, or what the legacy fields name.
func (b PatchBypass) List(key string) []patchrecovery.Hatch {
	if len(b.Hatches) > 0 || b.Unneeded {
		return b.Hatches
	}
	if b.Key != "" {
		key = b.Key
	}
	cmd := b.Command
	if cmd == "" {
		cmd = "yolo"
	}
	var out []patchrecovery.Hatch
	switch {
	case b.Runs != "":
		out = append(out, patchrecovery.Hatch{Env: patchrecovery.AllowPatchFailuresEnv, Value: key, Command: cmd,
			Label: "run " + b.Runs})
	case b.CachedGood != "":
		out = append(out, patchrecovery.Hatch{Env: patchrecovery.UseCachedGoodEnv, Value: b.CachedGood, Command: cmd,
			Label: "run the older build " + b.CachedGoodLabel})
	}
	if b.Missing {
		out = append(out, patchrecovery.Hatch{Env: patchrecovery.AllowMissingProgramsEnv, Value: "1", Command: cmd,
			Label: "start without it"})
	}
	return out
}

// Block is the error block for f without color (Render).
func (f *PatchFailure) Block(label string, recipe RebaseRecipe, bypass PatchBypass) string {
	return f.Render(label, recipe, bypass, false)
}

// Render is the error block for f: label names the subject ("fork pi-mine/pi", "extension <key>"),
// recipe is the subject's rebase recipe (RebaseRecipe, PF-D85), and bypass what the hatch list
// offers; color styles the ERROR line, the headings and the hatch commands.
func (f *PatchFailure) Render(label string, recipe RebaseRecipe, bypass PatchBypass, color bool) string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	target := f.Target.Tag
	if target == "" {
		target = f.Target.Commit
	}
	what, member := "patch series does not apply to", "Patch"
	if recipe.PatchBranchRepo != "" {
		what, member = "patch branch does not merge into", "Patch branch"
	}
	if f.Kind == PatchFailureBuild {
		what = "the build failed at"
	}
	head := fmt.Sprintf("ERROR: %s: %s upstream %s (%s)", label, what, target, shortCommit(f.Target.Commit))
	b.WriteString(richtext.Render("[bold red]"+richtext.Escape(head)+"[/bold red]", color) + "\n")
	if f.Member != "" {
		fmt.Fprintf(&b, "  %s: %s\n", member, PrintableFailureText(f.Member))
	}
	switch f.Kind {
	case PatchFailureBuild:
		fmt.Fprintf(&b, "  Build: %s\n", PrintableFailureText(f.Detail))
	case "conflict":
		if len(f.Paths) > 0 {
			fmt.Fprintf(&b, "  Conflict: %s\n", PrintableFailureText(strings.Join(f.Paths, ", ")))
		} else {
			b.WriteString("  Conflict: the patch could not be merged\n")
		}
	case "base":
		fmt.Fprintf(&b, "  Base rejection: %s\n", PrintableFailureText(f.Detail))
	default:
		fmt.Fprintf(&b, "  Application command: %s\n", PrintableFailureText(f.Detail))
	}
	if f.Log != "" {
		if st, err := os.Stat(f.Log); err == nil && !st.IsDir() {
			fmt.Fprintf(&b, "  Log: %s\n", PrintableFailureText(f.Log))
		}
	}
	key := bypass.Key
	if key == "" {
		key = strings.TrimPrefix(strings.TrimPrefix(label, "fork "), "extension ")
	}
	b.WriteString("\n" + richtext.Render("[bold]Repair:[/bold]", color) + "\n")
	switch f.Kind {
	case PatchFailureBuild:
		retry := f.Retry
		if retry == "" {
			retry = "yolo capture " + key
		}
		b.WriteString("  read the build's output, and once upstream or the build recipe is fixed, retry it:\n")
		b.WriteString("    " + retry + "\n")
	case "base":
		fmt.Fprintf(&b, "  re-export the series: git format-patch --base=%s\n", f.Target.Commit)
	default:
		for _, l := range recipe.Lines(f.Target) {
			b.WriteString("  " + l + "\n")
		}
	}
	switch hatches := bypass.List(key); {
	case bypass.Unneeded:
		b.WriteString("\nNo selected agent pack loads it here; this launch goes on.\n")
	case len(hatches) > 0:
		b.WriteString(patchrecovery.Section(patchrecovery.StartAnyway, hatches, color))
	}
	return b.String()
}

// PrintableFailureText escapes control characters a patch name, path or git detail could carry, so
// the block stays one copyable text on any terminal.
func PrintableFailureText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "\\x%02x", r)
		}
	}
	return b.String()
}
