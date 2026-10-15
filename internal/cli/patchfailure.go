package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	runpkg "github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

const allowPatchFailuresEnv = paths.AllowPatchFailuresEnv

var allowPatchFailures = func() bool { return os.Getenv(allowPatchFailuresEnv) == "1" }

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
	return record.CurrentPatchFailure(inputs, series.Digest)
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
// admitted names the intact build it runs — the CONTINUING line.
func writePatchFailure(w io.Writer, f *packsrc.PatchFailure, fork packload.Fork, bypass packsrc.PatchBypass, admitted string) {
	if f == nil {
		return
	}
	_, _ = io.WriteString(w, f.Block(fork.Key(), fork.RebaseRecipe(), bypass))
	if allowPatchFailures() && admitted != "" {
		fmt.Fprintf(w, "CONTINUING: using intact admitted build %s; skips this subject's advance.\n", admitted)
	}
}

func printableFailureText(s string) string { return packsrc.PrintableFailureText(s) }
