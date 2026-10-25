package worktreereap

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ReportName is the file, directly in the worktree directory, each pass writes its Result to.
// Its mtime is also the caller's rate limit: the pack's extension starts a pass only when it
// is older than its interval, and touches it first so concurrent starters do not pile up.
const ReportName = ".yolo-reap.json"

// lockName serializes passes over one directory; a pass that finds it held exits at once.
const lockName = ".yolo-reap.lock"

// Main is `yolo-jaild reap-worktrees --base DIR --prefix NAME [--repo DIR]... [--floor D]`:
// one pass, its Result written to DIR/ReportName. It exits 0 unless its arguments are wrong;
// a pass that removed nothing, or declined, is not a failure of the process that started it.
func Main(args []string) int { return mainTo(args, os.Stderr) }

func mainTo(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("reap-worktrees", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o Options
	var repos multiFlag
	fs.StringVar(&o.Base, "base", "", "the directory the tool makes its worktrees in")
	fs.StringVar(&o.Prefix, "prefix", "", "the directory-name prefix of the tool's worktrees")
	fs.Var(&repos, "repo", "a repository whose gone registrations beneath --base are dropped (repeatable)")
	fs.DurationVar(&o.Floor, "floor", DefaultFloor, "how long a worktree must have been idle")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if o.Base == "" || o.Prefix == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: yolo-jaild reap-worktrees --base DIR --prefix NAME [--repo DIR]... [--floor 24h]")
		return 2
	}
	o.Repos = repos
	if fi, err := os.Lstat(o.Base); err != nil || !isDir(fi) {
		return 0
	}
	lock, err := os.OpenFile(filepath.Join(o.Base, lockName), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "reap-worktrees: %v\n", err)
		return 0
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return 0 // another pass over this directory is running
	}
	res := Reap(o)
	if err := writeReport(o.Base, res); err != nil {
		fmt.Fprintf(stderr, "reap-worktrees: writing the report: %v\n", err)
	}
	return 0
}

// writeReport replaces DIR/ReportName with res, through a temp file and a rename.
func writeReport(base string, res Result) error {
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(base, ReportName+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(base, ReportName))
}

type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint(*m) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
