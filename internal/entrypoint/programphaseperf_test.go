package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func programPerfFile(t *testing.T, home string) string {
	t.Helper()
	p := filepath.Join(home, ".yolo-perf.log")
	writeTestFile(t, p, bootPerfRunPrefix+" program phase fixture\n")
	return p
}

func requireProgramPhase(t *testing.T, p, phase, bin, mode, rc string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"program phase: " + phase + " " + bin + " start", "program phase: " + phase + " " + bin + " end", "mode=" + mode, "coarse elapsed seconds=", "exit=" + rc} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q in actual phase sink: %s", want, b)
		}
	}
}

func TestReadinessPhasePerfThroughBootstrap(t *testing.T) {
	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[offline], func(t *testing.T) {
			h := newReadyHome(t, map[string]string{readyPack: readyManifest}, nil)
			p := programPerfFile(t, h.e.Home)
			var env []string
			wantRC := "0"
			if offline {
				env = []string{"FAKE_NPM_OFFLINE=1"}
				wantRC = "1"
			}
			rc, out, _ := h.run(t, env...)
			if offline && rc == 0 || !offline && rc != 0 {
				t.Fatalf("wrong readiness result %d: %s", rc, out)
			}
			requireProgramPhase(t, p, "readiness", readyBin, "foreground", wantRC)
			if !offline {
				_, out, _ = h.run(t)
				if strings.Contains(out, readyBin) {
					t.Fatal("warm readiness became noisy")
				}
			}
		})
	}
}

func TestProgramRefreshPhasePerfAtForegroundAndDetachedCalls(t *testing.T) {
	for _, mode := range []string{"foreground", "detached"} {
		for _, rc := range []string{"0", "3"} {
			t.Run(mode+rc, func(t *testing.T) {
				p := newPrelaunchProbe(t, false)
				if mode == "detached" {
					p = newTimingProbe(t, false, false)
				}
				sink := programPerfFile(t, p.home)
				stdout, _ := p.run(t, "", "FAKE_REFRESH_RC="+rc)
				if mode == "detached" {
					waitForJobs(t, p, 1)
				}
				if !strings.Contains(stdout, "RAN") {
					t.Fatal("refresh changed launch behavior")
				}
				requireProgramPhase(t, sink, "program_refresh", "tool", mode, rc)
			})
		}
	}
}

func TestProgramPhasePerfMissingOrUnusableSinkDoesNotChangeCommands(t *testing.T) {
	for _, unusable := range []bool{false, true} {
		h := newReadyHome(t, map[string]string{readyPack: readyManifest}, nil)
		p := filepath.Join(h.e.Home, ".yolo-perf.log")
		if unusable {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if rc, out, _ := h.run(t); rc != 0 {
			t.Fatalf("sink failure broke readiness: %d %s", rc, out)
		}
		probe := newPrelaunchProbe(t, false)
		p2 := filepath.Join(probe.home, ".yolo-perf.log")
		if unusable {
			if err := os.Mkdir(p2, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		stdout, stderr := probe.run(t, "")
		if !strings.Contains(stdout, "RAN") || strings.Contains(stderr, "program phase:") {
			t.Fatal("sink failure affected launch streams")
		}
		if !unusable {
			for _, f := range []string{p, p2} {
				if _, err := os.Stat(f); !os.IsNotExist(err) {
					t.Fatal("missing sink was recreated")
				}
			}
		}
	}
}

func TestProgramRefreshPerfDoesNotClaimFrozenWork(t *testing.T) {
	p := newPrelaunchProbe(t, false)
	p.updates = false
	sink := programPerfFile(t, p.home)
	p.run(t, "")
	b, _ := os.ReadFile(sink)
	if strings.Contains(string(b), "program phase:") {
		t.Fatalf("frozen refresh claimed work: %s", b)
	}
}

func TestProgramPhasePerfAppendFailureIsSilent(t *testing.T) {
	// /proc/version is a regular file that cannot be appended even by root.
	// A missing sink is separately covered on every platform.
	if info, err := os.Stat("/proc/version"); err != nil || !info.Mode().IsRegular() {
		t.Skip("no read-only regular /proc sink")
	}
	h := newReadyHome(t, map[string]string{readyPack: readyManifest}, nil)
	if err := os.Symlink("/proc/version", filepath.Join(h.e.Home, ".yolo-perf.log")); err != nil {
		t.Fatal(err)
	}
	if rc, out, _ := h.run(t); rc != 0 || strings.Contains(out, "program phase:") || strings.Contains(out, "Permission denied") {
		t.Fatalf("append failure changed readiness: %d %s", rc, out)
	}
	p := newPrelaunchProbe(t, false)
	if err := os.Symlink("/proc/version", filepath.Join(p.home, ".yolo-perf.log")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := p.run(t, "", "FAKE_REFRESH_RC=3")
	if !strings.Contains(stdout, "RAN") || !strings.Contains(stderr, "status 3") || strings.Contains(stderr, ".yolo-perf.log") {
		t.Fatalf("append failure changed refresh streams: %s %s", stdout, stderr)
	}
}
