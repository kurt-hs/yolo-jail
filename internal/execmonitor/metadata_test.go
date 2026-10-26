//go:build linux

package execmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fixtureCID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const fixtureExec = "exec-session-42"

func TestRecoverPinsAndSignalsOnlyTheMatchingExecMonitor(t *testing.T) {
	f := newFixture(t)
	var signaled int
	run := func(argv []string, _ time.Duration) (string, bool, int) {
		switch {
		case strings.Join(argv, " ") == "podman info --format json":
			return f.info, true, 0
		case len(argv) > 1 && argv[1] == "inspect":
			return f.inspect, true, 0
		default:
			t.Fatalf("unexpected probe argv: %v", argv)
			return "", false, 1
		}
	}
	ops := Ops{
		ProcRoot: f.proc, UID: os.Geteuid(), UIDSet: true, SelfPID: 99,
		PidfdOpen: func(pid int) (int, error) {
			if pid != 100 {
				t.Fatalf("pidfd opened for %d, want exact fake exec monitor 100", pid)
			}
			return 7, nil
		},
		SendSignal: func(fd int, sig syscall.Signal) error {
			if fd != 7 || sig != syscall.SIGKILL {
				t.Fatalf("signal fd=%d sig=%v", fd, sig)
			}
			signaled++
			return nil
		},
		ClosePidfd: func(fd int) error { return nil },
	}
	ok, err := Recover("fixture-name", run, ops)
	if err != nil || !ok || signaled != 1 {
		t.Fatalf("Recover = %v, %v; signal count %d", ok, err, signaled)
	}
}

func TestParseContainerRequiresExactlyFullID(t *testing.T) {
	raw := fmt.Sprintf(`{"Id":%q,"State":{"Status":"stopping"},"ExecIDs":[%q]}`, fixtureCID+"0", fixtureExec)
	if _, err := ParseContainer([]byte(raw)); err == nil {
		t.Fatal("inspect accepted an ID longer than the exact full CID")
	}
}

func TestParseRuntimeInfoDeclinesUntrustedOrUnsupportedMetadata(t *testing.T) {
	base := `{"version":{"Version":"5.8.9"},"host":{"serviceIsRemote":false,"conmon":{"path":"/usr/bin/conmon","version":"2.2.1"}}}`
	for _, tc := range []struct {
		name string
		json string
	}{
		{"remote", strings.Replace(base, `"serviceIsRemote":false`, `"serviceIsRemote":true`, 1)},
		{"missing locality", strings.Replace(base, `"serviceIsRemote":false,`, "", 1)},
		{"unsupported version", strings.Replace(base, "5.8.9", "5.9.0", 1)},
		{"missing conmon", strings.Replace(base, `,"conmon":{"path":"/usr/bin/conmon","version":"2.2.1"}`, "", 1)},
		{"unsupported conmon", strings.Replace(base, `"version":"2.2.1"`, `"version":"2.2.2"`, 1)},
		{"duplicate remote key", strings.Replace(base, `"serviceIsRemote":false`, `"serviceIsRemote":false,"serviceIsRemote":true`, 1)},
		{"bad JSON", base + " garbage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRuntimeInfo([]byte(tc.json)); err == nil {
				t.Fatal("malformed, ambiguous, remote, or unsupported info was accepted")
			}
		})
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	base := t.TempDir()
	proc := filepath.Join(base, "proc")
	bin := filepath.Join(base, "conmon")
	if err := os.WriteFile(bin, []byte("fake conmon executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeProc := func(pid int, state string, ppid int, start uint64, cmdline []byte) {
		dir := filepath.Join(proc, fmt.Sprint(pid))
		for _, path := range []string{dir, filepath.Join(dir, "task", fmt.Sprint(pid))} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), procStat(pid, state, ppid, start), 0o600); err != nil {
			t.Fatal(err)
		}
		uid := os.Geteuid()
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(fmt.Sprintf("Name:\tfake\nUid:\t%d\t%d\t%d\t%d\n", uid, uid, uid, uid)), 0o600); err != nil {
			t.Fatal(err)
		}
		if cmdline != nil {
			if err := os.WriteFile(filepath.Join(dir, "cmdline"), cmdline, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(proc, 0o700); err != nil {
		t.Fatal(err)
	}
	writeProc(100, "S", 1, 1000, []byte("conmon\x00-c\x00"+fixtureCID+"\x00-u\x00"+fixtureExec+"\x00-e\x00"))
	writeProc(200, "Z", 100, 1001, nil)
	if err := os.WriteFile(filepath.Join(proc, "100", "task", "100", "children"), []byte("200"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(proc, "100", "exe")); err != nil {
		t.Fatal(err)
	}
	return fixture{
		proc:    proc,
		info:    fmt.Sprintf(`{"version":{"Version":"5.8.9"},"host":{"serviceIsRemote":false,"conmon":{"path":%q,"version":"2.2.1"}}}`, bin),
		inspect: fmt.Sprintf(`{"Id":%q,"State":{"Status":"stopping"},"ExecIDs":[%q]}`, fixtureCID, fixtureExec),
	}
}

type fixture struct {
	proc, info, inspect string
}

func procStat(pid int, state string, ppid int, start uint64) []byte {
	fields := make([]string, 20)
	fields[0] = state
	fields[1] = fmt.Sprint(ppid)
	for i := 2; i < len(fields); i++ {
		fields[i] = "0"
	}
	fields[19] = fmt.Sprint(start)
	return []byte(fmt.Sprintf("%d (fake comm) %s\n", pid, strings.Join(fields, " ")))
}
