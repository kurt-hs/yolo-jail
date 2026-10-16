package macosuser

// optionalmount_test.go pins the DAC preflight's half of optional context mounts
// (docs/reference/context-mounts.md#optional-mounts) through RunMacosUser, the production caller:
// an optional link the sandbox account cannot reach is skipped with one line and never staged,
// while a required one still refuses the launch.

import (
	"bytes"
	"strings"
	"testing"
)

// unreachable fails the sandbox account's /bin/test probe of src and records every argv.
func unreachable(src string, rec *[]string) func([]string) int {
	return func(argv []string) int {
		joined := strings.Join(argv, " ")
		*rec = append(*rec, joined)
		if strings.Contains(joined, testBin) && strings.Contains(joined, src) {
			return 1
		}
		return 0
	}
}

func TestAnOptionalContextMountTheSandboxCannotReachIsSkipped(t *testing.T) {
	var rec []string
	d := mockDeps(nil)
	var buf bytes.Buffer
	d.Out = &buf
	d.Run = unreachable("/Users/Shared/yolo/locked", &rec)
	opts := newOpts("/Users/Shared/yolo/proj")
	opts.HostCtx.Links = []ContextLink{
		{Dest: "/ctx/locked", Source: "/Users/Shared/yolo/locked", Named: "/Users/Shared/yolo/locked",
			Dir: true, Optional: true},
		{Dest: "/ctx/open", Source: "/Users/Shared/yolo/open", Dir: true},
	}
	RunMacosUser(d, opts)
	out := buf.String()
	if strings.Contains(out, "cannot reach every context mount") {
		t.Fatalf("an optional unreachable mount refused the launch:\n%s", out)
	}
	want := "skipped optional mount: /Users/Shared/yolo/locked → /ctx/locked (" + SandboxUser + " cannot read it)"
	if !strings.Contains(out, want) {
		t.Errorf("no skip line %q:\n%s", want, out)
	}
	stagedOpen := false
	for _, argv := range rec {
		if strings.Contains(argv, testBin) {
			continue // the preflight's own probes
		}
		if strings.Contains(argv, "/Users/Shared/yolo/locked") {
			t.Errorf("the skipped mount was still staged: %s", argv)
		}
		if strings.Contains(argv, "/Users/Shared/yolo/open") {
			stagedOpen = true
		}
	}
	if !stagedOpen {
		t.Errorf("the required, reachable mount was never staged:\n%s", strings.Join(rec, "\n"))
	}
}

// The control: the same unreachable source, required, still refuses.
func TestARequiredContextMountTheSandboxCannotReachStillRefuses(t *testing.T) {
	var rec []string
	d := mockDeps(nil)
	var buf bytes.Buffer
	d.Out = &buf
	d.Run = unreachable("/Users/Shared/yolo/locked", &rec)
	opts := newOpts("/Users/Shared/yolo/proj")
	opts.HostCtx.Links = []ContextLink{{Dest: "/ctx/locked", Source: "/Users/Shared/yolo/locked", Dir: true}}
	if rc := RunMacosUser(d, opts); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(buf.String(), "cannot reach every context mount") {
		t.Errorf("no refusal:\n%s", buf.String())
	}
}
