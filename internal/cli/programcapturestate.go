package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// npmCaptureRecord retains a successful exact resolution separately from a later
// failed operation. A fresh waiter requires Completed > its registered boundary.
type npmCaptureRecord struct {
	Fingerprint        string
	Started, Completed uint64
	Resolution         *npmCaptureResolution
	Identity, Entry    string
	CheckedAt          time.Time
	Failure            string
}

func npmCaptureRecordPath(store *capture.Store, in npmCaptureInput) string {
	return filepath.Join(store.Dir, "program-resolutions", in.declarationIdentity()+".json")
}
func readNPMCaptureRecord(store *capture.Store, in npmCaptureInput) (npmCaptureRecord, error) {
	raw, err := os.ReadFile(npmCaptureRecordPath(store, in))
	if errors.Is(err, os.ErrNotExist) {
		return npmCaptureRecord{}, nil
	}
	if err != nil {
		return npmCaptureRecord{}, err
	}
	var r npmCaptureRecord
	err = json.Unmarshal(raw, &r)
	if err == nil && r.Resolution != nil && (r.Resolution.Input != in || r.Fingerprint != r.Resolution.Fingerprint) {
		return r, fmt.Errorf("ordinary resolution metadata does not match its selected declaration/recipe — remove the invalid resolution record and retry preparation")
	}
	return r, err
}
func withNPMCaptureRecord(ctx context.Context, store *capture.Store, in npmCaptureInput, fn func(*npmCaptureRecord) error) error {
	return withNPMCaptureRecordMode(ctx, store, in, true, fn)
}

func withNPMCaptureRecordMode(ctx context.Context, store *capture.Store, in npmCaptureInput, wait bool, fn func(*npmCaptureRecord) error) error {
	lock, err := pidlock.Acquire(filepath.Join(store.Dir, "locks", "npm-record-"+in.declarationIdentity()+".lock"), pidlock.Mode{Wait: wait, Bound: time.Minute, Cancel: ctx.Done()}, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	r, err := readNPMCaptureRecord(store, in)
	if err != nil {
		return err
	}
	if err = fn(&r); err != nil {
		return err
	}
	file := npmCaptureRecordPath(store, in)
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".npm-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}

func registerNPMCaptureRequest(ctx context.Context, store *capture.Store, in npmCaptureInput) (uint64, error) {
	var boundary uint64
	err := withNPMCaptureRecord(ctx, store, in, func(r *npmCaptureRecord) error { boundary = r.Started; return nil })
	return boundary, err
}

// advanceNPMCapture owns resolution/check exclusion. Its caller must retain authorized
// cross-process demand; the publication guard revalidates selected inputs outside transport.
func advanceNPMCapture(ctx context.Context, store *capture.Store, in npmCaptureInput, fingerprint string, fresh bool, boundary uint64, w io.Writer, guard func(func() error) error) (npmCaptureRecord, error) {
	guard = ordinaryMetadataGuard(ctx, guard)
	lock, err := pidlock.Acquire(filepath.Join(store.Dir, "locks", "npm-check-"+in.declarationIdentity()+".lock"), pidlock.Mode{Wait: true, Bound: forkBuildWaitBound, Cancel: ctx.Done()}, nil)
	if err != nil {
		return npmCaptureRecord{}, err
	}
	defer lock.Release()
	r, err := readNPMCaptureRecord(store, in)
	if err != nil {
		return r, err
	}
	if r.Fingerprint == fingerprint && r.Resolution != nil && (!fresh || r.Completed > boundary) && (!fresh || r.Failure == "") {
		inputs, inputErr := r.Resolution.inputs()
		if inputErr != nil {
			return r, inputErr
		}
		identity, _ := inputs.Identity()
		entry, _, hitErr := resolveProgramCaptureFor(store, in.Bin, inputs.Platform, identity)
		if hitErr == nil {
			r.Entry = entry.Key
			r.Identity = identity
			return r, guard(func() error { return ctx.Err() })
		}
	}
	var sequence uint64
	err = guard(func() error {
		return withNPMCaptureRecordMode(ctx, store, in, false, func(current *npmCaptureRecord) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			current.Started++
			sequence = current.Started
			return nil
		})
	})
	if err != nil {
		return r, err
	}
	resolution, err := resolveSelectedNPMCapture(ctx, store, in, w)
	if err == nil {
		resolution.Fingerprint = fingerprint
		var entry *capture.Entry
		entry, err = prepareSelectedNPMCapture(ctx, store, *resolution, w, guard)
		if err == nil {
			inputs, _ := resolution.inputs()
			identity, _ := inputs.Identity()
			err = guard(func() error {
				return withNPMCaptureRecordMode(ctx, store, in, false, func(current *npmCaptureRecord) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if current.Started != sequence {
						return fmt.Errorf("selected npm check sequence changed — retry preparation")
					}
					*current = npmCaptureRecord{Fingerprint: fingerprint, Started: sequence, Completed: sequence, Resolution: resolution, Identity: identity, Entry: entry.Key, CheckedAt: time.Now()}
					r = *current
					return nil
				})
			})
		}
	}
	if err != nil {
		_ = guard(func() error {
			return withNPMCaptureRecordMode(ctx, store, in, false, func(current *npmCaptureRecord) error {
				if current.Started == sequence {
					current.Failure = err.Error()
				}
				return nil
			})
		})
		return r, err
	}
	return r, nil
}

// ordinaryMetadataGuard retries ONLY no-wait short-lock contention, outside all
// metadata locks and without repeating resolution/installation. Every attempt
// rechecks the existing demand/selection guard before executing the commit once.
func ordinaryMetadataGuard(ctx context.Context, guard func(func() error) error) func(func() error) error {
	return func(commit func() error) error {
		waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		for {
			if waitCtx.Err() != nil {
				return waitCtx.Err()
			}
			err := guard(commit)
			if !errors.Is(err, pidlock.ErrHeld) && !errors.Is(err, packsrc.ErrLockHeld) {
				return err
			}
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-waitCtx.Done():
				timer.Stop()
				return waitCtx.Err()
			case <-timer.C:
			}
		}
	}
}
