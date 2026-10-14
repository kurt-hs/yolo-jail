package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
)

// wireFloorOrdinaryCapture is compatibility-gated reuse, not a native producer.
// The host must independently observe EVERY producer runtime fact. A host without
// the runtime identity marker (Darwin included) retains its bounded floor installer.
func wireFloorOrdinaryCapture(f *hostfloor.Floor, store *capture.Store, out io.Writer) {
	f.PrepareNPM = func(ctx context.Context, p hostfloor.Program, nodeBin string, fresh bool) (*hostfloor.PreparedNPM, error) {
		in, ok := npmCaptureInputFor(p.Pack, p.Install)
		if !ok {
			return nil, nil
		}
		if f.GOOS != "linux" {
			return nil, nil
		}
		cfg, err := config.UserScopeConfig(true, nil)
		if err != nil {
			return nil, nil
		}
		reason := run.OrdinaryRuntimeUnsupported(cfg, os.Getenv("YOLO_STORE_PACKAGES") == "1")
		for _, key := range []string{"NODE_OPTIONS", "NODE_PATH", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH"} {
			if os.Getenv(key) != "" {
				reason = "ambient " + key + " changes the host runtime environment"
			}
		}
		if loader := os.Getenv("LD_LIBRARY_PATH"); loader != "" && loader != "/usr/local/lib/yolo-ld" {
			reason = "the host loader environment is not the observed baseline"
		}
		if reason != "" {
			fmt.Fprintf(out, "yolo host: %s sealed artifact reuse is unsupported (%s); retaining its authorized bounded host installer — use an independently matching baseline runtime for reuse\n", p.Bin(), reason)
			return nil, nil
		}
		record, err := readNPMCaptureRecord(store, in)
		if err != nil {
			fmt.Fprintf(out, "yolo host: %s capture compatibility could not be proven (%v); retaining the bounded host installer — run a fresh jail launch to repair the capture record\n", p.Bin(), err)
			return nil, nil
		}
		if record.Resolution == nil {
			return nil, nil
		}
		actual, err := observeNPMCaptureRuntime(ctx, f.Home, filepath.Join(nodeBin, "node"))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil || actual != record.Resolution.Runtime || actual.Platform != f.GOOS+"/"+f.GOARCH {
			return nil, nil
		}
		inputs, err := record.Resolution.inputs()
		if err != nil {
			return nil, nil
		}
		identity, err := inputs.Identity()
		if err != nil || identity != record.Identity {
			return nil, nil
		}
		if fresh {
			d := preparationDemand{Kind: preparationOrdinaryProgramKind, Key: ordinaryProgramKey(in), Runtime: captureRuntime(), Platform: actual.Platform}
			current, fingerprint, release, err := resolveOrdinaryPreparation(ctx, d)
			release()
			if errors.Is(err, errSharedPreparationForegroundOnly) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if current == nil || *current != in {
				return nil, errPreparationAdmissionStale
			}
			d.Identity = ordinaryProgramIdentity(in, fingerprint, d.Runtime, d.Platform)
			guard := func(commit func() error) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				_, _, release, e := resolveOrdinaryPreparation(ctx, d)
				defer release()
				if e != nil {
					return e
				}
				return commit()
			}
			record, err = waitOrdinaryPreparation(ctx, in, fingerprint, d.Runtime, d.Platform, true, out, guard)
			if errors.Is(err, errNPMCaptureUnsupported) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if record.Resolution == nil || record.Resolution.Runtime != actual {
				return nil, nil
			}
			inputs, err = record.Resolution.inputs()
			if err != nil {
				return nil, err
			}
			identity, err = inputs.Identity()
			if err != nil {
				return nil, err
			}
		}
		entry, _, err := resolveProgramCaptureFor(store, in.Bin, actual.Platform, identity)
		if err != nil {
			return nil, err
		}
		// Selection closes over the exact identity for this floor operation. A pointer
		// admitted by a later launch cannot change its materialization under the lock.
		return &hostfloor.PreparedNPM{Compatibility: identity, EntryKey: entry.Key, Version: inputs.Release, Materialize: func(ctx context.Context, home string) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if rc := materializeCapture(materializeArgs{store: store.Dir, bin: in.Bin, home: home, compatibility: identity, declared: inputs.Declared}, out); rc != 0 {
				return fmt.Errorf("exact host npm materialization failed — run yolo host --update to retry")
			}
			return ctx.Err()
		}}, nil
	}
}
