package ghbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Authority is host-owned launch scope and fresh broker identity. Its reads are
// non-locking: store callbacks already hold the shared source transaction.
type Authority struct {
	path  string
	scope brokerscope.File
	start string
	lease *os.File
}

func BindAuthority(path string, sf brokerscope.File, start string) (*Authority, error) {
	if path != paths.BrokerScopeFile(Source, sf.LaunchID) {
		return nil, errors.New("scope is not the host launch path")
	}
	if sf.Source != Source || sf.Container == "" || sf.Workspace == "" || sf.PID <= 0 {
		return nil, errors.New("missing host launch identity")
	}
	if !validLifetimeToken(start) || !validLifetimeToken(sf.LaunchID) {
		return nil, errors.New("invalid fresh broker start")
	}
	var lease *os.File
	f, err := brokerscope.BindBroker(path, sf, start, func() error {
		dir := paths.BrokerSourceDir(Source)
		if err := collectInertLeases(dir); err != nil {
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
		if err = json.NewEncoder(lease).Encode(lifetimeLease{Version: lifetimeLeaseVersion, Source: Source, Start: start, LaunchID: sf.LaunchID}); err != nil {
			return err
		}
		return lease.Sync()
	})
	if err != nil {
		if lease != nil {
			lease.Close()
			os.Remove(lease.Name())
		}
		return nil, err
	}
	return &Authority{path: path, scope: f, start: start, lease: lease}, nil
}

// Close serializes lifetime invalidation with authorization. A process death also
// releases its lease, so PID reuse cannot resurrect the random start binding.
func (a *Authority) Close() error {
	if a == nil || a.lease == nil {
		return nil
	}
	return brokerlock.WithTransaction(paths.BrokerSourceDir(Source), func() error {
		if err := a.lease.Close(); err != nil {
			return err
		}
		return os.Remove(a.lease.Name())
	})
}
func (a *Authority) live() (brokerscope.File, error) {
	if a == nil {
		return brokerscope.File{}, errors.New("host authority unavailable; relaunch the jail")
	}
	if a.start == "" || strings.ContainsAny(a.start, "/\\.") {
		return brokerscope.File{}, errors.New("invalid broker start")
	}
	lease, err := os.OpenFile(filepath.Join(paths.BrokerSourceDir(Source), ".lifetime-"+a.start), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return brokerscope.File{}, err
	}
	err = syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	lease.Close()
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return brokerscope.File{}, errors.New("broker lifetime lease is not live; relaunch the jail")
	}
	f, err := brokerscope.ReadFile(a.path)
	if err != nil {
		return f, err
	}
	if f.BrokerStart == "" || f.BrokerStart != a.start || f.BrokerPID <= 0 || syscall.Kill(f.BrokerPID, 0) != nil || f.PID <= 0 || syscall.Kill(f.PID, 0) != nil {
		return f, errors.New("broker lifetime is not live; relaunch the jail")
	}
	if f.Source != a.scope.Source || f.LaunchID != a.scope.LaunchID || f.Container != a.scope.Container || f.Workspace != a.scope.Workspace || f.PID != a.scope.PID {
		return f, errors.New("host launch identity changed; relaunch the jail")
	}
	return f, nil
}

// Classify re-reads current host sets and current approved launch scope. Unknown
// or unsupported policy is an error, never a fallback to the default write set.
func (a *Authority) Classify(argv []string, repo string) (Decision, brokerstore.Policy, error) {
	f, err := a.live()
	if err != nil {
		return Decision{}, brokerstore.Policy{}, err
	}
	d := Classify(argv, repo, NewScope(append(append([]string(nil), f.Repos...), f.Widened...)))
	if d.Outcome != OutcomeStanding && d.Outcome != OutcomeWindowed {
		return d, brokerstore.Policy{}, errors.New(d.Reason)
	}
	d = Classify(d.Argv, repo, NewScope(append(append([]string(nil), f.Repos...), f.Widened...)))
	for _, r := range d.Repos {
		if err = brokerscope.ValidateCurrent(a.path, a.scope, r); err != nil {
			return d, brokerstore.Policy{}, err
		}
	}
	sets, err := config.CurrentBrokeredSets(Source)
	if err != nil {
		return d, brokerstore.Policy{}, err
	}
	if sets == nil {
		sets = []config.BrokeredSet{{Name: SetReadOnly, Mode: "standing"}, {Name: SetReadWrite, Mode: "windowed"}}
	}
	// Explicit admit replacement versus augmentation is not yet ruled (§5.7).
	// Refuse the entire policy rather than silently applying a broader default.
	for _, s := range sets {
		if s.Admit != nil || (s.Name != SetReadOnly && s.Name != SetReadWrite) {
			return d, brokerstore.Policy{}, fmt.Errorf("permission set %s has unsupported explicit rules; use default set rules on the host", s.Name)
		}
	}
	base := d.Set
	// A standing set that covers the command takes precedence over a windowed one.
	for _, mode := range []string{"standing", "windowed"} {
		for _, s := range sets {
			if s.Mode != mode || (s.Name != base && s.Name != SetReadWrite) {
				continue
			}
			d.Set = s.Name
			if mode == "standing" {
				d.Outcome = OutcomeStanding
				return d, brokerstore.Policy{}, nil
			}
			p := brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: 12 * time.Hour, Session: true, Uses: 25}
			if s.Window != "" {
				p.Window, err = time.ParseDuration(s.Window)
				if err != nil {
					return d, p, err
				}
			}
			if s.MaxWindow != "" && s.MaxWindow != "session" {
				p.MaxWindow, err = time.ParseDuration(s.MaxWindow)
				p.Session = false
				if err != nil {
					return d, p, err
				}
			}
			if s.Uses != 0 {
				p.Uses = s.Uses
			}
			if p.Window <= 0 || p.Window > p.MaxWindow || p.Uses < 1 {
				return d, p, errors.New("invalid host set bounds; repair the host policy")
			}
			d.Outcome = OutcomeWindowed
			return d, p, nil
		}
	}
	return d, brokerstore.Policy{}, errors.New("no host permission set admits this command; configure its set on the host")
}
func (a *Authority) Revalidate(q brokerstore.Request) (brokerstore.Policy, error) {
	if a == nil || q.Start != a.start || q.LaunchID != a.scope.LaunchID || q.Workspace != a.scope.Workspace || q.Jail != paths.JailShortHash(a.scope.Container) {
		return brokerstore.Policy{}, errors.New("request holder is not this live host launch")
	}
	d, p, err := a.Classify(q.Argv, q.Repo)
	if err != nil {
		return p, err
	}
	if d.Outcome != OutcomeWindowed || d.Set != q.Set || !slices.Equal(d.Argv, q.Argv) || strings.Join(d.Repos, ",") != q.Repo {
		return p, errors.New("current classification differs; refile the command")
	}
	return p, nil
}

// ApprovalAuthority is the standalone host frontend adapter. It derives the
// scope path from a validated launch id, not from a caller-provided path.
func ApprovalAuthority(q brokerstore.Request) (brokerstore.Policy, error) {
	if q.LaunchID == "" || strings.ContainsAny(q.LaunchID, "/\\.") {
		return brokerstore.Policy{}, errors.New("request has no valid host launch identity")
	}
	path := paths.BrokerScopeFile(Source, q.LaunchID)
	f, err := brokerscope.ReadFile(path)
	if err != nil {
		return brokerstore.Policy{}, err
	}
	a := &Authority{path: path, scope: f, start: q.Start}
	return a.Revalidate(q)
}
