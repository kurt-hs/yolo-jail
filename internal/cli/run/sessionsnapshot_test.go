package run

// sessionsnapshot_test.go pins the HOST HALF of the entry snapshot (docs/design/per-session-profiles.md
// §4.3 step 1, SP-D7, SP-D8, SP-D12): deliverChannel writes this entry's channel, complete, into a
// snapshot named by the exec's session id beside the shared files it still writes; a later entry
// never changes an earlier one's snapshot; the jail's teardown removes them; and an attach to a jail
// that cannot read snapshots says so in one line.

import (
	"bytes"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

const (
	snapIDA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	snapIDB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// piOnZai is the shipped claude + pi + zai channel with pi on zai, which writes pi.sh.
func piOnZai(t *testing.T) *packChannel {
	t.Helper()
	o := goldenOptions(t.TempDir(), packHome(t))
	packs := []*packload.Pack{officialPack(t, "claude"), officialPack(t, "pi"), officialPack(t, "zai")}
	o.UseProfiles = map[string]string{"pi": "zai"}
	return channelFor(t, o, bareConfig(), packs, awsAndZaiKeys())
}

// piUnprofiled is the same packs with no selection: another entry's channel.
func piUnprofiled(t *testing.T) *packChannel {
	t.Helper()
	o := goldenOptions(t.TempDir(), packHome(t))
	packs := []*packload.Pack{officialPack(t, "claude"), officialPack(t, "pi"), officialPack(t, "zai")}
	return channelFor(t, o, bareConfig(), packs, awsAndZaiKeys())
}

// sessionSnapshotFiles reads every regular file under dir, keyed by its path relative to dir.
func sessionSnapshotFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			out[rel] = readFileT(t, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSnapshotDeliverChannelWritesTheSessionsSnapshot(t *testing.T) {
	ws := t.TempDir()
	if err := deliverChannel(ws, "podman", snapIDA, piOnZai(t)); err != nil {
		t.Fatalf("deliverChannel: %v", err)
	}
	snap := filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir, snapIDA)
	if st, err := os.Stat(snap); err != nil || st.Mode().Perm() != agentEnvDirMode {
		t.Fatalf("the snapshot directory must exist, mode %04o: %v %v", agentEnvDirMode, st, err)
	}
	// The shared files are still written (H1-H3), and the snapshot holds the same bodies.
	if got, want := readFileT(t, filepath.Join(snap, "yolo-user-env.sh")), readFileT(t, filepath.Join(ws, "yolo-user-env.sh")); got != want {
		t.Errorf("the snapshot's shared file differs from H1:\n%s\n---\n%s", got, want)
	}
	if got, want := readFileT(t, filepath.Join(snap, "pi.sh")), readFileT(t, filepath.Join(ws, agentEnvStateDir, "pi.sh")); got != want {
		t.Errorf("the snapshot's pi.sh differs from H2:\n%s\n---\n%s", got, want)
	}
	if !strings.Contains(readFileT(t, filepath.Join(snap, "pi.sh")), "ZAI_API_KEY") {
		t.Error("the snapshot's pi.sh lacks the profile's key")
	}
	if st, _ := os.Stat(filepath.Join(snap, "pi.sh")); st == nil || st.Mode().Perm() != agentEnvFileMode {
		t.Errorf("the snapshot's pi.sh must be %04o", agentEnvFileMode)
	}
	// Nothing but the published directory is left in sessions/.
	entries, _ := os.ReadDir(filepath.Dir(snap))
	if len(entries) != 1 || entries[0].Name() != snapIDA {
		t.Errorf("sessions/ must hold only the published snapshot, got %v", entries)
	}
}

func TestSnapshotALaterEntryLeavesAnEarlierOneAlone(t *testing.T) {
	ws := t.TempDir()
	if err := deliverChannel(ws, "podman", snapIDA, piOnZai(t)); err != nil {
		t.Fatal(err)
	}
	snapA := filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir, snapIDA)
	before := sessionSnapshotFiles(t, snapA)
	if err := deliverChannel(ws, "podman", snapIDB, piUnprofiled(t)); err != nil {
		t.Fatal(err)
	}
	after := sessionSnapshotFiles(t, snapA)
	if len(before) == 0 || len(before) != len(after) {
		t.Fatalf("the shared dir rewrite must keep sessions/ and A's snapshot whole: before %v, after %v",
			keysOf(before), keysOf(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("a later entry changed A's snapshot file %s", k)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir, snapIDB)); err != nil {
		t.Errorf("B's snapshot was not published: %v", err)
	}
	// And the shared file is B's: last writer, as today.
	if readFileT(t, filepath.Join(ws, "yolo-user-env.sh")) == readFileT(t, filepath.Join(snapA, "yolo-user-env.sh")) {
		t.Error("the shared file must be the last entry's, and it still equals A's snapshot")
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSnapshotTeardownRemovesThem(t *testing.T) {
	for _, rt := range []string{"podman", "container"} {
		t.Run(rt, func(t *testing.T) {
			ws := t.TempDir()
			if err := deliverChannel(ws, rt, snapIDA, piOnZai(t)); err != nil {
				t.Fatal(err)
			}
			dir := agentEnvStateDir
			if rt == "container" {
				dir = entrypoint.AgentEnvDirRel
			}
			sessions := filepath.Join(ws, dir, entrypoint.SessionSnapshotsSubdir)
			if _, err := os.Stat(filepath.Join(sessions, snapIDA)); err != nil {
				t.Fatalf("no snapshot under %s: %v", sessions, err)
			}
			clearAgentEnvFiles(ws, rt)
			if _, err := os.Lstat(sessions); !os.IsNotExist(err) {
				t.Errorf("the jail's teardown must remove its snapshots (SP-D7); %s: %v", sessions, err)
			}
		})
	}
}

// An agent file's pointer in a snapshot's env file names the snapshot's copy, never the shared
// one a later entry rewrites.
func TestSnapshotAgentFileLineNamesTheSnapshotPath(t *testing.T) {
	o := goldenOptions(t.TempDir(), packHome(t))
	packs := []*packload.Pack{officialPack(t, "copilot"), officialPack(t, "zai")}
	o.UseProfiles = map[string]string{"copilot": "zai"}
	channel := channelFor(t, o, bareConfig(), packs, awsAndZaiKeys())
	ws := t.TempDir()
	if err := deliverChannel(ws, "podman", snapIDA, channel); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir, snapIDA)
	if _, err := os.Stat(filepath.Join(snap, "copilot.providers.json")); err != nil {
		t.Fatalf("the snapshot holds no providers.json: %v", err)
	}
	want := "'/home/agent/" + entrypoint.SessionSnapshotDirRel(snapIDA) + "/copilot.providers.json'"
	if env := readFileT(t, filepath.Join(snap, "copilot.sh")); !strings.Contains(env, want) {
		t.Errorf("the snapshot's copilot.sh must point at %s:\n%s", want, env)
	}
	shared := "'/home/agent/" + entrypoint.AgentEnvDirRel + "/copilot.providers.json'"
	if env := readFileT(t, filepath.Join(ws, agentEnvStateDir, "copilot.sh")); !strings.Contains(env, shared) {
		t.Errorf("the shared copilot.sh must still point at the shared file %s:\n%s", shared, env)
	}
}

func TestSnapshotAppleContainerLandsInTheHome(t *testing.T) {
	ws := t.TempDir()
	if err := deliverChannel(ws, "container", snapIDA, piOnZai(t)); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(ws, entrypoint.SessionSnapshotDirRel(snapIDA))
	for _, f := range []string{"yolo-user-env.sh", "pi.sh"} {
		if _, err := os.Stat(filepath.Join(snap, f)); err != nil {
			t.Errorf("Apple Container's jail reads its snapshot at %s, and %s is not there: %v", snap, f, err)
		}
	}
}

// No id (a jail that cannot name its sessions) writes no snapshot and still writes H1-H3.
func TestSnapshotNoSessionIDWritesOnlyTheSharedFiles(t *testing.T) {
	ws := t.TempDir()
	if err := deliverChannel(ws, "podman", "", piOnZai(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir)); !os.IsNotExist(err) {
		t.Errorf("no session id must write no snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, agentEnvStateDir, "pi.sh")); err != nil {
		t.Errorf("the shared pi.sh must still be written: %v", err)
	}
}

// A snapshot that cannot be published is an error the caller refuses the entry on: an id already
// published (snapshots are immutable) stands in for any failed write.
func TestSnapshotAFailedPublishIsAnError(t *testing.T) {
	ws := t.TempDir()
	taken := filepath.Join(ws, agentEnvStateDir, entrypoint.SessionSnapshotsSubdir, snapIDA)
	if err := os.MkdirAll(taken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taken, "pi.sh"), []byte("export X='earlier'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := deliverChannel(ws, "podman", snapIDA, piOnZai(t))
	if err == nil {
		t.Fatal("publishing over an existing snapshot must fail, never replace it")
	}
	if got := readFileT(t, filepath.Join(taken, "pi.sh")); got != "export X='earlier'\n" {
		t.Errorf("the existing snapshot was changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, "yolo-user-env.sh")); !os.IsNotExist(err) {
		t.Errorf("a refused entry must leave the shared files as the previous entry wrote them: %v", err)
	}
	if !strings.Contains(err.Error(), snapIDA) {
		t.Errorf("the error must name the snapshot's path: %v", err)
	}
	// The refusal the callers print: headline, the facts, then what to run.
	var buf bytes.Buffer
	o := goldenOptions(t.TempDir(), t.TempDir())
	o.Stderr = &buf
	o.refuseSnapshotWrite(err)
	msg := buf.String()
	for _, want := range []string{"Refusing", snapIDA, "yolo check"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, msg)
		}
	}
}

// SP-D8: an attach to a jail whose tags lack session-homes says, once, that a running session
// there may read this entry's profile and that a restart removes it. With the tag, nothing.
func TestSessionHomesTagNoteOnAnOlderJail(t *testing.T) {
	cases := []struct {
		name     string
		envLines []string
		want     int
	}{
		{"older jail", []string{entrypoint.ContractTagsEnv + "=entry-channel,agent-env-files,profile-sets,session-hangup"}, 1},
		{"this build's jail", []string{entrypoint.ContractTagsEnv + "=" + launchContractTagsValue()}, 0},
		{"unreadable", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			o := goldenOptions(t.TempDir(), t.TempDir())
			o.Stderr = &buf
			o.noteSessionSnapshotsUnsupported("podman", "yolo-x", c.envLines)
			out := strings.TrimSpace(buf.String())
			lines := 0
			if out != "" {
				lines = len(strings.Split(out, "\n"))
			}
			if lines != c.want {
				t.Fatalf("got %d lines, want %d:\n%s", lines, c.want, out)
			}
			if c.want == 1 && !strings.Contains(out, "yolo stop") {
				t.Errorf("the line must name 'yolo stop': %s", out)
			}
		})
	}
}

func TestSessionHomesTagIsFrozenByEveryLaunch(t *testing.T) {
	found := false
	for _, tag := range launchContractTags {
		found = found || tag == "session-homes"
	}
	if !found {
		t.Errorf("launchContractTags = %v lacks session-homes", launchContractTags)
	}
}

// The attach's call sites, which no unit test drives end to end: the session id is minted before
// the delivery that writes its snapshot and is the one handed to deliverChannelOnAttach, and the
// SP-D8 line is printed. Deleting either fails here.
func TestSnapshotAttachMintsItsIDBeforeDeliveryAndNotesAnOlderJail(t *testing.T) {
	decl := methodDecl(t, "run.go", "attachExisting")
	pos := map[string]token.Pos{}
	var deliverArg ast.Expr
	ast.Inspect(decl, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if _, seen := pos[name]; !seen {
			pos[name] = call.Pos()
		}
		if name == "deliverChannelOnAttach" && len(call.Args) > 2 {
			deliverArg = call.Args[2]
		}
		return true
	})
	for _, name := range []string{"attachSessionID", "deliverChannelOnAttach", "noteSessionSnapshotsUnsupported", "sessionEnvArgs"} {
		if _, ok := pos[name]; !ok {
			t.Fatalf("attachExisting no longer calls %s", name)
		}
	}
	if pos["attachSessionID"] > pos["deliverChannelOnAttach"] {
		t.Error("the attach mints its session id after the delivery that writes the session's snapshot")
	}
	if id, ok := deliverArg.(*ast.Ident); !ok || id.Name != "sessionID" {
		t.Errorf("deliverChannelOnAttach is handed %v, want the session's own id (sessionID)", deliverArg)
	}
}
