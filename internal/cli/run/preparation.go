package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// PreparationRequest is the explicit launch's fixed authorized vector. It is
// deliberately separate from capture's force-rebuild mode.
type PreparationRequest interface {
	Boundary(key string) (packsrc.CheckStart, string, error)
	Guard(key string, commit func() error) error
}

func (o *Options) refuseFreshAttach() {
	o.pr(o.Stderr).printf("Refusing --update: this workspace already has a running jail; cannot refresh a running jail on attach.")
	o.pr(o.Stderr).printf("Stop it with `yolo stop`, then retry `yolo --update -- <command>` as a fresh launch.")
}
