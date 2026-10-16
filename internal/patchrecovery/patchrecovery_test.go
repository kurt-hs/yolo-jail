package patchrecovery

import (
	"reflect"
	"strings"
	"testing"
)

var piFork = Subject{Key: "pi-fork/pi", Pack: "pi-fork", Name: "pi"}
var ext = Subject{Key: "matt/pi-subagents", Pack: "matt", Name: "pi-subagents"}

func TestParseGrammar(t *testing.T) {
	cases := []struct {
		value     string
		set, all  bool
		names     []string
		covers    []Subject
		uncovered []Subject
	}{
		{value: "", set: false},
		{value: "  ", set: false},
		{value: "1", set: true, all: true, covers: []Subject{piFork, ext}},
		{value: "pi", set: true, names: []string{"pi"}, covers: []Subject{piFork}, uncovered: []Subject{ext}},
		{value: "pi-fork", set: true, names: []string{"pi-fork"}, covers: []Subject{piFork}, uncovered: []Subject{ext}},
		{value: "matt/pi-subagents, pi-fork/pi,,pi-fork/pi", set: true,
			names: []string{"matt/pi-subagents", "pi-fork/pi"}, covers: []Subject{piFork, ext}},
		{value: "0", set: true, names: []string{"0"}, uncovered: []Subject{piFork, ext}},
	}
	for _, c := range cases {
		s := Parse(c.value)
		if s.Set() != c.set || s.All() != c.all || !reflect.DeepEqual(s.Names(), c.names) {
			t.Errorf("Parse(%q) = set %v all %v names %v; want %v %v %v", c.value, s.Set(), s.All(), s.Names(), c.set, c.all, c.names)
		}
		for _, sub := range c.covers {
			if !s.Covers(sub) {
				t.Errorf("Parse(%q) does not cover %s", c.value, sub.Key)
			}
		}
		for _, sub := range c.uncovered {
			if s.Covers(sub) {
				t.Errorf("Parse(%q) covers %s", c.value, sub.Key)
			}
		}
	}
}

func TestReadNamesItsVariable(t *testing.T) {
	s := Read(func(k string) string {
		if k == SkipPatchesEnv {
			return "pi"
		}
		return ""
	}, SkipPatchesEnv)
	if s.Env != SkipPatchesEnv || !s.Covers(piFork) {
		t.Fatalf("Read = %+v", s)
	}
}

func TestUnmatchedIsAWarningNamingWhatIsHere(t *testing.T) {
	s := Parse("pi,typo")
	got := s.Unmatched([]Subject{piFork, ext})
	if !reflect.DeepEqual(got, []string{"typo"}) {
		t.Fatalf("Unmatched = %v", got)
	}
	line := UnmatchedLine(SkipPatchesEnv, got, []Subject{piFork, ext})
	want := "Warning: YOLO_SKIP_PATCHES names typo, which matches no patched build here (patched builds: matt/pi-subagents, pi-fork/pi)"
	if line != want {
		t.Fatalf("UnmatchedLine =\n%s\nwant\n%s", line, want)
	}
	if Parse("1").Unmatched(nil) != nil || UnmatchedLine(SkipPatchesEnv, nil, nil) != "" {
		t.Fatal("1 and nothing unmatched must say nothing")
	}
}

// TestSectionGolden pins the hatch list's shape (ruled 2026-10-16): a blank line, the heading, one
// copyable command per line in one column, a few words of label after it.
func TestSectionGolden(t *testing.T) {
	hatches := []Hatch{
		{Env: AllowPatchFailuresEnv, Value: "pi-fork/pi", Command: "yolo", Label: "run v1.1.0 + 2 patches"},
		{Env: AllowOlderUpstreamEnv, Value: "pi-fork/pi", Command: "yolo", Label: "build on an older release"},
		{Env: AllowMissingProgramsEnv, Value: "1", Label: "start without it"},
	}
	want := strings.Join([]string{
		"",
		"Start anyway:",
		"  YOLO_ALLOW_PATCH_FAILURES=pi-fork/pi yolo    run v1.1.0 + 2 patches",
		"  YOLO_ALLOW_OLDER_UPSTREAM=pi-fork/pi yolo    build on an older release",
		"  YOLO_ALLOW_MISSING_PROGRAMS=1 yolo           start without it",
		"",
	}, "\n")
	if got := Section(StartAnyway, hatches, false); got != want {
		t.Fatalf("Section =\n%s\nwant\n%s", got, want)
	}
	colored := Section(StartAnyway, hatches, true)
	if !strings.Contains(colored, "\x1b[1m\x1b[36mYOLO_ALLOW_PATCH_FAILURES=pi-fork/pi yolo\x1b[0m") {
		t.Fatalf("color does not style the command:\n%q", colored)
	}
	if Section(StartAnyway, nil, true) != "" {
		t.Fatal("no hatches is no section")
	}
}
