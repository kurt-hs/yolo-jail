package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// TestOrdinaryPreparationIsHandedTheLaunchsImageIdentity pins the launch's call site: the ordinary
// preparation is handed this launch's image identity, through the launch's one evaluation, so a
// kept "the image already provides it" verdict can answer without a producer jail — and the image
// step after it reuses that evaluation (two asks, one evaluation: the launch's memo).
func TestOrdinaryPreparationIsHandedTheLaunchsImageIdentity(t *testing.T) {
	evals := 0
	prev := identityEval
	identityEval = func(context.Context, string) (string, bool) { evals++; return "sha256:launch", true }
	t.Cleanup(func() { identityEval = prev })
	var got string
	var gotOK bool
	fakePodmanLaunch(t, func(o *Options) {
		o.CapturesDir = func() string { return t.TempDir() }
		repo, _ := o.RepoRoot()
		for _, name := range []string{"flake.nix", "flake.lock"} {
			if err := os.WriteFile(filepath.Join(repo.Root, name), []byte("fixture"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		o.PreparePrograms = func(req ProgramCaptureRequest) (map[string]entrypoint.ProgramCaptureDelivery, error) {
			if req.ImageIdentity == nil {
				t.Fatal("the ordinary preparation was handed no image identity")
			}
			got, gotOK = req.ImageIdentity()
			got, gotOK = req.ImageIdentity()
			return nil, nil
		}
	})
	if !gotOK || got != "sha256:launch" {
		t.Fatalf("image identity = %q %v, want the launch's evaluation", got, gotOK)
	}
	if evals != 1 {
		t.Errorf("two asks made %d evaluations, want the launch's one", evals)
	}
}
