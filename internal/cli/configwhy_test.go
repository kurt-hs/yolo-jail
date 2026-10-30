package cli

import (
	"strings"
	"testing"
)

func TestConfigWhyNamesFileLineAndDefault(t *testing.T) {
	fx := newNamedVerbFixture(t, "[machine.resources]\ncpus = 4\n",
		"# overrides\n\n[active_provider]\ndefault = \"native\"\nclaude = \"native\"\n")

	rc, out, errs := captureStd(t, func() int {
		return registry["config"]([]string{"config", "why", "active_provider.claude"})
	})
	if rc != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	for _, want := range []string{
		`active_provider.claude = "native"`,
		"set in  ~/.config/yolo-jail/config.d/work.toml:5",
		`default would be: active_provider.default, "native", set in ~/.config/yolo-jail/config.d/work.toml:4`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config why output lacks %q:\n%s", want, out)
		}
	}

	rc, out, errs = captureStd(t, func() int {
		return registry["config"]([]string{"config", "why", "machine.resources.cpus"})
	})
	if rc != 0 || !strings.Contains(out, "machine.resources.cpus = 4") || !strings.Contains(out, "set in  "+fx.project+":2") {
		t.Fatalf("project key rc=%d stdout=%q stderr=%q", rc, out, errs)
	}

	rc, out, errs = captureStd(t, func() int {
		return registry["config"]([]string{"config", "why", "machine.resources.memory"})
	})
	if rc != 0 || !strings.Contains(out, "is not set") || !strings.Contains(out, "yolo config set machine.resources.memory") {
		t.Fatalf("unset key rc=%d stdout=%q stderr=%q", rc, out, errs)
	}

	rc, _, errs = captureStd(t, func() int { return registry["config"]([]string{"config", "why"}) })
	if rc != 2 || !strings.Contains(errs, "usage: yolo config why") {
		t.Fatalf("bare why rc=%d stderr=%q", rc, errs)
	}
}
