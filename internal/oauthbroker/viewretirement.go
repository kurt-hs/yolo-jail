package oauthbroker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
)

// RetireViewRegistration removes only a jail location's broker registration. It does not read,
// create, or modify the view or any credential file. The refresh lock serializes retirement with
// broker publication so the broker cannot rewrite a view after its registration is removed.
// Needs ConfigureStore; absent registration files are an idempotent no-op.
func RetireViewRegistration(loc claudeview.Location) (bool, error) {
	if !loc.Valid() || loc.IsHost() {
		return false, errors.New("a valid workspace view location is required")
	}
	if RefreshLockPath == "" || ViewRegistryDir == "" {
		return false, errors.New("the broker store is not configured")
	}
	registration := registrationFile(loc)
	if _, err := os.Lstat(registration); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect view registration: %w", err)
	}
	removed := false
	err := lockedErr(func() error {
		err := os.Remove(registration)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove view registration: %w", err)
		}
		removed = true
		return nil
	})
	return removed, err
}
