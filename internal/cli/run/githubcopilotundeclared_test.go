package run

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// githubcopilotundeclared_test.go pins the launch's undeclared-profile refusal at the call site
// for a profile only an UNSELECTED shipped pack declares: since BI-D14 packs/github-copilot, not
// packs/pi, declares `github-copilot`, so pi alone refuses `"profile": {"pi": "github-copilot"}`,
// and the refusal names the pack to add (the Happy Path) rather than only listing what is
// declared.
func TestAnUndeclaredGitHubCopilotProfileNamesItsPack(t *testing.T) {
	home := packHome(t)
	o := goldenOptions(t.TempDir(), home)
	cfg := bareConfig()
	prof := jsonx.NewOrderedMap()
	prof.Set("pi", "github-copilot")
	cfg.Set("profile", prof)
	_, err := o.composePackChannel(cfg, []*packload.Pack{officialPack(t, "pi")}, zaiAndRouterKeys())
	if err == nil || !strings.Contains(err.Error(), `no profile named "github-copilot" is declared`) ||
		!strings.Contains(err.Error(), `add "github-copilot" to your config's `+"`packs`") {
		t.Fatalf("composePackChannel = %v, want the refusal to name adding the github-copilot pack", err)
	}
	// Selected beside pi, the pack declares it.
	if _, err := o.composePackChannel(cfg, []*packload.Pack{officialPack(t, "pi"),
		officialPack(t, "github-copilot")}, zaiAndRouterKeys()); err != nil {
		t.Fatalf("pi with the github-copilot pack: %v", err)
	}
}
