package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

func TestRunRunWiresUnknownYOLOEnvironmentWarning(t *testing.T) {
	t.Setenv("YOLO_ITEM96_UNKNOWN_KEY", "secret-test-value")
	t.Setenv("YOLO_RUNTIME", "podman")
	var seen run.Options
	previous := launchRunPipeline
	launchRunPipeline = func(o run.Options) int { seen = o; return 0 }
	t.Cleanup(func() { launchRunPipeline = previous })

	stderr := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = write
	rc := runRun([]string{"run", "--", "true"})
	if seen.WarnUnknownYOLOEnv == nil {
		os.Stderr = stderr
		_ = write.Close()
		t.Fatal("runRun did not wire the unknown launch environment warning")
	}
	known := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "YOLO_") && name != "YOLO_ITEM96_UNKNOWN_KEY" {
			known = append(known, name)
		}
	}
	seen.WarnUnknownYOLOEnv(known, write)
	os.Stderr = stderr
	_ = write.Close()
	printed, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if rc != 0 {
		t.Fatalf("runRun = %d", rc)
	}
	if !strings.Contains(string(printed), "YOLO_ITEM96_UNKNOWN_KEY") || strings.Contains(string(printed), "secret-test-value") ||
		strings.Count(string(printed), "YOLO_ITEM96_UNKNOWN_KEY") != 2 ||
		!strings.Contains(string(printed), "unset YOLO_ITEM96_UNKNOWN_KEY") || !strings.Contains(string(printed), "YOLO_RUNTIME=podman") {
		t.Fatalf("warning must name the variable once, not its value, and give the runtime remedy:\n%s", printed)
	}
}

func TestNonLaunchCommandsDoNotWarnAboutYOLOEnvironment(t *testing.T) {
	t.Setenv("YOLO_ITEM96_NONLAUNCH", "secret-test-value")
	_, stderr := captureBoth(t, func() {
		if rc := Main([]string{"yolo", "--version"}); rc != 0 {
			t.Errorf("--version exit code = %d", rc)
		}
	})
	if strings.Contains(stderr, "YOLO_ITEM96_NONLAUNCH") || strings.Contains(stderr, "secret-test-value") {
		t.Fatalf("non-launch command emitted unknown-environment warning:\n%s", stderr)
	}
}

func TestWarnUnknownYOLOEnvKeepsRecognizedNamesQuiet(t *testing.T) {
	var out strings.Builder
	known := []string{
		"YOLO_RUNTIME", "YOLO_NO_TMUX", "YOLO_NO_HERDR", "YOLO_JAIL_BACKEND", "YOLO_BLOCK_CONFIG",
		"YOLO_PUBLISHED_PORTS", "YOLO_FORWARD_HOST_PORTS", "YOLO_PROGRAMS_AUTOPRUNE",
	}
	var environ []string
	for _, name := range known {
		environ = append(environ, name+"=secret-test-value")
	}
	warnUnknownYOLOEnv(known, environ, &out)
	if out.Len() != 0 {
		t.Fatalf("recognized launch names emitted warnings:\n%s", out.String())
	}
}

func TestWarnUnknownYOLOEnvSortsDeduplicatesAndOmitsValues(t *testing.T) {
	var out strings.Builder
	warnUnknownYOLOEnv([]string{"YOLO_RUNTIME"}, []string{
		"YOLO_ZED=hidden-zed", "YOLO_RUNTIME=secret-runtime-value", "YOLO_ALPHA=hidden-alpha", "YOLO_ZED=second-value",
	}, &out)
	got := out.String()
	if strings.Count(got, "warning:") != 2 || strings.Index(got, "YOLO_ALPHA") > strings.Index(got, "YOLO_ZED") ||
		strings.Contains(got, "hidden-") || strings.Contains(got, "secret-runtime-value") || strings.Contains(got, "second-value") {
		t.Fatalf("warning output is not sorted, deduplicated and name-only:\n%s", got)
	}
}
