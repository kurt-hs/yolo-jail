package packs_test

import (
	"io/fs"
	"sort"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/json5"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/packs"
)

// legacyManifestException says why a shipped pack stays on its pack.json.
type legacyManifestException struct {
	// reason is the sentence the failure messages repeat.
	reason string
	// holds reports whether the pack's legacy manifest still needs the exception, so a pack that
	// no longer does is told to convert.
	holds func(legacy *packdecl.Manifest, decoded any) bool
}

// holdsJSONNull is the exception for a manifest that holds a JSON null. A grouped pack.toml
// cannot say one (TOML has none), and each of these is read as "declared, no default" or "delete
// this key": dropping the key would remove the option from the provider's declared set, so a
// profile that sets it would be refused as undeclared, and spelling it as an empty string would
// hand a derive a value where it had none (docs/design/config-migration.md, "Shipped packs").
func holdsJSONNull(_ *packdecl.Manifest, decoded any) bool { return jsonHoldsNull(decoded) }

// holdsPartialInstallHints is the exception for a program whose legacy install_hints name fewer
// than every package manager. The grouped reader gives a manager the program does not name the
// bin as its package (`install` absent means every manager calls it by the bin), so converting
// such a program would invent a package for each manager it left out, `apt install pi` for a
// program installed as pi-coding-agent, and make provisioner and check-deps answers that
// were "no recipe" into recipes. It retires when the grouped manifest has a spelling for "no hint
// for this manager" and `yolo pack migrate` writes it.
func holdsPartialInstallHints(legacy *packdecl.Manifest, _ any) bool {
	for _, c := range legacy.Contributes {
		if c.Kind != packdecl.KindProgram || c.ForkOf != "" {
			continue
		}
		for _, manager := range []string{"brew", "apt", "dnf", "pacman", "nix"} {
			if _, named := c.InstallHints[manager]; !named {
				return true
			}
		}
	}
	return false
}

var legacyManifestExceptions = map[string]legacyManifestException{
	"cerebras":   {"provider option pin_model is declared with no default (null)", holdsJSONNull},
	"codex":      {"config surface managed key tui.model_availability_nux is a deletion (null)", holdsJSONNull},
	"kilo":       {"provider options api_timeout_ms, context_window, max_tokens, model and pin_model are declared with no default (null)", holdsJSONNull},
	"llamacpp":   {"provider options api_timeout_ms and pin_model are declared with no default (null)", holdsJSONNull},
	"openrouter": {"provider options model and pin_model are declared with no default (null)", holdsJSONNull},
	"zai":        {"provider option pin_model is declared with no default (null)", holdsJSONNull},

	"agy":             {"its program names a package manager's package for only some managers", holdsPartialInstallHints},
	"chrome-devtools": {"its program names no package manager's package", holdsPartialInstallHints},
	"claude":          {"its program names a package manager's package for only some managers", holdsPartialInstallHints},
	"copilot":         {"its program names a package manager's package for only some managers", holdsPartialInstallHints},
	"muse-code":       {"its program names no package manager's package", holdsPartialInstallHints},
	"omp":             {"its program names no package manager's package", holdsPartialInstallHints},
	"opencode":        {"its program names a package manager's package for only some managers", holdsPartialInstallHints},
	"pi":              {"its program names a package manager's package for only some managers", holdsPartialInstallHints},
}

// jsonHoldsNull reports whether any value in a decoded JSON document is null.
func jsonHoldsNull(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case map[string]any:
		for _, item := range v {
			if jsonHoldsNull(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if jsonHoldsNull(item) {
				return true
			}
		}
	}
	return false
}

// TestNoShippedPackHasAJSONManifest pins the conversion of the shipped packs to pack.toml: a
// shipped pack carries a pack.toml, never both manifests, and a pack.json only where
// legacyManifestExceptions says converting it would change what it means.
func TestNoShippedPackHasAJSONManifest(t *testing.T) {
	entries, err := fs.ReadDir(packs.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	var converted, legacy []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		_, tomlErr := fs.Stat(packs.FS, name+"/pack.toml")
		legacyRaw, jsonErr := fs.ReadFile(packs.FS, name+"/pack.json")
		_, jsoncErr := fs.Stat(packs.FS, name+"/pack.jsonc")
		hasTOML, hasJSON := tomlErr == nil, jsonErr == nil
		if jsoncErr == nil {
			t.Errorf("packs/%s ships a pack.jsonc: convert it with `yolo pack migrate packs/%s --yes`", name, name)
		}
		exception, excepted := legacyManifestExceptions[name]
		reason := exception.reason
		switch {
		case hasTOML && hasJSON:
			t.Errorf("packs/%s ships both pack.toml and pack.json: delete the pack.json", name)
		case hasTOML:
			converted = append(converted, name)
			if excepted {
				t.Errorf("packs/%s is converted but still listed as a legacy-manifest exception (%s): "+
					"delete the exception", name, reason)
			}
		case hasJSON:
			legacy = append(legacy, name)
			if !excepted {
				t.Errorf("packs/%s ships a pack.json: run `yolo pack migrate packs/%s --yes "+
					"--keep-install-hints`, or add it to legacyManifestExceptions with the reason", name, name)
				continue
			}
			decoded, err := json5.Decode(legacyRaw)
			if err != nil {
				t.Errorf("packs/%s/pack.json does not parse: %v", name, err)
				continue
			}
			legacyManifest, problems := packdecl.Decode(legacyRaw)
			if len(problems) != 0 {
				t.Errorf("packs/%s/pack.json does not decode: %v", name, problems)
				continue
			}
			if !exception.holds(legacyManifest, jsonx.Plain(decoded)) {
				t.Errorf("packs/%s no longer needs its exception (%s): run `yolo pack migrate "+
					"packs/%s --yes --keep-install-hints` and delete the entry", name, reason, name)
			}
		}
	}
	for name := range legacyManifestExceptions {
		if _, err := fs.Stat(packs.FS, name+"/pack.json"); err != nil {
			if _, err := fs.Stat(packs.FS, name+"/pack.toml"); err != nil {
				t.Errorf("legacyManifestExceptions names %q, which ships no manifest", name)
			}
		}
	}
	sort.Strings(converted)
	sort.Strings(legacy)
	if len(converted) == 0 {
		t.Fatal("no shipped pack carries a pack.toml: the test is not reading the tree it exists for")
	}
	t.Logf("converted %d packs; legacy manifest by exception: %v", len(converted), legacy)
}

// TestShippedProfilesAreNotProviderNames pins OQ-RC3: a provider is selectable by its own name
// (config.LoadSelectableProfiles), so a shipped profile named for the provider it selects is a
// second spelling of the same selection and is deleted. A profile with a different name is a
// variant (bedrock-bridge adds a via) and stays.
func TestShippedProfilesAreNotProviderNames(t *testing.T) {
	entries, err := fs.ReadDir(packs.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	profiles := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, profile := range shippedManifest(t, entry.Name()).Profiles() {
			profiles++
			if profile.Name == profile.Provider {
				t.Errorf("packs/%s ships profile %q over provider %q: delete it, the provider is "+
					"selectable by that name (OQ-RC3)", entry.Name(), profile.Name, profile.Provider)
			}
		}
	}
	if profiles == 0 {
		t.Fatal("no shipped pack declares a profile: the test is not reading the tree it exists for")
	}
}
