package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	runpkg "github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

const allowPatchFailuresEnv = paths.AllowPatchFailuresEnv

// allowPatchFailures reports whether YOLO_ALLOW_PATCH_FAILURES covers f: `1`, or a name equal to
// its key, its pack or its bin or extension name (patchrecovery's grammar).
var allowPatchFailures = func(f packload.Fork) bool {
	return patchrecovery.Read(os.Getenv, allowPatchFailuresEnv).Covers(recoverySubject(f))
}

// recoverySubject is f as a hatch's names match it.
func recoverySubject(f packload.Fork) patchrecovery.Subject {
	return patchrecovery.Subject{Key: f.Key(), Pack: f.Pack, Name: f.Bin}
}

// currentPatchFailure is what stops f's act, read from its check record with no git: the recorded
// patch failure of its series as it stands, else its failed newer build (buildFailureOf, OQ-PPX3).
func currentPatchFailure(f packload.Fork) *packsrc.PatchFailure {
	series, err := f.ReadSeries()
	if err != nil {
		return nil
	}
	record, err := runpkg.LoadPatchedRecord(patchedForkStore(), f, series)
	if err != nil {
		return nil
	}
	inputs, _, _, _ := f.CheckWant(series).Inputs()
	if pf := record.CurrentPatchFailure(inputs, series.Digest); pf != nil {
		return pf
	}
	return buildFailureOf(f, record, inputs, series, forkBuild{Fork: f, Series: series}.recipe(), "")
}

// buildFailureOf is f's FAILED NEWER BUILD (the maintainer's ruling OQ-PPX3, 2026-10-16:
// docs/reference/pi-extensions.md): the build of the newest candidate failed while a good build of
// the series as it stands is on this machine (packsrc.CheckRecord.CurrentBuildFailure), as a
// PatchFailure of Kind packsrc.PatchFailureBuild, which every act refuses as it does a patch failure,
// so none falls back to the older build on its own. nil when nothing stops f:
//
//   - while the failure's back-off has run out, since the next advance builds that candidate again
//     (and refuses if it fails again), rather than stopping on an old verdict;
//   - under an `agent_updates` hold (OQ-PBR5), which runs the good build past a newer version;
//   - for an upstream build (YOLO_SKIP_PATCHES), itself a recovery hatch the user set.
//
// workspace names the build's log (buildLogName), "" for none.
func buildFailureOf(f packload.Fork, rec *packsrc.CheckRecord, in packsrc.CheckInputs, series *packsrc.Series,
	recipe, workspace string) *packsrc.PatchFailure {
	if f.Upstream || rec == nil || series == nil || runpkg.PatchedForkHold(f) != "" {
		return nil
	}
	pf, o := rec.CurrentBuildFailure(in, series.Digest, recipe, patchedYoloVersion())
	if pf == nil || !patchedNow().Before(autoCaptureRetryAt(capture.AutoFailure{Failures: o.Count,
		Last: time.Unix(o.At, 0)})) {
		return nil
	}
	pf.Retry = "yolo capture " + shquote.Quote(f.CaptureArg())
	if workspace != "" {
		pf.Log = filepath.Join(paths.WorkspaceStateDir(workspace), buildLogName(f.Key()))
	}
	return pf
}

// preparationPatchFailure inspects scheduler authority without migrating a legacy good build or
// waiting for its check-record lock. ErrLockHeld, read failures and cancellation are unknown, not
// evidence that the subscription was revoked or that the record is clear.
func preparationPatchFailure(ctx context.Context, f packload.Fork) (*packsrc.PatchFailure, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	series, err := f.ReadSeries()
	if err != nil {
		return nil, err
	}
	inputs, _, _, err := f.CheckWant(series).Inputs()
	if err != nil {
		return nil, err
	}
	store := *patchedForkStore()
	store.NoWait = true
	var record *packsrc.CheckRecord
	err = store.WithCheckRecord(f.Key(), nil, func(r *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
		if readErr != nil {
			return false, readErr
		}
		copy := *r
		record = &copy
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record = runpkg.NormalizeLegacyPatchedRecord(f, series, record)
	return record.CurrentPatchFailure(inputs, series.Digest), nil
}

// patchFailureCommand is the operation a patch failure's bypass is put in front of: `yolo host --
// <bin>` at the host, else a jail launch.
func patchFailureCommand(bin string, host bool) string {
	if host {
		return "yolo host -- " + shquote.Quote(bin)
	}
	return "yolo"
}

// writePatchFailure prints PF-D81's error block (packsrc.PatchFailure.Block) for fork, its Bypass
// line offering only what works on this machine (PF-D83), and — when the bypass is set and
// admitted names the intact build it runs — no hatch list but the one Continuing line.
func writePatchFailure(w io.Writer, f *packsrc.PatchFailure, fork packload.Fork, bypass packsrc.PatchBypass, admitted string) {
	if f == nil {
		return
	}
	continuing := allowPatchFailures(fork) && admitted != ""
	if continuing {
		bypass = packsrc.PatchBypass{Key: fork.Key()}
	}
	_, _ = io.WriteString(w, f.Block(fork.Key(), fork.RebaseRecipe(), bypass))
	if continuing {
		sayContinuing(w, admitted)
	}
}

// sayContinuing is the one line a run says when the patch-failure bypass runs build in the failed
// series' place: the form the jail launch says too.
func sayContinuing(w io.Writer, build string) {
	fmt.Fprintf(w, "Continuing with %s (%s)\n", build, allowPatchFailuresEnv)
}

func printableFailureText(s string) string { return packsrc.PrintableFailureText(s) }
