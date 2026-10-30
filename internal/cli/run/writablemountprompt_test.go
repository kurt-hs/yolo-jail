package run

// writablemountprompt_test.go pins how the config-change gate shows a writable host mount a
// project's config requests (RC-D24): red with color on, and still marked WRITABLE HOST MOUNT
// with color off (docs/reference/cli-color.md), in the prompt, the no-terminal refusal and the
// --accept-config-changes report alike.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
)

func writableReport() config.ChangeReport {
	return config.ChangeReport{ConfigChanged: true,
		WritableMounts: []string{"/srv/data → /ctx/data"},
		DiffLines: []string{"--- previous", "+++ current", "@@ -1 +1,5 @@",
			`+  "mounts": [`, `+    {"host": "/srv/data", "at": "/ctx/data",`, `+      "write": true`, "+  ]"}}
}

func promptOptions(out *strings.Builder, color bool) *Options {
	return &Options{
		Color:       color,
		IsTTYStderr: func() bool { return true },
		IsTTYStdin:  func() bool { return true },
		Getenv:      func(string) string { return "" },
		Stderr:      out,
		Stdin:       strings.NewReader("n\n"),
	}
}

// redBefore reports whether s shows marker inside a red span: the last escape sequence opened
// before it sets red (31).
func redBefore(s, marker string) bool {
	i := strings.Index(s, marker)
	if i < 0 {
		return false
	}
	esc := strings.LastIndex(s[:i], "\x1b[")
	return esc >= 0 && strings.Contains(s[esc:i], "31")
}

func TestThePromptShowsAWritableMountInRed(t *testing.T) {
	var out strings.Builder
	(&changePrompter{o: promptOptions(&out, true)}).PromptReport(writableReport())
	s := out.String()
	if !redBefore(s, "WRITABLE HOST MOUNT: /srv/data → /ctx/data") {
		t.Fatalf("the writable mount is not shown in red: %q", s)
	}
	if !redBefore(s, `"write": true`) {
		t.Fatalf("the diff's write line is not red: %q", s)
	}
	if strings.Index(s, "WRITABLE HOST MOUNT") > strings.Index(s, "+++ current") {
		t.Fatalf("the writable mount block is not above the diff: %q", s)
	}
}

func TestThePromptMarksAWritableMountWithColorOff(t *testing.T) {
	var out strings.Builder
	(&changePrompter{o: promptOptions(&out, false)}).PromptReport(writableReport())
	s := out.String()
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("color off printed ANSI: %q", s)
	}
	if !strings.Contains(s, "WRITABLE HOST MOUNT: /srv/data → /ctx/data") {
		t.Fatalf("color off lost the writable-mount marker: %q", s)
	}
	if !strings.Contains(s, `"write": true  ← WRITABLE HOST MOUNT`) {
		t.Fatalf("color off lost the diff line's marker: %q", s)
	}
}

// MINOR e: when only a writable element's host changes, the `+` line carrying the new host is
// marked, though its `"write": true` is an unchanged context line below it; a read-only element's
// changed host is not.
func TestAChangedHostOfAWritableMountIsMarked(t *testing.T) {
	var out strings.Builder
	r := config.ChangeReport{ConfigChanged: true, DiffLines: []string{"--- previous", "+++ current", "@@ -1,9 +1,9 @@",
		`   "mounts": [`, `     {`, `       "at": "/ctx/data",`, `-      "host": "/srv/a",`, `+      "host": "/srv/b",`,
		`       "write": true`, `     },`, `     {`, `       "at": "/ctx/ro",`, `-      "host": "/srv/c",`, `+      "host": "/srv/d",`,
		`       "mode": "ro"`, `     }`}}
	(&changePrompter{o: promptOptions(&out, false)}).PromptReport(r)
	s := out.String()
	if !strings.Contains(s, `"host": "/srv/b",  ← WRITABLE HOST MOUNT`) {
		t.Fatalf("the writable element's changed host is not marked:\n%s", s)
	}
	if strings.Contains(s, `"host": "/srv/d",  ←`) {
		t.Fatalf("a read-only element's host was marked writable:\n%s", s)
	}
}

func TestTheRefusalAndTheFlagReportShowAWritableMount(t *testing.T) {
	var out strings.Builder
	o := promptOptions(&out, false)
	r := writableReport()
	o.printChangeRefusal(&config.ChangedNonInteractiveError{ConfigChanged: true, DiffLines: r.DiffLines,
		WritableMounts: r.WritableMounts, WorkspaceConfig: "/w/yolo-jail.toml", SnapshotPath: "/s"})
	if !strings.Contains(out.String(), "WRITABLE HOST MOUNT: /srv/data → /ctx/data") {
		t.Fatalf("the no-terminal refusal does not show the writable mount: %q", out.String())
	}
	out.Reset()
	(&changePrompter{o: o}).ReportAccepted(r)
	s := out.String()
	if !strings.Contains(s, "WRITABLE HOST MOUNT: /srv/data → /ctx/data") || !strings.Contains(s, config.AcceptConfigChangesFlag) {
		t.Fatalf("--accept-config-changes does not show the writable mount it approves: %q", s)
	}
	if strings.Contains(s, "Repository scope changed") {
		t.Fatalf("a config-only flag report claims the repository scope changed: %q", s)
	}
}
