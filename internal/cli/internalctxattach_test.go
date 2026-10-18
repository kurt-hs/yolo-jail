package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type ctxAttachCall struct {
	op           string
	pid          int
	source, dest string
	rw           bool
}

func fakeCtxInjector(err error) (*[]ctxAttachCall, ctxInjector) {
	var calls []ctxAttachCall
	return &calls, ctxInjector{
		attach: func(pid int, source, dest string, rw bool) error {
			calls = append(calls, ctxAttachCall{"attach", pid, source, dest, rw})
			return err
		},
		detach: func(pid int, dest string) error {
			calls = append(calls, ctxAttachCall{"detach", pid, "", dest, false})
			return err
		},
	}
}

// TestCtxAttachVerbRunsTheInjector: the verb hands its flags to the injector unchanged, read-only
// unless --rw, and --remove takes the detach.
func TestCtxAttachVerbRunsTheInjector(t *testing.T) {
	cases := []struct {
		args []string
		want ctxAttachCall
	}{
		{[]string{"--pid", "42", "--source", "/src", "--dest", "/ctx/a"}, ctxAttachCall{"attach", 42, "/src", "/ctx/a", false}},
		{[]string{"--pid", "42", "--source", "/src", "--dest", "/ctx/a", "--rw"}, ctxAttachCall{"attach", 42, "/src", "/ctx/a", true}},
		{[]string{"--pid", "7", "--dest", "/ctx/a", "--remove"}, ctxAttachCall{"detach", 7, "", "/ctx/a", false}},
	}
	for _, tc := range cases {
		calls, inj := fakeCtxInjector(nil)
		var stderr bytes.Buffer
		if rc := ctxAttachMain(tc.args, &stderr, inj); rc != 0 {
			t.Errorf("%v: rc %d, stderr %q", tc.args, rc, stderr.String())
		}
		if len(*calls) != 1 || (*calls)[0] != tc.want {
			t.Errorf("%v: calls %+v, want [%+v]", tc.args, *calls, tc.want)
		}
	}
}

// TestCtxAttachVerbRefusesBadArgsWithExit2: a malformed call never reaches the injector.
func TestCtxAttachVerbRefusesBadArgsWithExit2(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--pid", "0", "--source", "/s", "--dest", "/ctx/a"},
		{"--pid", "x", "--source", "/s", "--dest", "/ctx/a"},
		{"--pid", "1", "--dest", "/ctx/a"},                            // attach without a source
		{"--pid", "1", "--source", "/s"},                              // no dest
		{"--pid", "1", "--source", "/s", "--dest", "/ctx/a", "extra"}, // stray operand
		{"--pid", "1", "--source", "/s", "--dest", "/ctx/a", "--remove"},
	} {
		calls, inj := fakeCtxInjector(nil)
		var stderr bytes.Buffer
		if rc := ctxAttachMain(args, &stderr, inj); rc != 2 {
			t.Errorf("%v: rc %d, want 2", args, rc)
		}
		if len(*calls) != 0 {
			t.Errorf("%v: reached the injector: %+v", args, *calls)
		}
	}
}

// TestCtxAttachVerbFailureIsOneLineAndExit1: the host driver relays the verb's stderr as the
// outcome's reason, so a failure is one line naming the step.
func TestCtxAttachVerbFailureIsOneLineAndExit1(t *testing.T) {
	_, inj := fakeCtxInjector(errors.New("move_mount onto /ctx/a: operation not permitted"))
	var stderr bytes.Buffer
	rc := ctxAttachMain([]string{"--pid", "42", "--source", "/src", "--dest", "/ctx/a"}, &stderr, inj)
	if rc != 1 {
		t.Errorf("rc %d, want 1", rc)
	}
	out := stderr.String()
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "move_mount onto /ctx/a: operation not permitted") {
		t.Errorf("stderr %q; want one line carrying the injector's error", out)
	}
}
