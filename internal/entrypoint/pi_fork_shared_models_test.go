package entrypoint

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// pi_fork_shared_models_test.go pins where `reads_shared_models` is declared (docs/design/
// model-lists-and-pickers.md ML-D11, ML-D14; the downstream handoff's §11): on the `program`
// contribution that supplies pi's bytes. A fork pack (`fork_of`) supplies them, so ITS
// declaration is the one the render reads, and the base pack's is dropped with the rest of the
// base's delivery. Before, the fork rewrite kept the base's (never set by packs/pi), and a fork
// declaring it did not switch the render.

// piForkReadingSharedModels is a fork of packs/pi's program declaring it reads the shared form,
// the shape a consumer's patched pi pack takes. yolo knows no such pack: any fork does.
func piForkReadingSharedModels(t *testing.T) *packload.Pack {
	t.Helper()
	return namedFixturePack(t, "some-pi-fork", `{"kind":"program","bin":"pi","via":"source","fork_of":"pi",
	  "source":"git+https://example.com/pi-fork?ref=main","build":"make install",
	  "produces":[".local/bin/pi"],"reads_shared_models":true}`)
}

func TestAPiForkDeclaringReadsSharedModelsSwitchesTheRender(t *testing.T) {
	pi, err := embeddedPack("pi")
	if err != nil {
		t.Fatal(err)
	}
	ocode, err := embeddedPack("opencode")
	if err != nil {
		t.Fatal(err)
	}
	// The fork rewrite every reader of a launch's packs runs first (loadPackRoot,
	// config.applyForks).
	packs, err := packload.ApplyForks([]*packload.Pack{pi, ocode, piForkReadingSharedModels(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !packload.ReadsSharedModels(packs, "pi") {
		t.Fatal("after the fork rewrite, pi does not read the shared form its fork declares")
	}

	// A route on a provider that renders to both surfaces: models.json (pi does not ship acme) and
	// the model-lists file (its routing makes it a registration).
	routed := `{"acme":{"endpoints":{"openai":{"base_url":"https://acme.example/v1","wire_api":"openai-chat-completions"}},
	  "api_key_env_name":"ACME_API_KEY","models":{"big":{"id":"acme-big","extends":"big","context_window":64000,
	    "openrouter_routing":{"order":["x"]}}}}}`
	r := newPioencodeRender(t, composedFor(t, routed, sharedBig))
	r.e.Vars["YOLO_USE_PROFILES"] = `{"pi":"acme"}`
	r.wireProfiles(nameAsProviderTable(t, `{"pi":"acme"}`))
	ConfigurePackSurfaces(r.e, packs)
	if fails := r.e.GenFailures(); len(fails) != 0 {
		t.Fatalf("boot render failed: %v\n%s", fails, r.errw.String())
	}

	m := r.piModels(t)
	if shared, _ := m["sharedModels"].(map[string]any); shared["big"] == nil {
		t.Errorf("models.json sharedModels = %v, want big written once", m["sharedModels"])
	}
	if row := piRow(t, m, "acme", "acme-big"); row["extends"] != "big" || row["maxTokens"] != nil {
		t.Errorf("models.json acme-big = %v, want extends big and only its own facts", row)
	}

	// THE MENU FILE carries the same `sharedModels` and each row's `extends`, its rows still
	// FULL: yolo's own extension, the file's reader, resolves no `extends`.
	lists := piModelListsFileWhole(t, r)
	if shared, _ := lists["sharedModels"].(map[string]any); shared["big"] == nil {
		t.Errorf("model-lists sharedModels = %v, want big", lists["sharedModels"])
	}
	providers, _ := lists["providers"].(map[string]any)
	acme, _ := providers["acme"].(map[string]any)
	rows, _ := acme["models"].([]any)
	if len(rows) != 1 {
		t.Fatalf("model-lists acme = %v", acme)
	}
	row, _ := rows[0].(map[string]any)
	if row["extends"] != "big" || row["maxTokens"] != float64(32000) || row["contextWindow"] != float64(64000) {
		t.Errorf("menu row = %v, want extends big with every fact still stated", row)
	}
}

// piModelListsFileWhole decodes the whole pi/model-lists file a render wrote.
func piModelListsFileWhole(t *testing.T, r *pioencodeRender) map[string]any {
	t.Helper()
	return r.surface(t, ".pi", "agent", "yolo-model-lists.json")
}

// A STOCK pi's menu file carries neither key.
func TestAStockPiMenuFileCarriesNoSharedForm(t *testing.T) {
	routed := `{"acme":{"endpoints":{"openai":{"base_url":"https://acme.example/v1","wire_api":"openai-chat-completions"}},
	  "api_key_env_name":"ACME_API_KEY","models":{"big":{"id":"acme-big","extends":"big",
	    "openrouter_routing":{"order":["x"]}}}}}`
	r := newPioencodeRender(t, composedFor(t, routed, sharedBig))
	r.render(t, `{"pi":"acme"}`)
	lists := piModelListsFileWhole(t, r)
	if _, has := lists["sharedModels"]; has {
		t.Errorf("a stock pi's menu file carries sharedModels: %v", lists)
	}
	providers, _ := lists["providers"].(map[string]any)
	acme, _ := providers["acme"].(map[string]any)
	rows, _ := acme["models"].([]any)
	for _, r := range rows {
		if _, has := r.(map[string]any)["extends"]; has {
			t.Errorf("a stock pi's menu row carries extends: %v", r)
		}
	}
}
