package gitbroker

import (
	"encoding/json"
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

// policy.go is what covers a pushed ref (docs/design/git-broker.md §5 and §5.1, GB-D12, GB-D13,
// docs/design/git-broker-plan.md ID-9 and ID-11):
//
//   - a creation (old id all zeros) under refs/heads/ is covered by an approved `create-branch`
//     grant on the repository, or on `*`, while the user's own config still lists the kind;
//   - a creation, update or delete under refs/heads/ is covered by the workspace's working set:
//     a live entry in the store for that workspace, repository and ref, or a branch an approved
//     `branch` grant declares in config while the user's own config still lists the kind;
//   - nothing else is covered (tags, and any branch outside the working set, need approval).
//
// The live scope file's grants and the user config's `grant_kinds` are read at every push, so
// a `yolo config apply` or an edit of the user config on the host reaches the next one. A
// creation under create-branch the upstream then reports `ok` joins the working set in the
// store; a delete of a store entry reported `ok` ends it. A branch declared in config is never
// recorded: the config holds it, so a delete leaves it declared and removing the grant ends it.

const (
	// The grant kinds the git source defines.
	grantCreateBranch = "create-branch"
	grantBranch       = brokerscope.KindBranch
	// The audit sets a covered push falls in (ID-13).
	setCreateBranch = "create-branch"
	setWorkingSet   = "working-set"
	// byCreateBranch is a working-set entry's By when a creation under the grant added it.
	byCreateBranch = "create-branch"
)

// coverer decides whether one ref command is covered, and returns the set it falls in and what
// covered it: the grant (`create-branch`, or `branch:<name>` for a branch declared in config),
// or the store's working-set entry id.
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

// declaredBranch reports whether grants hold a `branch` grant declaring branch on repo. A branch
// grant always names its repository; one naming "*" is never valid (GB-D13) and covers nothing.
func declaredBranch(grants []brokerscope.Grant, repo, branch string) bool {
	for _, g := range grants {
		if g.Kind == grantBranch && g.Branch == branch && g.Repo == repo {
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
	kinds, canonical, authorityErr := config.BrokeredGrantKindsUsingReadablePolicy(Source)
	granted := grantCovers(f.Grants, repo)
	canCreate := authorityErr == nil && granted && slices.Contains(kinds, grantCreateBranch)
	branchAllowed := authorityErr == nil && slices.Contains(kinds, grantBranch)
	kindWarned := map[string]bool{}
	kindMissing := func(kind, what string) {
		if !kindWarned[kind] {
			kindWarned[kind] = true
			switch {
			case authorityErr != nil:
				say("this workspace's " + kind + " grant on " + repo + " is not in effect: the host user config's canonical `allow.git` write authority could not be validated, so the broker covers no such write. Repair `allow.git.always` or `allow.git.on_request` on the host, then push again; the broker rereads that config on each push, or push from the host.")
			case canonical:
				say("this workspace's " + kind + " grant on " + repo + " is not in effect: the host user config's canonical `allow.git` does not authorize it under the selected broker's declared capabilities. Add the required capability to `allow.git.always` or `allow.git.on_request` on the host, then push again; the broker rereads that config on each push, or push from the host.")
			default:
				say("this workspace's " + kind + " grant on " + repo + " is not in effect: your user config does not " +
					"list " + kind + " under `brokered.git.grant_kinds`. Add it there on the host to let the broker " +
					what + " (the next push reads it), or push from the host.")
			}
		}
	}
	// unlisted is what a grant of kind also needs from the host user config before it covers
	// anything, as the hint's clause; nothing when the config already allows the kind.
	unlisted := func(kind string) string {
		switch {
		case authorityErr != nil:
			return ", and repair `allow.git` in your user config on the host, which could not be validated"
		case slices.Contains(kinds, kind):
			return ""
		case canonical:
			return ", and add its capability to `allow.git.always` or `allow.git.on_request` in your user config on the host"
		default:
			return ", and list \"" + kind + "\" under `brokered.git.grant_kinds` in your user config on the host"
		}
	}
	// hint is a refused ref's next step: the one grant that covers it, spelled as the workspace
	// config takes it (docs/reference/happy-path-principle.md).
	hint := func(ref, why string, g brokerscope.Grant) {
		spelled, _ := json.Marshal(g)
		say(ref + ": " + why + ". To let the broker push it, add " + string(spelled) +
			" to `brokered.git.grants` in this workspace's yolo-jail.jsonc" + unlisted(g.Kind) +
			", then run `yolo config apply` on the host, in this workspace, and approve the grant; " +
			"or push it from the host (`git push` in this workspace there).")
	}
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
		branch, isBranch := strings.CutPrefix(c.Ref, "refs/heads/")
		if !isBranch {
			if strings.HasPrefix(c.Ref, "refs/tags/") {
				say(c.Ref + ": the broker never pushes a tag, and no grant changes that; push it from the " +
					"host (`git push origin " + c.Ref + "` in this workspace there).")
			}
			return "", "", false
		}
		declared := declaredBranch(f.Grants, repo, branch)
		// A branch declared in config, its kind allowed, is in the working set: creation,
		// update, force and delete alike (GB-D13).
		inConfig := func() (string, string, bool) {
			if !declared || !branchAllowed {
				return "", "", false
			}
			return setWorkingSet, grantBranch + ":" + branch, true
		}
		// refused is the end of every path that covers nothing: a declared branch whose kind
		// the user config no longer lists says so only here, once the push is refused, so a
		// push another grant covers prints no refusal line.
		refused := func() (string, string, bool) {
			switch {
			case declared && !branchAllowed:
				kindMissing(grantBranch, "push to the branches it declares")
			case c.Create() && granted:
				// kindMissing already named what keeps the create-branch grant from covering it.
			case c.Create():
				hint(c.Ref, "a new branch needs a create-branch grant on "+repo+", which covers any new branch name",
					brokerscope.Grant{Kind: grantCreateBranch, Repo: repo})
			case brokerscope.ValidGrantBranch(branch):
				hint(c.Ref, "not in this workspace's working set: the broker did not create it, and no branch grant declares it",
					brokerscope.Grant{Kind: grantBranch, Repo: repo, Branch: branch})
			default:
				say(c.Ref + ": not in this workspace's working set, and no branch grant can name it; push it from the host " +
					"(`git push` in this workspace there).")
			}
			return "", "", false
		}
		orRefused := func(s, by string, ok bool) (string, string, bool) {
			if ok {
				return s, by, ok
			}
			return refused()
		}
		e, live, err := b.store.LiveWorking(b.bound.Workspace, repo, c.Ref)
		if err != nil {
			storeFailed(err)
			// The store is not needed for a branch the config declares.
			return orRefused(inConfig())
		}
		if c.Create() {
			// The workspace held this branch, and the upstream no longer has it: someone
			// deleted it outside the broker. Its entry ends, and the push is a creation.
			if live {
				if err := b.store.EndWorking(e.ID, brokerstore.EndGone); err != nil {
					storeFailed(err)
					return orRefused(inConfig())
				}
			}
			// Declared in config first: such a branch is never recorded in the store.
			if s, by, ok := inConfig(); ok {
				return s, by, ok
			}
			if canCreate {
				return setCreateBranch, grantCreateBranch, true
			}
			if granted {
				kindMissing(grantCreateBranch, "create branches")
			}
			return refused()
		}
		// A store entry first, so a delete through the broker ends the record it covers.
		if live {
			return setWorkingSet, e.ID, true
		}
		return orRefused(inConfig())
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
