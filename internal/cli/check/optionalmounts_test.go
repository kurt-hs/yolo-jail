package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// `yolo check` names an optional `mounts` element this backend will skip, in the launch's own
// line, as information: not a [WARN], not a [FAIL] (docs/reference/context-mounts.md#optional-mounts).
// Through Check(), so deleting the call in sectionMergedConfig fails it.
func TestCheckReportsAnOptionalMountItWillSkipAsInformation(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	out := runCheckOverConfig(t, `{"mounts": [{"host": "`+gone+`", "at": "/ctx/gone", "mode": "ro", "optional": true}]}`, false)
	want := "- skipped optional mount: " + gone + " → /ctx/gone (source does not exist)"
	if !strings.Contains(out, want) {
		t.Errorf("check did not report %q:\n%s", want, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, gone) && (strings.Contains(line, "[WARN]") || strings.Contains(line, "[FAIL]")) {
			t.Errorf("an optional mount was graded: %s", line)
		}
	}
}
