package brokerscope

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// live.go is the host side of a live scope change (docs/design/live-config-apply.md §7): `yolo
// config apply` finds the scope files of the brokers serving a running jail (LiveFiles) and
// rewrites only their repositories (ReplaceRepos), inside the source's lock transaction that
// every broker decision already takes, so a broker sees the old scope or the new one and never
// a torn file. The scope is still written only from a gate's approved result, never from
// re-reading the config.

// LiveFile is one scope file a running broker is bound to, as read.
type LiveFile struct {
	Path string
	File File // as read, identity fields included
}

// LiveFiles returns every scope file of source whose Container is container and whose
// Workspace is workspace, and whose launching process and bound broker are both alive. A file
// no broker has bound (BrokerPID 0) is not live, and neither is one that does not parse. It
// reads under the source's lock transaction.
func LiveFiles(source, container, workspace string) ([]LiveFile, error) {
	if source == "" || strings.ContainsAny(source, "/\\.") {
		return nil, errors.New("scope file: bad source")
	}
	var out []LiveFile
	err := brokerlock.WithTransaction(paths.BrokerSourceDir(source), func() error {
		dir := filepath.Dir(paths.BrokerScopeFile(source, "x"))
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			f, err := ReadFile(p)
			if err != nil || f.Source != source || f.Container != container || f.Workspace != workspace {
				continue
			}
			if !processAlive(f.PID) || !processAlive(f.BrokerPID) {
				continue
			}
			out = append(out, LiveFile{Path: p, File: f})
		}
		return nil
	})
	return out, err
}

// processAlive is kill(pid, 0) answering anything but ESRCH: a process this user may not
// signal still exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// ReplaceRepos rewrites the Repos of the scope file at path and nothing else, under the
// source's transaction. It refuses a path that is not expect's launch path, a symlink, an
// entry ValidPattern rejects, and a file whose identity (source, launch id, container,
// workspace, launching pid) differs from expect's: the broker validates those fields on every
// request, so a rewrite that changed them would refuse every call. Every other field, the
// broker binding (broker_start, broker_pid) and live_scope included, is kept as read. The
// write is atomic and 0600.
func ReplaceRepos(path string, expect File, repos []string) error {
	if expect.Source == "" || expect.LaunchID == "" || strings.ContainsAny(expect.Source+expect.LaunchID, "/\\.") {
		return errors.New("scope file: bad source or launch id")
	}
	if path != paths.BrokerScopeFile(expect.Source, expect.LaunchID) {
		return errors.New("scope path is not the host launch path")
	}
	for _, r := range repos {
		if !ValidPattern(r) {
			return fmt.Errorf("scope file: %q is not an OWNER/REPO or a whole-segment wildcard", r)
		}
	}
	return brokerlock.WithTransaction(paths.BrokerSourceDir(expect.Source), func() error {
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return errors.New("scope file is not a regular file")
		}
		f, err := ReadFile(path)
		if err != nil {
			return err
		}
		if f.Source != expect.Source || f.LaunchID != expect.LaunchID || f.Container != expect.Container ||
			f.Workspace != expect.Workspace || f.PID != expect.PID {
			return errors.New("scope identity changed; the jail this scope was approved for is not the one running")
		}
		f.Repos = append([]string{}, repos...)
		got, err := write(f)
		if err != nil {
			return err
		}
		if got != path {
			return errors.New("scope path is not its host launch path")
		}
		return nil
	})
}
