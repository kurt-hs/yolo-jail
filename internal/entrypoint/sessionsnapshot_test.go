package entrypoint

// sessionsnapshot_test.go pins the JAIL HALF of the entry snapshot (docs/design/per-session-profiles.md
// §4.3 steps 3 and 6, SP-D12): a session whose YOLO_SESSION_ID names a snapshot the host wrote
// reads that snapshot's files, in its boot's hydrate, in the launchers it runs and in every new
// interactive shell, and one with no snapshot for its id reads the shared files as before. Each
// shell cell RUNS the generated text under bash.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// snapshotID is a well-formed session id, the shape the host mints (32 lowercase hex).
const snapshotID = "0123456789abcdef0123456789abcdef"

// writeSnapshotFile puts name into session id's snapshot under home, where the host writes it.
func writeSnapshotFile(t *testing.T, home, id, name, body string) {
	t.Helper()
	p := filepath.Join(home, SessionSnapshotDirRel(id), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeSharedUserEnv writes the shared ~/.config/yolo-user-env.sh (H1).
func writeSharedUserEnv(t *testing.T, home, body string) {
	t.Helper()
	p := filepath.Join(home, ".config", "yolo-user-env.sh")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotLayoutIsTheAgentEnvDirsSessionsSubdir(t *testing.T) {
	if got, want := SessionSnapshotDirRel(snapshotID), ".config/yolo-agent-env/sessions/"+snapshotID; got != want {
		t.Errorf("SessionSnapshotDirRel = %q, want %q: the host writes there (SP-D12)", got, want)
	}
}

func TestSnapshotHydrateReadsTheSessionsOwnSnapshot(t *testing.T) {
	home := t.TempDir()
	writeSharedUserEnv(t, home, "export SNAPTEST_SHARED_ONLY='shared'\n")
	writeSnapshotFile(t, home, snapshotID, "yolo-user-env.sh", "export SNAPTEST_SNAP_ONLY='snap'\n")
	t.Cleanup(func() { os.Unsetenv("SNAPTEST_SHARED_ONLY"); os.Unsetenv("SNAPTEST_SNAP_ONLY") })

	e := NewEnv(map[string]string{"JAIL_HOME": home, SessionIDEnv: snapshotID})
	hydrateEnvFromUserEnvFile(e)
	if e.Vars["SNAPTEST_SNAP_ONLY"] != "snap" {
		t.Errorf("a session with a snapshot must hydrate from it; SNAPTEST_SNAP_ONLY = %q", e.Vars["SNAPTEST_SNAP_ONLY"])
	}
	if _, ok := e.Vars["SNAPTEST_SHARED_ONLY"]; ok {
		t.Error("a session with a snapshot read the shared file, which the next attach rewrites (the deadlock)")
	}
	if p := activationPrefix(e); !strings.Contains(p, filepath.Join(home, SessionSnapshotDirRel(snapshotID), "yolo-user-env.sh")) {
		t.Errorf("the session's shell prefix must source the snapshot's file: %q", p)
	}
}

func TestSnapshotHydrateFallsBackToTheSharedFile(t *testing.T) {
	for name, vars := range map[string]map[string]string{
		"no snapshot for the id": {SessionIDEnv: snapshotID},
		"no session id":          {},
		"a malformed id":         {SessionIDEnv: "../../x"},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeSharedUserEnv(t, home, "export SNAPTEST_SHARED_ONLY='shared'\n")
			if name == "a malformed id" {
				writeSnapshotFile(t, home, snapshotID, "yolo-user-env.sh", "export SNAPTEST_SNAP_ONLY='snap'\n")
			}
			t.Cleanup(func() { os.Unsetenv("SNAPTEST_SHARED_ONLY"); os.Unsetenv("SNAPTEST_SNAP_ONLY") })
			vars["JAIL_HOME"] = home
			e := NewEnv(vars)
			hydrateEnvFromUserEnvFile(e)
			if e.Vars["SNAPTEST_SHARED_ONLY"] != "shared" {
				t.Errorf("with no snapshot the shared file is read, as today; got %q", e.Vars["SNAPTEST_SHARED_ONLY"])
			}
			if p := activationPrefix(e); !strings.Contains(p, filepath.Join(home, ".config", "yolo-user-env.sh")) {
				t.Errorf("the shell prefix must source the shared file: %q", p)
			}
		})
	}
}

// The launcher text (agentEnvShellFn, spliced into both installer templates): the snapshot's
// env and provider-keys files when this session has a snapshot, the shared ones otherwise, and
// NEVER the shared file for a session whose snapshot holds none for this agent, since the
// shared one is whichever entry wrote last.
func TestSnapshotLauncherSourcesTheSessionsFiles(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	cases := []struct {
		name     string
		snapEnv  string // "" writes no env file in the snapshot
		snapKeys string
		snapDir  bool
		id       string
		want     string
	}{
		{"the snapshot's files", "export SCOPED='snap'\n", "export OTHER='snapkey'\n", true, snapshotID,
			"SCOPED=snap OTHER=snapkey"},
		{"no snapshot: the shared files", "", "", false, snapshotID, "SCOPED=shared OTHER=sharedkey"},
		{"no session id: the shared files", "", "", true, "", "SCOPED=shared OTHER=sharedkey"},
		{"a snapshot with nothing for this agent", "", "", true, snapshotID, "SCOPED=inherited OTHER="},
	}
	for _, kind := range []string{"npm", "native"} {
		for _, c := range cases {
			t.Run(kind+"/"+c.name, func(t *testing.T) {
				home := t.TempDir()
				logPath := filepath.Join(home, "seen.log")
				launcher := probeLauncher(t, home, kind, logPath, launcherServers{})
				writeAgentEnvFile(t, home, "probe", "export SCOPED='shared'\n")
				writeProviderKeysFile(t, home, "probe", "export OTHER='sharedkey'\n")
				if c.snapDir {
					if err := os.MkdirAll(filepath.Join(home, SessionSnapshotDirRel(snapshotID)), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if c.snapEnv != "" {
					writeSnapshotFile(t, home, snapshotID, "probe.sh", c.snapEnv)
				}
				if c.snapKeys != "" {
					writeSnapshotFile(t, home, snapshotID, filepath.Join(ProviderKeysSubdir, "probe.sh"), c.snapKeys)
				}
				env := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SCOPED=inherited"}
				if c.id != "" {
					env = append(env, SessionIDEnv+"="+c.id)
				}
				out, rc := runScript(t, launcher, nil, env)
				if rc != 0 {
					t.Fatalf("the %s launcher exited %d:\n%s", kind, rc, out)
				}
				if got := strings.Join(logLines(t, logPath), " "); got != c.want {
					t.Errorf("the %s launcher handed its program %q, want %q\n%s", kind, got, c.want, out)
				}
			})
		}
	}
}

// The generated .bashrc sources the session's snapshot of the shared file, so a nested
// interactive shell does not reset the session's tables from whichever entry wrote H1 last.
func TestSnapshotBashrcSourcesTheSessionsSharedFile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	for name, c := range map[string]struct {
		id, want string
	}{
		"with a snapshot":    {snapshotID, "snap"},
		"without a snapshot": {"fedcba9876543210fedcba9876543210", "shared"},
		"no session id":      {"", "shared"},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeSharedUserEnv(t, home, "export SNAPTEST_WHICH='shared'\n")
			writeSnapshotFile(t, home, snapshotID, "yolo-user-env.sh", "export SNAPTEST_WHICH='snap'\n")
			e := NewEnv(map[string]string{"JAIL_HOME": home})
			if err := GenerateBashrc(e); err != nil {
				t.Fatal(err)
			}
			outFile := filepath.Join(home, "which")
			cmd := exec.Command(bash, "--rcfile", e.BashrcPath(), "-i")
			cmd.Env = []string{"HOME=" + home, "TERM=dumb", "PATH=/usr/bin:/bin"}
			if c.id != "" {
				cmd.Env = append(cmd.Env, SessionIDEnv+"="+c.id)
			}
			cmd.Stdin = strings.NewReader("printf %s \"$SNAPTEST_WHICH\" > " + shellSingleQuote(outFile) + "\nexit\n")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("interactive bash over the generated .bashrc: %v\n%s", err, out)
			}
			if got := readFileString(t, outFile); got != c.want {
				t.Errorf("the shell sourced %q, want %q\n%s", got, c.want, out)
			}
		})
	}
}
