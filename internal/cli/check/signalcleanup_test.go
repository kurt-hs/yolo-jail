package check

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/nixchildren"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/reporoot"
)

const signalCleanupChildEnv = "YOLO_CHECK_SIGNAL_CLEANUP_CHILD"

// TestSignalExitRemovesSelectedPackTree runs a child through the real Check call and
// sends SIGTERM only after its BuildImage seam proves sectionPacks staged the selected
// pack. Unlike a fake exit callback, os.Exit in the signal arm skips every Go defer.
func TestSignalExitRemovesSelectedPackTree(t *testing.T) {
	if os.Getenv(signalCleanupChildEnv) == "1" {
		runSignalCleanupChild(t)
		return
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	tmp := filepath.Join(root, "tmp")
	pack := filepath.Join(root, "fixture-pack")
	for _, dir := range []string{filepath.Join(home, ".config", "yolo-jail"), tmp, pack} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(`{"name":"signal-fixture","contributes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{"runtime":"container","packs":[%q]}`, "file://"+pack)
	if err := os.WriteFile(filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		code int
	}{{"SIGINT", syscall.SIGINT, 130}, {"SIGHUP", syscall.SIGHUP, 129}, {"SIGTERM", syscall.SIGTERM, 143}} {
		t.Run(tc.name, func(t *testing.T) { runSignalCleanupCase(t, root, home, tmp, tc.sig, tc.code) })
	}
}

func runSignalCleanupCase(t *testing.T, root, home, tmp string, sig syscall.Signal, code int) {
	t.Helper()
	name := sig.String()
	marker := filepath.Join(root, "staged-"+name)
	terminalMarker := filepath.Join(root, "terminal-fence-"+name)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalExitRemovesSelectedPackTree$", "-test.v")
	cmd.Env = testEnv(map[string]string{
		signalCleanupChildEnv:   "1",
		"HOME":                  home,
		"TMPDIR":                tmp,
		"CHECK_SIGNAL_MARKER":   marker,
		"CHECK_SIGNAL_TERMINAL": terminalMarker,
		"CHECK_SIGNAL_REPO":     root,
	})
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var stagedPaths []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil {
			stagedPaths = strings.Split(strings.TrimSpace(string(b)), "\n")
			break
		}
		select {
		case err := <-waited:
			t.Fatalf("Check child exited before its post-pack image-build block (%v); output:\n%s", err, output.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(stagedPaths) != 3 {
		t.Fatalf("child captured %d paths, want selected, fallback, and process-pack roots: %q", len(stagedPaths), stagedPaths)
	}
	staged, fallbackRoot, processRoot := stagedPaths[0], stagedPaths[1], stagedPaths[2]
	if filepath.Dir(staged) != tmp || !strings.HasPrefix(filepath.Base(staged), "yolo-check-pack-") {
		t.Fatalf("captured staging path %q is not a yolo-check-pack-* tree in TMPDIR %q", staged, tmp)
	}
	for _, tree := range []string{fallbackRoot, processRoot} {
		if !strings.HasPrefix(filepath.Base(tree), packload.EmbeddedFallbackPrefix) {
			t.Fatalf("captured packload tree %q does not use the leased fallback naming", tree)
		}
	}
	if fallbackRoot == processRoot {
		t.Fatalf("embedded fallback and process pack roots unexpectedly alias: %q", fallbackRoot)
	}
	if manifests, err := filepath.Glob(filepath.Join(staged, "*", "pack.json")); err != nil || len(manifests) != 1 {
		t.Fatalf("captured selected tree does not contain one staged synthetic pack: manifests=%q error=%v", manifests, err)
	}
	for _, tree := range []string{staged, fallbackRoot, processRoot} {
		if _, err := os.Stat(tree); err != nil {
			t.Fatalf("process-owned tree %q absent before signal: %v", tree, err)
		}
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != code {
			t.Fatalf("child exit = %v, want status %d; output:\n%s", err, code, output.String())
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatalf("child did not exit after %s; output:\n%s", sig, output.String())
	}
	if b, err := os.ReadFile(terminalMarker); err != nil {
		t.Fatalf("Check signal callback did not reach its terminal-fence probe: %v; output:\n%s", err, output.String())
	} else if got, want := strings.TrimSpace(string(b)), `embedded=0 process-error=true fallback="" process="" nix-stopped=true`; got != want {
		t.Fatalf("post-fence allocation/ordering probe = %q, want %q", got, want)
	}
	for _, tree := range []string{staged, fallbackRoot, processRoot} {
		if _, err := os.Lstat(tree); !os.IsNotExist(err) {
			t.Errorf("%s exit left process-owned tree %q (stat error %v)", sig, tree, err)
		}
	}
}

func runSignalCleanupChild(t *testing.T) {
	cacheBlocker := filepath.Join(os.TempDir(), "embedded-cache-is-a-file")
	if err := os.WriteFile(cacheBlocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	packload.OverrideEmbeddedCacheDir(filepath.Join(cacheBlocker, "embedded-packs"))
	if packs := packload.Embedded(); len(packs) == 0 {
		t.Fatalf("embedded pack fixture unavailable: %v", packload.EmbeddedProblems())
	}
	fallbackRoot, fallback := packload.EmbeddedLocation()
	if !fallback || fallbackRoot == "" {
		t.Fatalf("embedded packs did not use fallback: root=%q fallback=%v", fallbackRoot, fallback)
	}
	if _, err := packload.ProcessPackDir("signal-check"); err != nil {
		t.Fatalf("creating process pack tree: %v", err)
	}
	processRoot := packload.ProcessTreeLocation()
	nixStopped := filepath.Join(os.TempDir(), "check-signal-nix-stopped")
	nixStarted := filepath.Join(os.TempDir(), "check-signal-nix-started")
	_ = os.Remove(nixStopped)
	_ = os.Remove(nixStarted)
	nixCmd := exec.Command("/bin/sh", "-c", `trap 'touch "$1"; exit 0' INT; touch "$2"; while :; do sleep 1; done`, "signal-nix", nixStopped, nixStarted)
	releaseNix, err := nixchildren.Start(nixCmd)
	if err != nil {
		t.Fatalf("starting tracked stand-in nix: %v", err)
	}
	go func() {
		_ = nixCmd.Wait()
		releaseNix()
	}()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(nixStarted); err == nil {
			break
		}
	}
	if _, err := os.Stat(nixStarted); err != nil {
		t.Fatalf("tracked stand-in nix did not start: %v", err)
	}
	var out bytes.Buffer
	opts := baseOptions(t, &out)
	opts.Build = true
	opts.Getenv = func(key string) string {
		if key == "YOLO_RUNTIME" {
			return "container"
		}
		return ""
	}
	opts.LookPath = func(name string) (string, bool) {
		return "/fixture/" + name, name == "container" || name == "nix"
	}
	opts.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		switch strings.Join(argv, " ") {
		case "container --version":
			return ExecResult{Ran: true, Stdout: "container version fixture", RC: 0}
		case "container system status":
			return ExecResult{Ran: true, Stdout: "running", RC: 0}
		case "nix --version":
			return ExecResult{Ran: true, Stdout: "nix (Nix) 2.24.0", RC: 0}
		case "nix store info":
			return ExecResult{Ran: true, Stdout: "Store: /nix/store", RC: 0}
		default:
			return ExecResult{Ran: false}
		}
	}
	opts.PathExists = func(path string) bool {
		return path == filepath.Join(os.Getenv("CHECK_SIGNAL_REPO"), "flake.nix")
	}
	opts.RepoRoot = func() (reporoot.Resolution, bool) {
		return reporoot.Resolution{Root: os.Getenv("CHECK_SIGNAL_REPO")}, true
	}
	opts.signalCleanupProbe = func() {
		packs := packload.Embedded()
		_, processErr := packload.ProcessPackDir("after-signal")
		root, _ := packload.EmbeddedLocation()
		processRoot := packload.ProcessTreeLocation()
		result := fmt.Sprintf(`embedded=%d process-error=%t fallback=%q process=%q nix-stopped=%t`, len(packs), errors.Is(processErr, packload.ErrEmbeddedExiting), root, processRoot, fileExists(nixStopped))
		_ = os.WriteFile(os.Getenv("CHECK_SIGNAL_TERMINAL"), []byte(result), 0o600)
	}
	opts.BuildImage = func(string, []any) (string, []string) {
		trees, err := filepath.Glob(filepath.Join(os.TempDir(), "yolo-check-pack-*"))
		if err != nil || len(trees) != 1 {
			t.Fatalf("post-pack image build saw staging trees %q, glob error %v", trees, err)
		}
		if err := os.WriteFile(os.Getenv("CHECK_SIGNAL_MARKER"),
			[]byte(strings.Join([]string{trees[0], fallbackRoot, processRoot}, "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	rc := Check(opts)
	t.Fatalf("Check returned instead of remaining in the injected image build (status %d):\n%s", rc, out.String())
}

func testEnv(overrides map[string]string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
