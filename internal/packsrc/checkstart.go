package packsrc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// CheckStart is start authority, not proof of a successful check or admitted build.
// Lock order is build -> record -> start transaction. A request releases the
// start transaction before waiting for any record, mirror or build lock.
type CheckStart struct {
	Epoch                    string
	Seq                      uint64
	InputIdentity, RequestID string
	StartedAt                time.Time
}

func (s *CheckStart) Qualifies(boundary CheckStart, identity string) bool {
	return s != nil && s.Epoch == boundary.Epoch && s.Seq > boundary.Seq && s.InputIdentity == identity
}

func (s *Store) withCheckStart(owner string, fn func(*CheckStart) error) error {
	if s.Ctx != nil && s.Ctx.Err() != nil {
		return s.Ctx.Err()
	}
	mode := pidlock.Mode{Wait: true, Bound: 10 * time.Second}
	if s.Ctx != nil {
		mode.Cancel = s.Ctx.Done()
	}
	var onWait func(int)
	if s.checkStartWaiting != nil {
		onWait = func(int) { s.checkStartWaiting() }
	}
	lock, err := pidlock.Acquire(filepath.Join(s.Dir, "locks", "check-start-"+mirrorSlug(owner)+".lock"), mode, onWait)
	if errors.Is(err, pidlock.ErrCanceled) && s.Ctx != nil && s.Ctx.Err() != nil {
		// A cancelled wait reports the context's error, as the check before the wait does: a
		// caller asks errors.Is(err, context.Canceled), not which side of the lock it was on.
		return fmt.Errorf("%w: %w", s.Ctx.Err(), err)
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	path := filepath.Join(s.Dir, "checks", "start-"+mirrorSlug(owner)+".json")
	data, err := os.ReadFile(path)
	var start CheckStart
	if errors.Is(err, os.ErrNotExist) {
		nonce := make([]byte, 16)
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		start.Epoch = hex.EncodeToString(nonce)
		if err = saveCheckStart(path, start); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err = json.Unmarshal(data, &start); err != nil || start.Epoch == "" {
		return fmt.Errorf("check-start authority corrupt; inspect %s and retry with repaired preparation state", path)
	}
	// A deleted/reset or rolled-back ledger cannot certify a recorded winner.
	if rec, e := s.LoadCheckRecord(owner); e == nil && rec.Fresh != nil && rec.Fresh.Epoch == start.Epoch && rec.Fresh.Seq > start.Seq {
		return fmt.Errorf("check-start sequence rolled back; repair %s before retrying --update", path)
	}
	if err = fn(&start); err != nil {
		return err
	}
	return nil
}
func saveCheckStart(path string, start CheckStart) error {
	data, err := json.Marshal(start)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".check-start-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) CheckBoundary(owner string) (boundary CheckStart, err error) {
	err = s.withCheckStart(owner, func(start *CheckStart) error { boundary = *start; return nil })
	return
}
func (s *Store) beginCheck(owner, identity, request string, boundary *CheckStart, at time.Time, guard func(func() error) error) (begun CheckStart, err error) {
	err = s.withCheckStart(owner, func(start *CheckStart) error {
		if boundary != nil && (start.Epoch != boundary.Epoch || start.Seq < boundary.Seq) {
			return errors.New("fresh check authority changed; retry --update")
		}
		if start.Seq == math.MaxUint64 {
			return errors.New("check-start sequence exhausted; repair preparation state and retry")
		}
		start.Seq++
		start.InputIdentity = identity
		start.RequestID = request
		start.StartedAt = at
		begun = *start
		save := func() error {
			return saveCheckStart(filepath.Join(s.Dir, "checks", "start-"+mirrorSlug(owner)+".json"), *start)
		}
		if guard != nil {
			return guard(save)
		}
		return save()
	})
	return
}

// WithFreshAuthority rejects reset/rollback even if a historical record still
// carries a numerically qualifying check. Called inside the record transaction.
func (s *Store) WithFreshAuthority(owner string, boundary CheckStart, begun *CheckStart, identity string, commit func() error) error {
	return s.withCheckStart(owner, func(current *CheckStart) error {
		if !begun.Qualifies(boundary, identity) || current.Epoch != boundary.Epoch || current.Seq < begun.Seq {
			return errors.New("fresh check authority reset or changed; retry --update")
		}
		return commit()
	})
}

// BeginFreshCheck exposes the same short start protocol to a plain-source
// resolver that already owns its keyed/global pin transaction. It does no transport.
func (s *Store) BeginFreshCheck(owner, identity, request string, boundary CheckStart, guard func(func() error) error) (CheckStart, error) {
	var begun CheckStart
	err := s.WithCheckRecord(owner, nil, func(r *CheckRecord, readErr error, save func() error) (bool, error) {
		if readErr != nil {
			return false, readErr
		}
		var err error
		begun, err = s.beginCheck(owner, identity, request, &boundary, time.Now(), guard)
		if err != nil {
			return false, err
		}
		r.CheckedAt = begun.StartedAt.Unix()
		return true, nil
	})
	return begun, err
}
