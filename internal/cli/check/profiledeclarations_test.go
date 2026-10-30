package check

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// profiledeclarations_test.go pins `yolo check`'s prediction of the launch's undeclared-profile
// refusal (profiledeclarations.go), at the call site: sectionMergedConfig. A config selecting a
// profile no selected pack declares FAILs with the launch's own next step; the same config with
// the declaring pack selected passes. Deleting the call in sectionMergedConfig fails the first.

func runMergedConfigSection(t *testing.T, cfgJSON string) string {
	t.Helper()
	cfg := capCfg(t, cfgJSON)
	empty := jsonx.NewOrderedMap()
	var buf bytes.Buffer
	r := newReporter(&buf, false)
	o := &Options{
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, bool) { return "", false },
	}
	o.sectionMergedConfig(r, cfg, t.TempDir(), empty, empty, nil, false)
	return stripANSI(buf.String())
}

func TestCheckFailsAProfileNoSelectedPackDeclares(t *testing.T) {
	capabilityHome(t, `{"packs": ["pi"]}`)
	got := runMergedConfigSection(t, `{"profile": {"pi": "copilot"}}`)
	for _, want := range []string{"[FAIL]", `profile "copilot" selected for pi`,
		`no profile named "copilot" is declared`, `add "copilot-auth" to your config's`} {
		if !strings.Contains(got, want) {
			t.Errorf("check must FAIL with the launch's refusal, lacking %q:\n%s", want, got)
		}
	}
}

func TestCheckTypesTheDeclarationForABuiltInName(t *testing.T) {
	capabilityHome(t, `{"packs": ["pi"]}`)
	got := runMergedConfigSection(t, `{"profile": {"pi": "groq"}}`)
	if !strings.Contains(got, "[FAIL]") || !strings.Contains(got, "pi has groq built in") {
		t.Errorf("check must FAIL typing out the declaration:\n%s", got)
	}
}

func TestCheckPassesAProfileTheSelectedPackDeclares(t *testing.T) {
	capabilityHome(t, `{"packs": ["pi", "copilot-auth"]}`)
	got := runMergedConfigSection(t, `{"profile": {"pi": "copilot"}}`)
	if strings.Contains(got, "no profile named") {
		t.Errorf("copilot-auth is selected and declares copilot, so check must pass:\n%s", got)
	}
}
