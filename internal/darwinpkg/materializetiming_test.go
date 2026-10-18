package darwinpkg

// materializetiming_test.go pins Materialize's two timing spans (docs/reference/perf-logging.md,
// macos_user.materialize.*) through Materialize itself, against a fake `nix` on PATH, on success
// and on a failed build.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

func fakeNix(t *testing.T, buildExit string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n*eval*) echo '[]' ;;\n*) echo /nix/store/aaaa-profile; exit " +
		buildExit + " ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
}

func TestMaterializeSpansItsEvalAndItsBuild(t *testing.T) {
	for _, exit := range []string{"0", "1"} {
		fakeNix(t, exit)
		var events []perf.Event
		log := perf.New(nil, func(e perf.Event) { events = append(events, e) })
		var stderr bytes.Buffer
		_, err := Materialize(t.TempDir(), nil, "aarch64-darwin", &stderr, log)
		if (err == nil) != (exit == "0") {
			t.Fatalf("Materialize err = %v with a build exiting %s", err, exit)
		}
		ends := map[string]int{}
		for _, e := range events {
			if e.Kind == perf.KindEnd {
				ends[e.Name]++
			}
		}
		for _, name := range []string{"macos_user.materialize.skip_eval", "macos_user.materialize.build"} {
			if ends[name] != 1 {
				t.Errorf("build exit %s: %s ended %d times, want once (%v)", exit, name, ends[name], ends)
			}
		}
	}
}
