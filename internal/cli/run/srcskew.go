package run

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/version"
)

// AllowSourceSkewEnv opts back into launching with a host binary older than the
// tree the image is about to be built from. Named on the refusal, in the house
// style of YOLO_ALLOW_STALE_IMAGE and YOLO_ALLOW_UNREACHABLE_SERVICES: a fatal
// witness always says how to overrule it.
const AllowSourceSkewEnv = "YOLO_ALLOW_SOURCE_SKEW"

// refuseOnSourceSkew reports whether this launch must stop because the source tree
// at repoRoot has moved past the binary running this function, through paths the
// jail image is built from (version.SourceSkew).
//
// # Why it refuses instead of warning
//
// The launch is already doomed at this point — it just fails later, more expensively,
// and somewhere that names neither half of the cause. AutoLoadImage will nix-build and
// stream a multi-gigabyte image from the NEW source, hand it a launcher argv from the
// OLD source, and the mismatch surfaces (if it surfaces at all) as an entrypoint error
// about a path nobody asked about. Refusing costs the user one `just install`; not
// refusing costs a build, a stream, and a debugging session that starts in the wrong
// package. Cheap, early and named beats late, expensive and anonymous.
//
// It is deliberately CONTAINER-ONLY, called under the same `rt != "macos-user"` guard
// as the repo-root gate. macos-user has no image and no second binary: the host yolo
// provisions the sandbox in-process, so there are not two halves to skew.
func (o *Options) refuseOnSourceSkew(repoRoot string) bool {
	if o.Getenv(AllowSourceSkewEnv) != "" {
		return false
	}
	skew := version.SourceSkew(repoRoot)
	if skew == nil {
		return false
	}
	changes, fix, ifRan := sourceSkewNextStep(repoRoot, o.Getenv("YOLO_VERSION") != "")
	o.pr(o.Stderr).print(
		"[bold red]Refusing to launch: this yolo is older than the source tree it would " +
			"build the jail from.[/bold red]\n\n" +
			"  this yolo   " + runningBinary() + "\n" +
			"              was built from  " + short(skew.BinaryCommit) + "\n" +
			"  " + repoRoot + "\n" +
			"              is at  " + short(skew.TreeCommit) + "\n" +
			"  they differ in  " + strings.Join(skew.Changed, ", ") + "\n\n" +
			"The jail's yolo-entrypoint is BUILT AND MOUNTED from the tree on every launch\n" +
			"(and the image is rebuilt from it too). The yolo you just ran\n" +
			changes + " Launching now would pair a launcher\n" +
			"and a yolo-entrypoint built from different source — which fails deep inside the\n" +
			"boot, naming neither half (the last one refused with\n" +
			"`mkdir /home/agent/.yolo: read-only file system`).\n\n" +
			"[bold]Fix:[/bold]  " + fix + "\n\n" +
			ifRan +
			"To launch anyway and own the mismatch: [bold]" + AllowSourceSkewEnv + "=1[/bold]")
	return true
}

// sourceSkewNextStep returns the refusal's sentence about when the running yolo
// changes, its fix, and its paragraph for a reader who already applied that fix.
//
// IN A JAIL THE HOST'S FIX IS WRONG: `just install` refuses there, and the jail's own
// yolo is frozen for its session (AGENTS.md, "Build & deploy"). A flake build stamps
// the commit its jail was built from (flake.nix: buildStamp), so once the tree moves
// through an image path this refusal fires in a jail too, and the next step is the
// by-path binary `just build-go` writes, which carries the current HEAD.
func sourceSkewNextStep(repoRoot string, inJail bool) (changes, fix, ifRan string) {
	if inJail {
		bin := filepath.Join(repoRoot, "dist-go", "linux-"+runtime.GOARCH, "yolo")
		return "is this jail's own, frozen for its session.",
			"(cd " + shquote.Quote(repoRoot) + " && just build-go), then launch with\n" +
				"      " + shquote.Quote(bin) + " instead of `yolo`,\n" +
				"      from a directory outside /workspace (AGENTS.md, nested-jail verification).",
			""
	}
	return "changes only when you run `just install`.",
		"(cd " + shquote.Quote(repoRoot) + " && just install)",
		"[bold]If you just ran that[/bold], the path above is not where it installed — compare it\n" +
			"with `go env GOBIN` (or `$(go env GOPATH)/bin`), check `command -v -a yolo` for an\n" +
			"older copy earlier on PATH, and `hash -r` if your shell cached the old location.\n\n"
}

// runningBinary is the path of the executable printing this refusal.
//
// IT IS THE LINE THAT DISTINGUISHES THE TWO CAUSES, and they need different fixes:
// a yolo nobody reinstalled, versus a `just install` that landed somewhere PATH does
// not reach first — a second copy in ~/.local/bin, a package-manager one, or a shell
// that cached the old location. Both print the same commit mismatch and only the path
// tells them apart, so the refusal that omitted it sent the reader to `just install`
// for a problem `just install` had already failed to fix once.
func runningBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "(path unknown: " + err.Error() + ")"
	}
	// Resolve symlinks: a GOBIN copy reached through one is the same binary, and
	// printing the link would hide that.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// short abbreviates a full sha for the report. Not git's `--short` (which is
// length-adaptive): this is display only, and the refusal prints the repo root
// beside it, so a fixed 8 is unambiguous enough and stable to assert on.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
