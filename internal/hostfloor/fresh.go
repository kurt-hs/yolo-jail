package hostfloor

import (
	"context"
	"fmt"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// EnsureFresh is human authorization for an ordinary installer check, not
// capture replacement policy or source-fork pin movement. Failures remain errors
// even when the previous installation remains intact.
func (f *Floor) EnsureFresh(ctx context.Context, p Program) (Status, Outcome, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, "", err
	}
	if p.Install.Kind == packdecl.InstallKindSource {
		return Status{}, "", fmt.Errorf("%s requires source preparation before floor consumption; request its authorized pin/build and retry", p.Bin())
	}
	st := f.Status(p)
	if st.Disposition != Provisioned || st.Pending != "" {
		st, outcome, err := f.Ensure(ctx, p)
		if err == nil && outcome == Kept {
			err = fmt.Errorf("%s fresh install failed; repair and retry --update or launch without --update", p.Bin())
		}
		return st, outcome, err
	}
	if st.Newer {
		return st, "", newerRecordError{st.Reason}
	}
	if p.Install.Kind == "npm" {
		_, version := packdecl.SplitNpmSpec(p.Install.Package)
		if packdecl.NpmSpecIsPinned(version) {
			return st, Current, nil
		}
	}
	lock, err := pidlock.Acquire(f.lockPath(p.Bin()), pidlock.Mode{Wait: true, Bound: 5 * time.Minute, Cancel: ctx.Done()}, nil)
	if err != nil {
		return st, "", err
	}
	defer lock.Release()
	st = f.Status(p)
	if st.Disposition != Provisioned || st.Pending != "" {
		return st, "", fmt.Errorf("%s changed while awaiting fresh update; retry --update", p.Bin())
	}
	rec := st.Record
	var newer string
	if p.Install.Kind == "native" {
		if why := f.cannotCapture(p.Bin(), "checks it now"); why != "" {
			return st, "", fmt.Errorf("%s: %s", p.Bin(), why)
		}
		entry, e := f.recapture(p.Bin())
		if e != nil {
			return st, "", e
		}
		newer = newerCapture(p.Install, rec, entry)
	} else {
		newer, err = f.newerThan(ctx, p, rec)
		if err != nil {
			return st, "", err
		}
	}
	if err = ctx.Err(); err != nil {
		return st, "", err
	}
	rec.Checked = f.now().UTC()
	if err = f.writeRecord(rec); err != nil {
		return st, "", err
	}
	if newer == "" {
		return st, Current, nil
	}
	next, err := f.install(ctx, p)
	if err != nil {
		return st, "", err
	}
	f.say("updated %s to %s", p.Bin(), next.Version)
	return f.Status(p), Updated, nil
}
