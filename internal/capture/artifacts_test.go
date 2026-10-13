package capture

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProgramArtifactsExcludeSecretsFromActualProducer(t *testing.T) {
	home := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("SYNTHETIC_PROVIDER_TOKEN", "must-not-cross")
	r, err := Run(Options{Home: home, Out: out, Artifacts: []string{".npm-global/bin/probe", ".npm-global/lib/node_modules/@fixture/probe"}, ScanContentRefs: true, Command: []string{"/bin/sh", "-c", `test -z "$SYNTHETIC_PROVIDER_TOKEN" || exit 9
mkdir -p "$HOME/.npm-global/bin" "$HOME/.npm-global/lib/node_modules/@fixture/probe" "$HOME/.local/share/login" "$HOME/.config"
printf program > "$HOME/.npm-global/lib/node_modules/@fixture/probe/main.js"
ln -s ../lib/node_modules/@fixture/probe/main.js "$HOME/.npm-global/bin/probe"
printf login > "$HOME/.local/share/login/token"
printf settings > "$HOME/.config/settings"
printf private > "$HOME/.npm-global/.npmrc"`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Manifest.Entries {
		if strings.Contains(e.Path, "login") || strings.Contains(e.Path, "settings") || strings.Contains(e.Path, "npmrc") {
			t.Fatalf("state captured: %+v", e)
		}
	}
	if _, err := os.Lstat(filepath.Join(r.Tree, ".npm-global/bin/probe")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/share/login/token")); err != nil {
		t.Fatal("producer's excluded output was consumed")
	}
}

func TestProgramArtifactsRefuseEscapeAndPermissionsBeforeOutput(t *testing.T) {
	for _, body := range []string{`ln -s /etc/passwd "$HOME/.local/bin/probe"`, `ln -s ../../.config/token "$HOME/.local/bin/probe"`, `printf program > "$HOME/.local/bin/probe"; chmod 4777 "$HOME/.local/bin/probe"`, `rmdir "$HOME/.local/bin"; ln -s "$HOME/.config" "$HOME/.local/bin"`} {
		t.Run(body, func(t *testing.T) {
			home := t.TempDir()
			out := filepath.Join(t.TempDir(), "out")
			_, err := Run(Options{Home: home, Out: out, Artifacts: []string{".local/bin/probe"}, Command: []string{"/bin/sh", "-c", `mkdir -p "$HOME/.local/bin" "$HOME/.config"; printf secret > "$HOME/.config/token"; ` + body}})
			if err == nil {
				t.Fatal("unsafe output accepted")
			}
			if _, err := os.Stat(ManifestPath(out)); !os.IsNotExist(err) {
				t.Fatal("failed producer wrote manifest")
			}
		})
	}
	for _, root := range []string{".local", ".config", "../escape", ".local/share/yolo-jail", ".npm-global"} {
		home := t.TempDir()
		_, err := Run(Options{Home: home, Out: filepath.Join(t.TempDir(), "out"), Artifacts: []string{root}, Command: []string{"/bin/sh", "-c", "exit 9"}})
		if err == nil || strings.Contains(err.Error(), "installer") {
			t.Fatalf("root %q was not rejected before execution: %v", root, err)
		}
	}
}

func TestProgramArtifactsCancellationProducesNoManifest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "out")
	_, err := Run(Options{Context: ctx, Home: t.TempDir(), Out: out, Artifacts: []string{".local/bin/probe"}, Command: []string{"/bin/sh", "-c", "sleep 30"}})
	if err == nil {
		t.Fatal("cancelled producer succeeded")
	}
	if _, err := os.Stat(ManifestPath(out)); !os.IsNotExist(err) {
		t.Fatal("cancelled producer published manifest")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = Run(Options{Context: ctx, Home: t.TempDir(), Out: filepath.Join(t.TempDir(), "out"), Artifacts: []string{".local/bin/probe"}, Command: []string{"/bin/sh", "-c", "sleep 30 & wait"}})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("child cancellation: %v after %v", err, time.Since(start))
	}
}

func TestLegacyCaptureInstallerRetainsCallerProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps is unavailable")
	}
	res, err := Run(Options{Home: t.TempDir(), Out: filepath.Join(t.TempDir(), "out"), Command: []string{"/bin/sh", "-c", `mkdir -p "$HOME/.local/bin"; ps -o pgid= -p $$ > "$HOME/.local/bin/probe"`}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(res.Tree, ".local/bin/probe"))
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if pgid != syscall.Getpgrp() {
		t.Fatalf("legacy installer process group %d differs from caller %d", pgid, syscall.Getpgrp())
	}
}
