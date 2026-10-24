package gitbroker

import (
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// policy.go is what covers a pushed ref (docs/design/git-broker.md §5 and §5.1,
// docs/design/git-broker-plan.md ID-9 and ID-11):
//
//   - a creation (old id all zeros) under refs/heads/ is covered by an approved `create-branch`
//     grant on the repository, or on `*`, while the user's own config still lists the kind;
//   - an update or delete under refs/heads/ is covered by a live working-set entry for that
//     workspace, repository and ref;
//   - nothing else is covered (tags, and any branch outside the working set, need approval).
//
// The live scope file's grants and the user config's `grant_kinds` are read at every push, so
// a `yolo config apply` or an edit of the user config on the host reaches the next one. A
// creation the upstream then reports `ok` joins the working set; a delete reported `ok` ends
// its entry.

const (
	// grantCreateBranch is the one grant kind the git source defines.
	grantCreateBranch = "create-branch"
	// The audit sets a covered push falls in (ID-13).
	setCreateBranch = "create-branch"
	setWorkingSet   = "working-set"
	// byCreateBranch is a working-set entry's By when a creation under the grant added it.
	byCreateBranch = "create-branch"
)

// coverer decides whether one ref command is covered, and returns the set it falls in and what
// covered it: the grant kind, or the working-set entry's id.
type coverer func(gitproto.Command) (set, coveredBy string, ok bool)

// grantCovers reports whether grants hold a create-branch grant on repo or on every repository.
func grantCovers(grants []brokerscope.Grant, repo string) bool {
	for _, g := range grants {
		if g.Kind == grantCreateBranch && (g.Repo == brokerscope.AnyRepo || g.Repo == repo) {
			return true
		}
	}
	return false
}

// coverFor is the push policy for one push to repo under the live scope file f. A store it
// cannot read covers nothing, and says so once.
func (b *Broker) coverFor(f brokerscope.File, repo string, say func(string)) coverer {
	if b.cover != nil {
		return b.cover
	}
	canCreate := grantCovers(f.Grants, repo) && slices.Contains(config.BrokeredGrantKinds(Source), grantCreateBranch)
	warned := false
	storeFailed := func(err error) {
		if !warned {
			warned = true
			say("the git-broker could not read this workspace's working set (" + err.Error() + "), so no branch " +
				"in it is covered for this push. Check that the host store under " + b.store.Dir + " is readable, " +
				"then push again.")
		}
	}
	return func(c gitproto.Command) (string, string, bool) {
		if !strings.HasPrefix(c.Ref, "refs/heads/") {
			return "", "", false
		}
		e, live, err := b.store.LiveWorking(b.bound.Workspace, repo, c.Ref)
		if err != nil {
			storeFailed(err)
			return "", "", false
		}
		if c.Create() {
			// The workspace held this branch, and the upstream no longer has it: someone
			// deleted it outside the broker. Its entry ends, and the push is a creation.
			if live {
				if err := b.store.EndWorking(e.ID, brokerstore.EndGone); err != nil {
					storeFailed(err)
					return "", "", false
				}
			}
			if canCreate {
				return setCreateBranch, grantCreateBranch, true
			}
			return "", "", false
		}
		if live {
			return setWorkingSet, e.ID, true
		}
		return "", "", false
	}
}

// errNoReport is a forwarded push whose client asked for no report-status, so nothing says
// which refs the upstream took.
var errNoReport = errors.New("the push asked for no report")

// relayReport copies the upstream's answer to a forwarded push to the client, and reads
// receive-pack's report out of the same bytes. Every byte is read from the upstream even once
// the client stops reading, so the report is still known.
func relayReport(out io.Writer, upstream io.Reader, caps gitproto.Caps) (gitproto.Report, error) {
	if !caps.Has("report-status") && !caps.Has("report-status-v2") {
		_, _ = io.Copy(out, upstream)
		return gitproto.Report{}, errNoReport
	}
	type result struct {
		rep gitproto.Report
		err error
	}
	pr, pw := io.Pipe()
	done := make(chan result, 1)
	go func() {
		rep, err := gitproto.ReadReport(gitproto.NewReader(pr), gitproto.UsesSideband(caps))
		_, _ = io.Copy(io.Discard, pr)
		done <- result{rep, err}
	}()
	buf := make([]byte, 32<<10)
	clientGone := false
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			_, _ = pw.Write(buf[:n])
			if !clientGone {
				if _, werr := out.Write(buf[:n]); werr != nil {
					clientGone = true
				}
			}
		}
		if err != nil {
			break
		}
	}
	_ = pw.Close()
	r := <-done
	return r.rep, r.err
}

// record updates the working set from the upstream's report of a forwarded push: a creation
// covered by the grant and reported `ok` joins it, a delete of an entry reported `ok` ends it.
// A creation the report does not confirm records nothing (§5.1: a creation the push lost).
func (b *Broker) record(repo string, p pushRead, refs []brokeraudit.RefUpdate, rep gitproto.Report, repErr error, say func(string)) {
	for i, c := range p.cmds {
		created := c.Create() && refs[i].CoveredBy == grantCreateBranch
		deleted := c.Delete() && strings.HasPrefix(refs[i].CoveredBy, "w-")
		if !created && !deleted {
			continue
		}
		if repErr != nil {
			say("the remote's report could not be read (" + repErr.Error() + "), so " + c.Ref + " was not " +
				"recorded in this workspace's working set; if the remote took it, the next push to it needs approval.")
			continue
		}
		if !rep.OK(c.Ref) {
			continue
		}
		var err error
		if created {
			_, _, err = b.store.AddWorking(brokerstore.WorkingEntry{Workspace: b.bound.Workspace, Repo: repo,
				Kind: brokerstore.WorkingBranch, Resource: c.Ref, By: byCreateBranch})
		} else {
			err = b.store.EndWorking(refs[i].CoveredBy, brokerstore.EndDeleted)
		}
		if err != nil {
			say("the remote took " + c.Ref + ", but the git-broker could not record it in this workspace's " +
				"working set (" + err.Error() + "); `yolo approve` on the host lists the working set.")
		}
	}
}
