package capture

import "testing"

func TestProgramInputsIdentityIncludesCompatibilityNotWorkspace(t *testing.T) {
	base := ProgramInputs{Owner: "fixture", Bin: "probe", Declared: "@fixture/probe@1", Resolver: "npm", Release: "1.2.3", Recipe: "recipe", Dependencies: []string{"mcp@2", "refresh@1"}, Runtime: "node", Platform: "linux/amd64", NodeABI: "127"}
	first, err := base.Identity()
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Dependencies = []string{"refresh@1", "mcp@2", "mcp@2"}
	if got, err := reordered.Identity(); err != nil || got != first {
		t.Fatalf("dependency set changed identity: %s %v", got, err)
	}
	changes := []func(*ProgramInputs){func(p *ProgramInputs) { p.Owner = "other" }, func(p *ProgramInputs) { p.Bin = "other" }, func(p *ProgramInputs) { p.Declared = "@fixture/probe@2" }, func(p *ProgramInputs) { p.Release = "2" }, func(p *ProgramInputs) { p.Source = "git+https://fixture.invalid/probe" }, func(p *ProgramInputs) { p.Recipe = "new" }, func(p *ProgramInputs) { p.Dependencies = []string{"mcp@3"} }, func(p *ProgramInputs) { p.Runtime = "other" }, func(p *ProgramInputs) { p.Platform = "darwin/arm64" }, func(p *ProgramInputs) { p.NodeABI = "128" }}
	for i, change := range changes {
		next := base
		change(&next)
		got, err := next.Identity()
		if err != nil || got == first {
			t.Fatalf("change %d: %s %v", i, got, err)
		}
	}
	base.NodeABI = ""
	if _, err := base.Identity(); err == nil {
		t.Fatal("npm identity without observed Node ABI accepted")
	}
	base = ProgramInputs{}
	if _, err := base.Identity(); err == nil {
		t.Fatal("empty compatibility accepted")
	}
}
