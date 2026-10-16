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

// THE SAFETY PREDICATES WIN OVER RULE ORDER. A link refused first for a delivery reason (a jail
// path outside /ctx, a Data-volume spelling) must still be marked Unsafe when a later safety rule
// holds, because "optional" skips only the delivery reasons. Both cases are the review's
// reproductions on 309018a70.
func TestAnUnsafeSourceIsUnsafeWhicheverRuleRefusesItFirst(t *testing.T) {
	s := DarwinContextSiting()
	for _, l := range []ContextLink{
		{Dest: "/data", Source: "/Users", Optional: true, Dir: true},
		{Dest: "/ctx/staged", Source: "/System/Volumes/Data" + stateDir, Optional: true, Dir: true},
		{Dest: "/ctx/sbx", Source: "/System/Volumes/Data" + SandboxHome() + "/x", Optional: true, Dir: true},
	} {
		got := SiteContextLinks(s, "/Users/Shared/yolo/proj", []ContextLink{l}, nil)
		if len(got) != 1 || !got[0].Unsafe {
			t.Errorf("%s → %s: refusals %+v, want one marked Unsafe", l.Source, l.Dest, got)
		}
	}
}
