package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPreparationSpawnEnvironmentDiscoversSelectedProviderNames(t *testing.T) {
	pf := newPoolFixture(t)
	path := filepath.Join(pf.packs, "treepool", "pack.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, strings.Replace(string(b), `"contributes":[`, `"contributes":[{"kind":"provider","name":"synthetic-provider","api_key_env_name":"HTTP_PROXY"},`, 1))
	t.Setenv("HTTP_PROXY", "synthetic-only-secret")
	if !slices.Contains(preparationProviderEnvNames(), "HTTP_PROXY") {
		t.Fatal("selected synthetic provider credential name was not discovered")
	}
	cmd := preparationSchedulerProcess()
	for _, value := range cmd.Env {
		if strings.HasPrefix(value, "HTTP_PROXY=") {
			t.Fatalf("production spawned cmd.Env included discovered provider credential: %q", value)
		}
	}
	if !slices.Contains(cmd.Env, "HOME="+pf.home) {
		t.Fatalf("production spawn did not keep fixture HOME: %v", cmd.Env)
	}
}
