package run

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func resolvedRunTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// ctxArgvInput is a minimal assembly input for rt with one read-only config mount whose source
// exists.
func ctxArgvInput(t *testing.T, rt string) (*Options, *assembleInput, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	emptyLoopholeDirs(t)
	o := goldenOptions("/ws", home)
	src := resolvedRunTemp(t)
	sec := jsonx.NewOrderedMap()
	sec.Set("blocked_tools", []any{})
	return o, &assembleInput{
		cfg:          newConfig("security", sec, "mounts", []any{src + ":/ctx/livesrc"}),
		rt:           rt,
		cname:        "yolo-ws-abcd1234",
		packs:        claudePackFixture(t),
		agentsPath:   "/agents/yolo-ws-abcd1234",
		wsState:      t.TempDir(),
		miseStore:    "/mise-store",
		yoloVersion:  "9.9.9-test",
		mountTargets: map[string]struct{}{},
	}, src
}

// TestPodmanArgvMountsAReadOnlyCtxTmpfs: a podman launch puts /ctx on a tmpfs read-only to the
// jail (LA-D5), and freezes this launch's record token; Apple Container gets neither.
func TestPodmanArgvMountsAReadOnlyCtxTmpfs(t *testing.T) {
	const flag = "type=tmpfs,destination=/ctx,ro=true"
	o, in, _ := ctxArgvInput(t, "podman")
	in.ctxMountsToken = "tok-1"
	got := o.assembleRunCmd(in)
	if i := slices.Index(got, flag); i < 1 || got[i-1] != "--mount" {
		t.Errorf("podman argv lacks --mount %s:\n%v", flag, got)
	}
	if !slices.Contains(got, ctxMountsTokenEnv+"=tok-1") {
		t.Errorf("podman argv lacks the record token:\n%v", got)
	}

	o, in, _ = ctxArgvInput(t, "container")
	in.ctxMountsToken = "tok-1"
	got = o.assembleRunCmd(in)
	for _, a := range got {
		if strings.Contains(a, "destination=/ctx") || strings.HasPrefix(a, ctxMountsTokenEnv+"=") {
			t.Errorf("Apple Container argv carries %q; want neither the tmpfs nor the token", a)
		}
	}
}

// TestRecordLaunchCtxMountsWritesWhatTheArgvBound: the record a fresh podman launch writes is the
// mounts its argv bound, under the token its argv froze; no other backend writes one.
func TestRecordLaunchCtxMountsWritesWhatTheArgvBound(t *testing.T) {
	o, in, src := ctxArgvInput(t, "podman")
	tok, err := o.prepareCtxMountsToken(in)
	if err != nil || tok == "" || in.ctxMountsToken != tok {
		t.Fatalf("prepareCtxMountsToken = %q, %v (input carries %q)", tok, err, in.ctxMountsToken)
	}
	argv := o.assembleRunCmd(in)
	if !slices.Contains(argv, src+":/ctx/livesrc:ro") {
		t.Fatalf("argv does not bind the fixture mount:\n%v", argv)
	}
	if err := o.recordLaunchCtxMounts(in); err != nil {
		t.Fatal(err)
	}
	got, ok := readCtxMountRecord(in.cname, []string{ctxMountsTokenEnv + "=" + tok})
	if want := []ctxMountEntry{{Source: src, Dest: "/ctx/livesrc"}}; !ok || !slices.Equal(got, want) {
		t.Errorf("record %+v, %v; want %+v", got, ok, want)
	}

	o, in, _ = ctxArgvInput(t, "container")
	if tok, _ := o.prepareCtxMountsToken(in); tok != "" {
		t.Errorf("Apple Container minted a token %q", tok)
	}
	o.assembleRunCmd(in)
	if err := o.recordLaunchCtxMounts(in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ctxMountRecordPath(in.cname)); err == nil {
		t.Error("Apple Container wrote a live-mounts record")
	}
}

func ctxWant(source, dest string, rw bool) configCtxMount {
	return configCtxMount{source: source, dest: dest, rw: rw, host: source}
}

// TestDiffCtxMountsCases: diffCtxMounts' four shapes. A destination is the identity of a mount in
// the jail, so a changed source or mode at the same destination is a removal and an addition.
func TestDiffCtxMountsCases(t *testing.T) {
	a := ctxMountEntry{Source: "/h/a", Dest: "/ctx/a"}
	b := ctxMountEntry{Source: "/h/b", Dest: "/ctx/b", RW: true}
	cases := []struct {
		name       string
		have       []ctxMountEntry
		want       []configCtxMount
		add        []string // dests
		remove     []string // dests
		addSources []string
	}{
		{"unchanged", []ctxMountEntry{a, b}, []configCtxMount{ctxWant("/h/a", "/ctx/a", false), ctxWant("/h/b", "/ctx/b", true)}, nil, nil, nil},
		{"add", []ctxMountEntry{a}, []configCtxMount{ctxWant("/h/a", "/ctx/a", false), ctxWant("/h/c", "/ctx/c", false)}, []string{"/ctx/c"}, nil, []string{"/h/c"}},
		{"remove", []ctxMountEntry{a, b}, []configCtxMount{ctxWant("/h/a", "/ctx/a", false)}, nil, []string{"/ctx/b"}, nil},
		{"source change", []ctxMountEntry{a}, []configCtxMount{ctxWant("/h/other", "/ctx/a", false)}, []string{"/ctx/a"}, []string{"/ctx/a"}, []string{"/h/other"}},
		{"mode change", []ctxMountEntry{a}, []configCtxMount{ctxWant("/h/a", "/ctx/a", true)}, []string{"/ctx/a"}, []string{"/ctx/a"}, []string{"/h/a"}},
		{"from nothing", nil, []configCtxMount{ctxWant("/h/a", "/ctx/a", false)}, []string{"/ctx/a"}, nil, []string{"/h/a"}},
		{"to nothing", []ctxMountEntry{a}, nil, nil, []string{"/ctx/a"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, remove := diffCtxMounts(tc.have, tc.want)
			var addDests, addSources, removeDests []string
			for _, m := range add {
				addDests, addSources = append(addDests, m.dest), append(addSources, m.source)
			}
			for _, e := range remove {
				removeDests = append(removeDests, e.Dest)
			}
			if !slices.Equal(addDests, tc.add) || !slices.Equal(addSources, tc.addSources) {
				t.Errorf("add = %v from %v; want %v from %v", addDests, addSources, tc.add, tc.addSources)
			}
			if !slices.Equal(removeDests, tc.remove) {
				t.Errorf("remove = %v; want %v", removeDests, tc.remove)
			}
		})
	}
}

// TestCtxMountRecordRoundTripAndTokenMismatch: the record reads back for the jail whose env
// carries its token, and for no other: a different token, or none, is a jail this record does not
// describe.
func TestCtxMountRecordRoundTripAndTokenMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-ws-abcd1234"
	entries := []ctxMountEntry{{Source: "/h/a", Dest: "/ctx/a"}, {Source: "/h/b", Dest: "/ctx/b", RW: true}}
	if err := writeCtxMountRecord(cname, "tok-1", entries); err != nil {
		t.Fatal(err)
	}
	if p := ctxMountRecordPath(cname); p != filepath.Join(paths.AgentsDir(), cname, "ctx-mounts", "record.json") {
		t.Errorf("record path %s; want under AgentsDir/<cname>/ctx-mounts", p)
	}
	got, ok := readCtxMountRecord(cname, []string{"HOME=/home/agent", ctxMountsTokenEnv + "=tok-1"})
	if !ok || !slices.Equal(got, entries) {
		t.Errorf("read = %+v, %v; want %+v, true", got, ok, entries)
	}
	for name, env := range map[string][]string{
		"other token": {ctxMountsTokenEnv + "=tok-2"},
		"no token":    {"HOME=/home/agent"},
		"empty token": {ctxMountsTokenEnv + "="},
		"no env":      nil,
	} {
		if got, ok := readCtxMountRecord(cname, env); ok {
			t.Errorf("%s: read = %+v, true; want false", name, got)
		}
	}
	// No record at all.
	if _, ok := readCtxMountRecord("yolo-other", []string{ctxMountsTokenEnv + "=tok-1"}); ok {
		t.Error("a jail with no record read as having one")
	}
}

// TestUpdateCtxMountRecordKeepsTheToken: after an apply step the record is rewritten with the
// mounts the jail now has, and still answers to the jail's token.
func TestUpdateCtxMountRecordKeepsTheToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-ws-abcd1234"
	if err := writeCtxMountRecord(cname, "tok-1", []ctxMountEntry{{Source: "/h/a", Dest: "/ctx/a"}}); err != nil {
		t.Fatal(err)
	}
	if err := invalidateCtxMountRecord(cname); err != nil {
		t.Fatal(err)
	}
	now := []ctxMountEntry{{Source: "/h/c", Dest: "/ctx/c"}}
	if err := updateCtxMountRecord(cname, "tok-1", now); err != nil {
		t.Fatal(err)
	}
	got, ok := readCtxMountRecord(cname, []string{ctxMountsTokenEnv + "=tok-1"})
	if !ok || !slices.Equal(got, now) {
		t.Errorf("read after update = %+v, %v; want %+v, true", got, ok, now)
	}
	if err := updateCtxMountRecord("yolo-none", "tok-1", now); err == nil {
		t.Error("updating a record that does not exist succeeded; want an error (no token to keep)")
	}
}

// TestInvalidatedCtxMountRecordCannotClaimStaleMounts: an invalidated record cannot be read for
// the old jail, and its postmutation writer cannot replace a different launch's new token.
func TestInvalidatedCtxMountRecordCannotClaimStaleMounts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-ws-abcd1234"
	if err := writeCtxMountRecord(cname, "tok-1", []ctxMountEntry{{Source: "/h/old", Dest: "/ctx/old"}}); err != nil {
		t.Fatal(err)
	}
	if err := invalidateCtxMountRecord(cname); err != nil {
		t.Fatal(err)
	}
	env := []string{ctxMountsTokenEnv + "=tok-1"}
	if !ctxMountRecordInvalidated(cname, env) {
		t.Fatal("record invalidation did not leave a detectable tombstone")
	}
	if _, ok := readCtxMountRecord(cname, env); ok {
		t.Fatal("stale mount record still answered for the jail's token")
	}
	if err := os.Remove(ctxMountRecordPath(cname)); err != nil {
		t.Fatal(err)
	}
	if !ctxMountRecordInvalidated(cname, env) {
		t.Fatal("a missing record for a current jail was not detected as unknown state")
	}
	if ctxMountRecordInvalidated(cname, nil) {
		t.Fatal("a jail without the live-mount token was treated as an invalidated record")
	}
	// Simulate a launch completing between invalidation and a stale postmutation write.
	if err := writeCtxMountRecord(cname, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := writeCtxMountRecord(cname, "tok-2", nil); err != nil {
		t.Fatal(err)
	}
	if err := updateCtxMountRecord(cname, "tok-1", []ctxMountEntry{{Source: "/h/stale", Dest: "/ctx/a"}}); err == nil {
		t.Fatal("a postmutation write replaced another launch's record")
	}
	if got, ok := readCtxMountRecord(cname, []string{ctxMountsTokenEnv + "=tok-2"}); !ok || len(got) != 0 {
		t.Errorf("the competing launch record was changed: %+v, %v", got, ok)
	}
	if ctxMountRecordInvalidated(cname, []string{ctxMountsTokenEnv + "=tok-2"}) {
		t.Error("the competing launch token was marked invalid")
	}
}

// TestCtxMountRecordIsOwnerOnly: the record names host paths, so it is the owner's alone, like
// the grant beside it.
func TestCtxMountRecordIsOwnerOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := writeCtxMountRecord("yolo-x", "t", nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ctxMountRecordPath("yolo-x"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("record mode %v; want 0600", fi.Mode().Perm())
	}
}

// TestCtxMountEntriesOfIsTheLaunchsList: the record a launch writes is the configCtxMounts list
// it bound, in order.
func TestCtxMountEntriesOfIsTheLaunchsList(t *testing.T) {
	got := ctxMountEntriesOf([]configCtxMount{ctxWant("/h/a", "/ctx/a", false), ctxWant("/h/b", "/ctx/b", true)})
	want := []ctxMountEntry{{Source: "/h/a", Dest: "/ctx/a"}, {Source: "/h/b", Dest: "/ctx/b", RW: true}}
	if !slices.Equal(got, want) {
		t.Errorf("entries %+v; want %+v", got, want)
	}
}
