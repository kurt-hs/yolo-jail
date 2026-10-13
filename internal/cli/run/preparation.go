package run

import (
	"fmt"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// PreparationRequest is the explicit launch's fixed authorized vector. It is
// deliberately separate from capture's force-rebuild mode.
type PreparationRequest interface {
	Boundary(key string) (packsrc.CheckStart, string, error)
	Guard(key string, commit func() error) error
}

func (o *Options) recordFreshFailure(err error) {
	if err == nil {
		return
	}
	freshFailureMu.Lock()
	defer freshFailureMu.Unlock()
	if o.freshFailure == nil {
		o.freshFailure = err
	}
}
func (o *Options) refuseFreshFailure() bool {
	if !o.Update {
		return false
	}
	freshFailureMu.Lock()
	defer freshFailureMu.Unlock()
	if o.freshFailure == nil {
		return false
	}
	fmt.Fprintf(o.Stderr, "Fresh preparation refused: %v — repair and retry --update, or launch without --update\n", o.freshFailure)
	return true
}

var freshFailureMu sync.Mutex
