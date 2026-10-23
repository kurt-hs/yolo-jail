package hostrun

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseHostArgs(t *testing.T) {
	l, err := ParseHostArgs([]string{"-C", "/w", "--name", "m-1", "--", "gh", "pr", "merge", "42"})
	if err != nil || l.Dir != "/w" || l.Label != "m-1" || strings.Join(l.Argv, " ") != "gh pr merge 42" {
		t.Fatalf("%+v %v", l, err)
	}
	l, err = ParseHostArgs([]string{"--", "printf", "--name", "-C"})
	if err != nil || l.Dir != "" || l.Label != "" || strings.Join(l.Argv, " ") != "printf --name -C" {
		t.Fatalf("flags after -- are argv: %+v %v", l, err)
	}
	for _, bad := range [][]string{
		{"--"}, {"gh"}, {"-C", "/a", "-C", "/b", "--", "x"}, {"--name", "Bad", "--", "x"},
		{"--", "yolo", "host-run", "--", "x"}, {"--", "/usr/bin/yolo", "host-run", "x"},
		{"--bogus", "--", "x"}, {"-C"}, {"--name"}, {"-C", "", "--", "x"},
		{"--name", "a", "--name", "b", "--", "x"},
	} {
		if _, err := ParseHostArgs(bad); err == nil {
			t.Errorf("accepted %q", bad)
		} else if !strings.Contains(err.Error(), "yolo host-run") {
			t.Errorf("%q: the usage error names no fix: %v", bad, err)
		}
	}
}

func TestCheckLabel(t *testing.T) {
	for _, ok := range []string{"a", "m-1", "merge-42-q7", "0.x_y", strings.Repeat("a", 40)} {
		if err := CheckLabel(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", "A", "a b", "a/b", ".a", strings.Repeat("a", 41)} {
		if err := CheckLabel(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCheckWordsNamesPositionAndCodePoint(t *testing.T) {
	for _, w := range []string{"a\nb", "a\u202eb", "a\u200bb", "a\u00adb", "a\u2028b", "a\u2029b", "a\xffb"} {
		err := CheckWords([]string{"ok", w})
		var re *RuneError
		if !errors.As(err, &re) || re.Index != 1 {
			t.Fatalf("%q: %v", w, err)
		}
		if strings.Contains(err.Error(), w) {
			t.Errorf("the refusal prints the raw word %q", w)
		}
		if !strings.Contains(err.Error(), "U+") && !strings.Contains(err.Error(), "byte 0x") {
			t.Errorf("%q: the refusal names no code point: %v", w, err)
		}
	}
	if err := CheckWords([]string{"café", `\d+`, "it's", "a b"}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatLineQuotesLeadingEqualsAndPercent(t *testing.T) {
	got := FormatLine(Line{Dir: "/home/you/w", Label: "t1", Argv: []string{"echo", "=x", "%self", "it's"}})
	want := `yolo host-run -C /home/you/w --name t1 -- echo '=x' '%self' 'it'"'"'s'`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := FormatLine(Line{Argv: []string{"true"}}); got != "yolo host-run -- true" {
		t.Fatalf("bare: %s", got)
	}
	if got := FormatLine(Line{Dir: "/a b", Argv: []string{"x=1"}}); got != "yolo host-run -C '/a b' -- x=1" {
		t.Fatalf("spaced dir: %s", got)
	}
}

func TestRunIDRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 23, 14, 33, 7, 0, time.UTC)
	id, err := NewRunID(at, bytes.NewReader(make([]byte, 8)))
	if err != nil || !strings.HasPrefix(id, "20261023T143307Z-") || len(id) != 23 {
		t.Fatalf("%q %v", id, err)
	}
	if got, ok := ParseRunID(id); !ok || !got.Equal(at) {
		t.Fatalf("%v %v", got, ok)
	}
	for _, bad := range []string{".20261023T143307Z-aaaaaa.tmp", "20261023T143307Z-AAAAAA", "x", "../x", "20261023T143307Z-aaaaa1"} {
		if _, ok := ParseRunID(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}
	// A different random source gives a different suffix, from the 32-letter alphabet.
	id2, _ := NewRunID(at, bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff}))
	if id2 == id || !strings.HasSuffix(id2, "-777777") {
		t.Fatalf("%q", id2)
	}
	if _, err := NewRunID(at, bytes.NewReader(nil)); err == nil {
		t.Fatal("a failing random source minted an id")
	}
}

func TestTmpNameRoundTrip(t *testing.T) {
	id := "20261023T143307Z-k4f2qa"
	if got, ok := ParseTmpName(TmpName(id)); !ok || got != id {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := ParseTmpName(".x.tmp"); ok {
		t.Fatal("parsed a non-id tmp name")
	}
}
