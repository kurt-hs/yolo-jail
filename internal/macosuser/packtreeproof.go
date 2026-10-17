package macosuser

// packtreeproof.go decides when a guest pack tree (docs/reference/macos-user-provisioning.md,
// "The guest pack tree") may be removed, and removes it: at the end of the launch or capture that
// staged it, and from `yolo prune --apply` for a tree an earlier one had to keep.
//
// # The proof, and why it needs no root
//
// A tree may go once nothing that could read it, execute from it or restart from it is left. Two
// kinds of process ever touch it:
//
//   - THE WRITERS: the host user's own /bin/cp and /bin/chmod, run through Deps.Run, which
//     returns only once the process it started has exited and been reaped. Neither starts a
//     process of its own. So a writer whose Run returned has stopped.
//   - THE GUEST: the bootstrap, the provisioning stage, the jail-daemon supervisor, the service
//     probe, the session and everything any of them starts, all of it the sandbox account's —
//     `sudo -n --user=_yolojail /usr/bin/env …` is the only way in, and Seatbelt refuses a setuid
//     exec, so a guest process cannot leave that uid. The host user can read neither its
//     arguments nor its open files, but every process's pid, parent, uids and start time are
//     public (kern.proc.all, no privilege). That is enough: a guest process is this launch's when
//     the launch is among its ancestors, and one that left the launch's tree (a daemon that
//     double-forked onto launchd) started after the launch staged its tree.
//
// So a sandbox-account process is a HOLDER of a tree made at time A unless it is provably
// someone else's: its ancestors include another launch that is still running (named by that
// launch's held claim, below), or it started before A, when nothing could have handed it the
// tree. Any process the table could not be read for, and every escaped process started since A,
// keeps the tree. A table that cannot be read at all keeps it too: unknown keeps the bytes.
//
// # Claims
//
// A launch or capture holds a CLAIM on its tree from before the tree exists until it has decided
// its fate: an exclusive flock on <global storage>/locks/macos-user-pack-trees/<leaf>.lock, made
// the way a session record is (held under a pending name, then renamed, so it never exists
// unlocked), holding the launcher's pid. A claim does two things for `yolo prune`: a claimed tree
// is a running launch's and is not touched, and a process descended from a claim's pid belongs
// to that launch, which is running, so it is not counted against any other tree.
//
// Prune lists the trees BEFORE it reads the claims: a tree it lists was reserved after its
// launch's claim was renamed into place, so the claim is visible to the later read unless that
// launch has already ended.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// GuestProc is one process as the holder proof reads it: its pid, its parent's, whether any of
// its uids is the sandbox account's, and when it started. Name is the command name the table
// carries for it (p_comm, "" when unknown): never part of the proof, which a process could
// rename itself out of, only what a report prints beside the pid so a kept tree says what kept it.
type GuestProc struct {
	PID, PPID int
	Sandbox   bool
	Start     time.Time
	Name      string
}

// packTreeClaimLeaf is the claims' directory under locks/.
const packTreeClaimLeaf = "macos-user-pack-trees"

// PackTreeClaimsDir is where every launch's and capture's pack-tree claim lives.
func PackTreeClaimsDir() string {
	return filepath.Join(paths.GlobalStorage(), "locks", packTreeClaimLeaf)
}

// packTreeAnchorSlack moves a tree's anchor earlier than the moment it was reserved, so a process
// the clock reads as having started a moment before its tree is still counted.
const packTreeAnchorSlack = 2 * time.Second

// packTreeSettleBound is how long a finishing launch waits for its guest processes to exit
// (a supervisor that was just told to stop takes a moment) before it keeps the tree; and
// packTreeSettlePoll how often it re-reads the table meanwhile. Variables so tests can shorten them.
var (
	packTreeSettleBound = 3 * time.Second
	packTreeSettlePoll  = 50 * time.Millisecond
)

// packTreeHolders returns every sandbox-account process that may hold a tree reserved at anchor:
// one whose ancestors include self (this launch, still running), or one that no running launch
// in others is an ancestor of and that started at or after anchor. self is 0 when the caller
// launched nothing (prune).
func packTreeHolders(procs []GuestProc, self int, others map[int]bool, anchor time.Time) []GuestProc {
	byPID := make(map[int]GuestProc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	var holders []GuestProc
	for _, p := range procs {
		if !p.Sandbox {
			continue
		}
		ours, elsewhere := false, false
		seen := map[int]bool{p.PID: true}
		for pid := p.PPID; pid > 1 && !seen[pid]; {
			seen[pid] = true
			if self > 0 && pid == self {
				ours = true
				break
			}
			if others[pid] {
				elsewhere = true
				break
			}
			parent, ok := byPID[pid]
			if !ok {
				break
			}
			pid = parent.PPID
		}
		switch {
		case ours:
			holders = append(holders, p)
		case elsewhere:
		case !p.Start.Before(anchor):
			holders = append(holders, p)
		}
	}
	return holders
}

// packTreeClaims is what the claims directory says: the leaves of every claimed tree and the pid
// of every launch holding one. known is false when it could not be read, and then nothing may be
// removed on its say-so.
type packTreeClaims struct {
	known  bool
	leaves map[string]bool
	pids   map[int]bool
}

// readPackTreeClaims reads dir. A claim nobody holds is a launch that died before letting it go;
// it claims nothing, and is removed. A dir that does not exist holds no claims.
func readPackTreeClaims(dir string) packTreeClaims {
	c := packTreeClaims{known: true, leaves: map[string]bool{}, pids: map[int]bool{}}
	if dir == "" {
		return c
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return c
	}
	if err != nil {
		c.known = false
		return c
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, sessionRecordSuffix) {
			continue // a pending claim names a tree that does not exist yet
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			c.known = false
			continue
		}
		held, state := claimOpenedSessionRecord(f, path)
		switch state {
		case sessionGone:
			_ = os.Remove(path)
			_ = held.Close()
		case sessionLive:
			c.leaves[strings.TrimSuffix(name, sessionRecordSuffix)] = true
			if b, err := os.ReadFile(path); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
					c.pids[pid] = true
				}
			}
		default:
			c.known = false
		}
	}
	return c
}

// openPackTreeClaim creates and holds the claim on the tree at packRoot, holding pid. It appears
// under its final name already held and already holding the pid.
func openPackTreeClaim(dir, packRoot string, pid int) (*sessionRecord, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	final := filepath.Join(dir, filepath.Base(packRoot)+sessionRecordSuffix)
	f, pending, err := createHeldPendingRecord(final)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(f, strconv.Itoa(pid)+"\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(pending)
		return nil, err
	}
	if err := os.Rename(pending, final); err != nil {
		_ = f.Close()
		_ = os.Remove(pending)
		return nil, err
	}
	return &sessionRecord{path: final, lock: f}, nil
}

// packTreeUse is one launch's (or capture's) dealings with its guest pack tree, which
// finishPackTree reads to decide the tree's fate.
type packTreeUse struct {
	// root is the tree's path; "" when the launch stages none.
	root string
	// owned is set once the launch's exclusive mkdir of root succeeded.
	owned bool
	// consumerDispatched is set before the first guest process that reads root is started.
	consumerDispatched bool
	// anchor is when the launch began reserving root, less packTreeAnchorSlack.
	anchor time.Time
	// claim is the launch's claim on root, nil when it could not be made.
	claim *sessionRecord
}

// beginPackTree claims the tree at root before anything reserves it. A claim that cannot be made
// is said in one line and the launch goes on: its own end still removes the tree, but a `yolo
// prune` running meanwhile cannot tell the tree is in use.
func beginPackTree(deps Deps, out printer, root string) *packTreeUse {
	u := &packTreeUse{root: root, anchor: time.Now().Add(-packTreeAnchorSlack)}
	if root == "" || deps.PackTreeClaimDir == nil {
		return u
	}
	dir := deps.PackTreeClaimDir()
	if dir == "" {
		return u
	}
	claim, err := openPackTreeClaim(dir, root, os.Getpid())
	if err != nil {
		out.printf("[yellow]Could not claim guest pack tree %s (%v);[/yellow] do not run `yolo prune --apply` "+
			"until this launch has ended.", shquote.QuoteDisplay(root), err)
		return u
	}
	u.claim = claim
	return u
}

// finishPackTree removes the launch's own tree once nothing may still hold it. A holder still
// running when the launch's wait ends hands the tree, with its claim, to the pack-tree reaper
// (packtreereap.go), which removes it once the holders have exited; only a launch that cannot
// hand it over keeps it, and says so in one line naming the command that removes it later. A
// tree the launch does not own is never touched. The claim goes last, whatever was decided,
// unless the reaper took it.
func finishPackTree(deps Deps, out printer, u *packTreeUse) {
	if u == nil {
		return
	}
	defer u.claim.close()
	if !u.owned || u.root == "" {
		return
	}
	path := shquote.QuoteDisplay(u.root)
	if u.consumerDispatched {
		holders, err := awaitNoPackTreeHolders(deps, u)
		if err != nil {
			out.printf("[yellow]Kept guest pack tree %s:[/yellow] could not list the sandbox account's "+
				"processes (%v); `yolo prune --apply` removes it once none may read it.", path, err)
			return
		}
		if len(holders) > 0 {
			handed, err := handOffPackTree(deps, out, u, holders)
			if handed {
				return
			}
			why := ""
			if err != nil {
				why = " (its removal in the background could not start: " + err.Error() + ")"
			}
			out.printf("[yellow]Kept guest pack tree %s:[/yellow] %s may still read it%s; "+
				"`yolo prune --apply` removes it once %s exited.", path, describeHolders(holders), why,
				map[bool]string{true: "it has", false: "they have"}[len(holders) == 1])
			return
		}
	}
	if removeSealedTree(deps.Run, u.root) {
		return
	}
	out.printf("[yellow]Could not remove guest pack tree %s;[/yellow] remove it with `%s -R -- u+w %s && %s -rf -- %s`.",
		path, chmodBin, path, rmBin, path)
}

// awaitNoPackTreeHolders reads the process table until no process may hold u's tree, for at most
// packTreeSettleBound, and returns the holders of the last read.
func awaitNoPackTreeHolders(deps Deps, u *packTreeUse) ([]GuestProc, error) {
	if deps.GuestProcesses == nil {
		return nil, errors.New("this build has no process-table reader")
	}
	others := map[int]bool{}
	if deps.PackTreeClaimDir != nil {
		for pid := range readPackTreeClaims(deps.PackTreeClaimDir()).pids {
			others[pid] = true
		}
	}
	self := os.Getpid()
	delete(others, self)
	deadline := time.Now().Add(packTreeSettleBound)
	for {
		procs, err := deps.GuestProcesses()
		if err != nil {
			return nil, err
		}
		holders := packTreeHolders(procs, self, others, u.anchor)
		if len(holders) == 0 || !time.Now().Before(deadline) {
			return holders, nil
		}
		time.Sleep(packTreeSettlePoll)
	}
}

// describeHolders names up to three holders by pid, each with its command name when the table
// has one: "sandbox process 812 (sleep)", "sandbox processes 812, 913 and 2 more".
func describeHolders(holders []GuestProc) string {
	sorted := append([]GuestProc(nil), holders...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PID < sorted[j].PID })
	label := func(h GuestProc) string {
		if printableName(h.Name) == "" {
			return strconv.Itoa(h.PID)
		}
		return strconv.Itoa(h.PID) + " (" + richtext.Escape(printableName(h.Name)) + ")"
	}
	if len(sorted) == 1 {
		return "sandbox process " + label(sorted[0])
	}
	shown := sorted
	if len(shown) > 3 {
		shown = shown[:3]
	}
	words := make([]string, len(shown))
	for i, h := range shown {
		words[i] = label(h)
	}
	s := "sandbox processes " + strings.Join(words, ", ")
	if rest := len(sorted) - len(shown); rest > 0 {
		s += fmt.Sprintf(" and %d more", rest)
	}
	return s
}

// printableName is a process's command name with every control byte dropped: the sandbox account
// chooses the name it execs under, and it reaches the launch's output, prune's and a log.
func printableName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
}

// removeSealedTree unseals the tree at root (its 0555 directories block their owner too) and
// removes it, as the host user that made it.
func removeSealedTree(run func([]string) int, root string) bool {
	if run == nil {
		return false
	}
	for _, cmd := range append(unsealCommands(root), []string{rmBin, "-rf", "--", root}) {
		if run(cmd) != 0 {
			return false
		}
	}
	return true
}

// PackTreeReapEntry is one guest pack tree `yolo prune` looked at: removed (or, on a dry run,
// removable), or kept for Reason.
type PackTreeReapEntry struct {
	Path  string
	Bytes int64
	// Reason is why a kept tree was kept, in words that group (the report counts by it), and
	// Detail its own specifics (which processes), or "".
	Reason, Detail string
	Err            error
}

// PackTreeReap is what one sweep of the staging root's packs directory found.
type PackTreeReap struct {
	// Dir is the packs directory swept.
	Dir string
	// Applicable is false when Dir does not exist: no macos-user launch ever staged here.
	Applicable bool
	// Declined, when set, is why nothing was removed at all.
	Declined string
	// Removed are the trees removed (or removable, on a dry run); Kept the ones kept, with why.
	Removed, Kept []PackTreeReapEntry
}

// Bytes is what Removed adds up to.
func (r PackTreeReap) Bytes() int64 {
	var n int64
	for _, e := range r.Removed {
		if e.Err == nil {
			n += e.Bytes
		}
	}
	return n
}

// PrunePackTrees is `yolo prune`'s sweep of the per-launch guest pack trees in the staging root:
// every one this macOS user staged that no running launch claims and no sandbox process may
// still read. apply false removes nothing.
func PrunePackTrees(apply bool) PackTreeReap {
	return reapPackTrees(stateDir, PackTreeClaimsDir(), SessionRecordsDir(), guestProcessesReal, runReal,
		os.Geteuid(), apply)
}

// liveSessionCnames is the container name of every session whose liveness record (sessionfiles.go)
// is held in dir: a running launch of that workspace, which may be between steps with no guest
// process alive. known is false when a record could not be asked.
func liveSessionCnames(dir string) (cnames map[string]bool, known bool) {
	cnames = map[string]bool{}
	if dir == "" {
		return cnames, true
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*"+sessionRecordSuffix))
	if err != nil {
		return cnames, false
	}
	known = true
	for _, path := range matches {
		cname, _, ok := parseSessionKey(strings.TrimSuffix(filepath.Base(path), sessionRecordSuffix))
		if !ok {
			continue
		}
		f, state := claimGoneSessionRecord(path)
		switch state {
		case sessionGone:
			_ = f.Close() // the next launch's sweep removes what that session left
		case sessionLive:
			cnames[cname] = true
		default:
			known = false
		}
	}
	return cnames, known
}

// reapPackTrees is PrunePackTrees with every input injected: sd the staging root, claimDir the
// claims, sessionDir the session liveness records, procs the process table, run the host user's command runner, uid the host user.
//
// Only per-launch trees (<cname>.<tree id>) are considered; a legacy workspace-keyed tree is left
// alone. Each is kept when another account owns it, when a running launch claims it, when a
// session of its workspace is running (a launch whose claim failed, or an older yolo that makes no
// claims, between steps with no guest alive), or when a
// sandbox process may still read it (packTreeHolders, anchored at the tree's mtime: the copy into
// it is the last change it sees before any guest starts, and a sealed tree changes no more).
func reapPackTrees(sd, claimDir, sessionDir string, procs func() ([]GuestProc, error), run func([]string) int, uid int, apply bool) PackTreeReap {
	r := PackTreeReap{Dir: filepath.Join(sd, packsLeaf)}
	entries, err := os.ReadDir(r.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return r
	}
	r.Applicable = true
	if err != nil {
		r.Declined = "the directory could not be read (" + err.Error() + ")"
		return r
	}
	type tree struct {
		path  string
		mtime time.Time
	}
	var trees []tree
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.Contains(name, ".") {
			continue
		}
		path := filepath.Join(r.Dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
			r.Kept = append(r.Kept, PackTreeReapEntry{Path: path, Reason: "another account's"})
			continue
		}
		trees = append(trees, tree{path: path, mtime: info.ModTime()})
	}
	if len(trees) == 0 {
		return r
	}
	// AFTER the listing: see the file comment.
	claims := readPackTreeClaims(claimDir)
	if !claims.known {
		r.Declined = "the launches' claims in " + claimDir + " could not all be read"
		return r
	}
	sessions, known := liveSessionCnames(sessionDir)
	if !known {
		r.Declined = "the sessions' liveness records in " + sessionDir + " could not all be read"
		return r
	}
	table, err := procs()
	if err != nil {
		r.Declined = "the sandbox account's processes could not be listed (" + err.Error() + ")"
		return r
	}
	for _, t := range trees {
		e := PackTreeReapEntry{Path: t.path, Bytes: treeBytes(t.path)}
		if claims.leaves[filepath.Base(t.path)] {
			e.Reason = "a running launch's"
			r.Kept = append(r.Kept, e)
			continue
		}
		if cname, _, ok := strings.Cut(filepath.Base(t.path), "."); ok && sessions[cname] {
			e.Reason = "its workspace has a running session"
			r.Kept = append(r.Kept, e)
			continue
		}
		if h := packTreeHolders(table, 0, claims.pids, t.mtime.Add(-packTreeAnchorSlack)); len(h) > 0 {
			e.Reason, e.Detail = "a sandbox process may still read it", describeHolders(h)
			r.Kept = append(r.Kept, e)
			continue
		}
		if apply && !removeSealedTree(run, t.path) {
			e.Err = errors.New("unseal or remove failed")
		}
		r.Removed = append(r.Removed, e)
	}
	return r
}

// treeBytes is the apparent size of every regular file under root; 0 when it cannot be walked.
func treeBytes(root string) int64 {
	var n int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
