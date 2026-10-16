package entrypoint

// updaterefusal_helpers_test.go holds the fakes the launchers' no-silent-fallback cells share
// (docs/design/program-delivery.md PD-NF1..PD-NF6): a stand-in npm registry whose reachability a
// cell decides, and installer hosts that answer or do not. Nothing here reaches a network.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// registryDownEnv makes fakeRegistryDir's npm say the registry cannot be reached.
const registryDownEnv = "FAKE_REGISTRY_DOWN=1"

// fakeRegistryDir writes a stand-in `npm` whose `ping` — the launchers' registry probe
// (_yolo_registry_reachable) — answers, or fails under FAKE_REGISTRY_DOWN, and returns the
// directory to put first on PATH. Every other npm call exits 0 having done nothing, so a cell that
// prepends it reaches no registry, real or not.
func fakeRegistryDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fakeregistry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	npm := `#!/bin/bash
if [ "${1:-}" = ping ] && [ -n "${FAKE_REGISTRY_DOWN:-}" ]; then
    echo "npm error code ENOTFOUND" >&2
    exit 1
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(npm), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// answeringHost is an installer host that answers every request, for an update whose upstream is
// reachable; its URL is returned.
func answeringHost(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/bash\nexit 0\n"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/install.sh"
}

// exitCode is a command's exit status from its Run error: 0 for nil, -1 when it did not exit.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// silentHost is the URL of an installer host that is gone: a server started and closed, so a
// connection is refused at once, the way an offline jail's is.
func silentHost(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/install.sh"
	srv.Close()
	return url
}
