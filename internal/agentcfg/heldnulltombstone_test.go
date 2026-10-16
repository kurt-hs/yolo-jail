package agentcfg

import "testing"

// A steady-state capture drops every tombstone at a keypath the CURRENT file holds as a
// literal null, and keeps a real deletion — including the deletion of a key the stale last
// render held as `null`, which an authority reading last_render would have missed
// (docs/reference/config-ownership.md#CO-D20).
//
// With a host layer, so the real deletion is one the render must keep enforcing (the host
// would re-emit `extra`) and the converged-overlay retirement cannot drop it.
func TestCaptureKeepsARealDeletionOfARenderedNull(t *testing.T) {
	host := `{"extra":"fromHost","env":{"FOO":"fromHost"}}`
	lastRender := `{"defaultProjectTrust":"always","theme":"system","extra":null,"env":{"BAR":"0","FOO":"fromHost"}}`
	// In the jail: `extra` deleted, `statusLine` added as null, env.FOO set to null, env.BAR edited.
	current := `{"defaultProjectTrust":"always","theme":"system","statusLine":null,"env":{"BAR":"1","FOO":null}}`

	out, err := ComposeStateful(StatefulInputs{
		Base:              Inputs{Surface: piSurface(), HostBytes: []byte(host)},
		CurrentBytes:      []byte(current),
		LastRenderPresent: true,
		LastRenderBytes:   []byte(lastRender),
		OverlayJSON:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("ComposeStateful: %v", err)
	}
	overlay := jsonObj(t, string(out.OverlayJSON))
	if v, ok := overlay["extra"]; !ok || v != nil {
		t.Errorf("a real deletion must stay a tombstone: overlay[extra] = %v (present=%v)\n%s", v, ok, out.OverlayJSON)
	}
	if _, ok := overlay["statusLine"]; ok {
		t.Errorf("a null the file holds was captured as a deletion tombstone:\n%s", out.OverlayJSON)
	}
	env, _ := overlay["env"].(map[string]any)
	if _, ok := env["FOO"]; ok {
		t.Errorf("a nested null the file holds was captured as a deletion tombstone:\n%s", out.OverlayJSON)
	}
	if env["BAR"] != "1" {
		t.Errorf("the live sibling edit was not captured:\n%s", out.OverlayJSON)
	}

	// The render is unchanged by the narrowing: the nulls come back through the literal-null
	// channel, and the real deletion still beats the host layer.
	cfg := out.Result.ConfigMap()
	if v, ok := cfg["statusLine"]; !ok || v != nil {
		t.Errorf("render lost the user's statusLine null: %v", cfg)
	}
	renv, _ := cfg["env"].(map[string]any)
	if v, ok := renv["FOO"]; !ok || v != nil {
		t.Errorf("render lost the user's env.FOO null: %v", cfg)
	}
	if _, ok := cfg["extra"]; ok {
		t.Errorf("the deleted key came back from the host layer: %v", cfg)
	}
}
