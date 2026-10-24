package brokercore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// authority.go is the source-neutral half of a broker's host authority: binding a fresh
// broker lifetime to the launch's scope file under a lifetime lease, proving that binding is
// still live before each decision, and ending it. What a source then decides with the live
// scope (its classifier and its revalidation) stays with the source.

// BindLease binds a fresh broker start to the host-written scope file at path for source,
// and takes the lifetime lease that proves the binding live: an exclusively locked
// `.lifetime-<start>` file in the source's store directory. It returns the scope as bound and
// the lease, which the caller keeps open for the broker's lifetime and releases with
// CloseLease.
func BindLease(source, path string, sf brokerscope.File, start string) (brokerscope.File, *os.File, error) {
	if path != paths.BrokerScopeFile(source, sf.LaunchID) {
		return brokerscope.File{}, nil, errors.New("scope is not the host launch path")
	}
	if sf.Source != source || sf.Container == "" || sf.Workspace == "" || sf.PID <= 0 {
		return brokerscope.File{}, nil, errors.New("missing host launch identity")
	}
	if !ValidLifetimeToken(start) || !ValidLifetimeToken(sf.LaunchID) {
		return brokerscope.File{}, nil, errors.New("invalid fresh broker start")
	}
	var lease *os.File
	f, err := brokerscope.BindBroker(path, sf, start, func() error {
		dir := paths.BrokerSourceDir(source)
		if err := collectInertLeases(source, dir); err != nil {
			return err
		}
		var err error
		lease, err = os.OpenFile(filepath.Join(dir, ".lifetime-"+start), os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		if err = syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return err
		}
		if err = json.NewEncoder(lease).Encode(lifetimeLease{Version: lifetimeLeaseVersion, Source: source, Start: start, LaunchID: sf.LaunchID}); err != nil {
			return err
		}
		return lease.Sync()
	})
	if err != nil {
		if lease != nil {
			lease.Close()
			os.Remove(lease.Name())
		}
		return brokerscope.File{}, nil, err
	}
	return f, lease, nil
}

// CloseLease serializes lifetime invalidation with authorization. A process death also
// releases its lease, so PID reuse cannot resurrect the random start binding.
func CloseLease(source string, lease *os.File) error {
	if lease == nil {
		return nil
	}
	return brokerlock.WithTransaction(paths.BrokerSourceDir(source), func() error {
		if err := lease.Close(); err != nil {
			return err
		}
		return os.Remove(lease.Name())
	})
}

// Live re-reads the scope file at path and returns it only while the broker lifetime bound
// as start is live: its lease still held, the file still naming that start and a running
// broker and launch, and the launch identity unchanged from bound. Its reads are non-locking:
// store callbacks already hold the shared source transaction.
func Live(source, path string, bound brokerscope.File, start string) (brokerscope.File, error) {
	if start == "" || strings.ContainsAny(start, "/\\.") {
		return brokerscope.File{}, errors.New("invalid broker start")
	}
	lease, err := os.OpenFile(filepath.Join(paths.BrokerSourceDir(source), ".lifetime-"+start), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return brokerscope.File{}, err
	}
	err = syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	lease.Close()
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return brokerscope.File{}, errors.New("broker lifetime lease is not live; relaunch the jail")
	}
	f, err := brokerscope.ReadFile(path)
	if err != nil {
		return f, err
	}
	if f.BrokerStart == "" || f.BrokerStart != start || f.BrokerPID <= 0 || syscall.Kill(f.BrokerPID, 0) != nil || f.PID <= 0 || syscall.Kill(f.PID, 0) != nil {
		return f, errors.New("broker lifetime is not live; relaunch the jail")
	}
	if f.Source != bound.Source || f.LaunchID != bound.LaunchID || f.Container != bound.Container || f.Workspace != bound.Workspace || f.PID != bound.PID {
		return f, errors.New("host launch identity changed; relaunch the jail")
	}
	return f, nil
}
