package config

import (
	"slices"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// gateread.go is the config-change gate's ONE read of the workspace config
// (docs/design/workspace-widening.md §3.2, WW-D18): strict, with its provenance, made once per
// gate and feeding both halves of the approval record. The config part is that read with
// `brokered` projected out (approvalConfigPart), and the scope part's entry is that read's
// `brokered.<source>.repos` (WorkspaceRead.Entry).
//
// STRICT, BECAUSE A NON-STRICT READ NEVER FAILS. The gate used to read non-strict with a warning
// callback that discarded every message, and in that mode the loader returns `{}` and no error
// for an unparseable file, an unreadable one and a bad include alike. So a file broken between
// the launch's own strict read and the gate reached the gate as an empty config, and its entry
// read as empty. Here any problem refuses, naming the file.
//
// ONE HELPER FOR BOTH CALLERS. A launch and host `yolo check --accept-config-changes` read the
// scope through ReadWorkspaceForGate and NewScopeCheck alike, since a preflight with its own copy
// of the gate is how the two drift.

// WorkspaceRead is one gate's read of a workspace's config.
type WorkspaceRead struct {
	// Config is the workspace config as read, `brokered` included: the projection to the config
	// part is the approval code's (approvalConfigPart), never the reader's.
	Config  *jsonx.OrderedMap
	sources *Sources
}

// ReadWorkspaceForGate reads the workspace config for the gate: strictly, with its provenance,
// and refusing a `brokered` value any file outside the workspace wrote (WW-D17), so the entry
// the gate shows is only ever this workspace's own. Its error names the file.
func ReadWorkspaceForGate(workspace string) (*WorkspaceRead, error) {
	cfg, src, err := LoadWorkspaceConfigWithSources(workspaceOrCwd(workspace), true, func(string) {})
	if err != nil {
		return nil, err
	}
	if probs := brokeredOutsideProblems(src.node().key(brokeredKey)); len(probs) > 0 {
		return nil, configErr("%s", probs[0])
	}
	if cfg == nil {
		cfg = jsonx.NewOrderedMap()
	}
	return &WorkspaceRead{Config: cfg, sources: src}, nil
}

// EntryRepo is one repository a workspace's `brokered.<source>.repos` lists.
type EntryRepo struct {
	// Repo is its first spelling in merge order.
	Repo string
	// Files are the workspace-relative files that list it, in merge order (the config file, its
	// includes, the local file, its includes), as they were read: escape them to print them.
	Files []string
}

// Entry is what this read's `brokered.<source>.repos` lists: each repository once, compared
// without case, with the files that list it. A malformed element, which validation refuses, is
// skipped, so a refused one reaches no scope even where nothing validated.
func (r *WorkspaceRead) Entry(source string) []EntryRepo {
	if r == nil || r.Config == nil {
		return nil
	}
	sv, _ := r.Config.Get(brokeredKey)
	sources, ok := asMap(sv)
	if !ok {
		return nil
	}
	srcV, _ := sources.Get(source)
	src, ok := asMap(srcV)
	if !ok {
		return nil
	}
	rv, _ := src.Get(brokeredReposKey)
	list, _ := rv.([]any)
	node := r.sources.node().key(brokeredKey).key(source).key(brokeredReposKey)
	index := map[string]int{}
	var out []EntryRepo
	var files [][]*srcFile
	for i, e := range list {
		repo, ok := e.(string)
		if !ok || !brokerscope.ValidPattern(repo) {
			continue
		}
		k := strings.ToLower(repo)
		at, seen := index[k]
		if !seen {
			at = len(out)
			index[k] = at
			out = append(out, EntryRepo{Repo: repo})
			files = append(files, nil)
		}
		for _, w := range node.elem(i).writerList() {
			if w.file != nil && w.file.contained {
				files[at] = append(files[at], w.file)
			}
		}
	}
	// Each repository's files in merge order, the order the load read them in: the list's own
	// order is not it, since a case variant in an include is an element of its own after the
	// one a later file repeats exactly.
	for i := range out {
		sort.SliceStable(files[i], func(a, b int) bool { return files[i][a].seq < files[i][b].seq })
		for _, f := range files[i] {
			if !inList(out[i].Files, f.rel) {
				out[i].Files = append(out[i].Files, f.rel)
			}
		}
	}
	return out
}

// Grants is what this read's `brokered.<source>.grants` names that can join the scope (BB-D96,
// docs/design/git-broker-plan.md ID-10): each grant once, in the order written, of a kind in
// defined (the source's manifest) that the user's own config allows (BrokeredGrantKinds), and
// with the workspace-relative files that name any of them, in merge order. An entry validation
// refuses or warns about (a malformed one, `read`, a kind outside either list) is skipped, so it
// reaches no scope even where nothing validated.
func (r *WorkspaceRead) Grants(source string, defined []string) (grants []brokerscope.Grant, files []string) {
	if r == nil || r.Config == nil {
		return nil, nil
	}
	defined = definedGrantKinds(defined)
	sv, _ := r.Config.Get(brokeredKey)
	sources, ok := asMap(sv)
	if !ok {
		return nil, nil
	}
	srcV, _ := sources.Get(source)
	src, ok := asMap(srcV)
	if !ok {
		return nil, nil
	}
	gv, _ := src.Get(brokeredGrantsKey)
	list, _ := gv.([]any)
	allowed := allowedGrantKinds(source)
	node := r.sources.node().key(brokeredKey).key(source).key(brokeredGrantsKey)
	var fileList []*srcFile
	for i, e := range list {
		m, ok := asMap(e)
		if !ok {
			continue
		}
		kv, _ := m.Get(brokeredGrantKindKey)
		rv, _ := m.Get(brokeredGrantRepoKey)
		bv, hasBranch := m.Get(brokeredGrantBranchKey)
		kind, _ := kv.(string)
		repo, _ := rv.(string)
		branch, branchOK := bv.(string)
		keys := 2
		if hasBranch {
			keys = 3
		}
		// A branch is part of the grant only for a kind that names one (ValidGrant refuses it on
		// any other, and refuses a branch grant without one).
		g := brokerscope.Grant{Kind: kind, Repo: repo, Branch: branch}
		if len(m.Keys()) != keys || (hasBranch && !branchOK) || !brokerscope.ValidGrant(g) ||
			!inList(defined, kind) || !inList(allowed, kind) {
			continue
		}
		dup := false
		for _, have := range grants {
			dup = dup || have == g
		}
		if !dup {
			grants = append(grants, g)
		}
		for _, w := range node.elem(i).writerList() {
			if w.file != nil && w.file.contained {
				fileList = append(fileList, w.file)
			}
		}
	}
	sort.SliceStable(fileList, func(a, b int) bool { return fileList[a].seq < fileList[b].seq })
	for _, f := range fileList {
		if !inList(files, f.rel) {
			files = append(files, f.rel)
		}
	}
	return grants, files
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ScopeBroker is one brokered loophole a gate asks about: its source, its name, and the forge
// whose remotes make up its scope (loopholedecl.Brokered).
type ScopeBroker struct {
	Source, Label, RemoteHost string
	// GrantKinds are the grant kinds the source defines (loopholedecl.Brokered.Grants): a
	// workspace grant of any other kind reaches no scope.
	GrantKinds []string
}

// NewScopeCheck is the scope part in play for brokers: each one's remotes, read from the
// workspace's git config as text (brokerscope.ReadRemotes), and its entry, from the gate's own
// read; for a broker on any host (`remote_host: "*"`), every remote URL (brokerscope.ReadAllRemotes)
// in the remote-url form, and no entry. Each source also carries the grants the same read names
// (WorkspaceRead.Grants). nil when no broker starts, which leaves the gate exactly what it was before the scope
// part existed.
//
// A SOURCE WITH NO READ GRANT HOLDS NOTHING (BB-D110): its remotes, its entry and its other
// grants are dropped from what the gate reads, since its broker refuses every call anyway. One
// whose last approved record held no read grant either is Dormant, out of the gate entirely; one
// whose record held one shows that grant and its scope removed, once.
func NewScopeCheck(workspace string, brokers []ScopeBroker, read *WorkspaceRead) *ScopeCheck {
	if len(brokers) == 0 {
		return nil
	}
	rec, _, _ := readScopeRecord(ApprovalScopePath(workspace))
	check := &ScopeCheck{}
	for _, b := range brokers {
		s := ScopeSource{Source: b.Source, Label: b.Label, RemoteHost: b.RemoteHost,
			Form: brokerscope.FormFor(b.RemoteHost)}
		if s.Form == brokerscope.FormRemoteURL {
			// A source on any host: every remote URL, verbatim, and no entry, which validation
			// refuses for it (validateBrokeredWorkspace); a remote joins by `git remote add`.
			s.Read = brokerscope.ReadAllRemotes(workspace)
		} else {
			s.Read, s.Entry = brokerscope.ReadRemotes(workspace, b.RemoteHost), read.Entry(b.Source)
		}
		// The user config's own grants first (BB-D107), then the workspace's, each once: both
		// join only at this gate, and the user's are marked as theirs on the block's line.
		s.UserGrants = BrokeredUserGrants(b.Source)
		ws, files := read.Grants(b.Source, b.GrantKinds)
		s.Grants, s.GrantFiles = append([]brokerscope.Grant(nil), s.UserGrants...), files
		for _, g := range ws {
			if !slices.Contains(s.Grants, g) {
				s.Grants = append(s.Grants, g)
			}
		}
		if !brokerscope.HoldsRead(s.Grants) {
			s.Read.Remotes, s.Entry, s.Grants, s.GrantFiles, s.UserGrants = nil, nil, nil, nil, nil
			s.Dormant = !recordHeldRead(rec, b.Source)
		}
		check.Sources = append(check.Sources, s)
	}
	return check
}

// recordHeldRead reports whether an approval record holds a read grant for source: whether the
// last approval let its broker do anything at all (BB-D110).
func recordHeldRead(rec scopeRecord, source string) bool {
	for _, e := range rec[grantsRecordKey(source)] {
		if g, ok := brokerscope.ParseGrant(e); ok && g.Kind == brokerscope.KindRead {
			return true
		}
	}
	return false
}
