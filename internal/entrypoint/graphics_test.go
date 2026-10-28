package entrypoint

import "testing"

// This intentionally calls through the production boot table: deleting the
// graphics step is a RED failure, even when the generator implementation exists.
func TestContainerBootCallsGraphicsDriverGenerator(t *testing.T) {
	var found *bootStep
	for _, step := range bootSteps() {
		if step.name == "configure_graphics_drivers" {
			found = &step
			break
		}
	}
	if found == nil {
		t.Fatal("container boot has no configure_graphics_drivers step")
	}
	if !isGen(*found, GenerateGraphicsDrivers) {
		t.Fatal("configure_graphics_drivers no longer calls GenerateGraphicsDrivers")
	}
	assertStepBefore(t, bootContainer, "generate_store_packages", "configure_graphics_drivers",
		"store profile metadata must be available before graphics exposure")
	assertStepBefore(t, bootContainer, "configure_graphics_drivers", "export_image_ld_lib",
		"graphics exposure must precede remaining application setup")
	if found.excludedFrom(bootContainer) != "" || found.excludedFrom(bootDarwin) == "" {
		t.Fatalf("graphics step target exclusions = container:%q darwin:%q, want container-only",
			found.excludedFrom(bootContainer), found.excludedFrom(bootDarwin))
	}
	if found.notSessionPass == "" {
		t.Fatal("graphics step must be omitted on session passes, which reuse launch /run state")
	}
	e := NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1"})
	runSteps(&bootRun{e: e, target: bootContainer, sessionPass: true}, []bootStep{*found})
	if len(e.genFailures) != 0 {
		t.Fatalf("session pass ran graphics generation without launch metadata: %v", e.genFailures)
	}
}
