package packdecl

import (
	"strings"
	"testing"
)

func TestVersionCompatibilitySurvivesDecodeAndInstallProjection(t *testing.T) {
	readers := []struct {
		name     string
		manifest string
		decode   func([]byte) (*Manifest, []string)
	}{
		{
			name: "JSON",
			manifest: `{"name":"copilot","contributes":[{
			  "kind":"program","bin":"copilot","via":"installer","url":"https://example.test/install",
			  "probe_args":["--version"],"version_compatibility":{
			    "refuse_at_or_above":"1.0.93","message":"Use a container; don't use self mode."
			  }
			}]}`,
			decode: Decode,
		},
		{
			name: "canonical TOML",
			manifest: `name = "copilot"

[[program]]
bin = "copilot"
via = "installer"
url = "https://example.test/install"
probe_args = ["--version"]
version_compatibility = { refuse_at_or_above = "1.0.93", message = "Use a container; don't use self mode." }
`,
			decode: DecodeTOML,
		},
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			m, problems := reader.decode([]byte(reader.manifest))
			if len(problems) != 0 {
				t.Fatalf("compatibility declaration should validate: %v", problems)
			}
			installs := m.InstallContributions()
			if len(installs) != 1 || installs[0].VersionCompatibility == nil {
				t.Fatalf("compatibility did not reach InstallContributions: %+v", installs)
			}
			got := installs[0].VersionCompatibility
			if got.RefuseAtOrAbove != "1.0.93" || got.Message != "Use a container; don't use self mode." {
				t.Errorf("projected compatibility = %+v", got)
			}
		})
	}
}

func TestVersionCompatibilityRejectsUnusableDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name, declaration, want string
	}{
		{"bad numeric threshold", `"probe_args":["--version"],"version_compatibility":{"refuse_at_or_above":"1.0.9999999999","message":"use container"}`, "at most 9 digits"},
		{"missing probe", `"version_compatibility":{"refuse_at_or_above":"1.0.93","message":"use container"}`, "requires the program's declared probe_args"},
		{"shell multiline message", `"probe_args":["--version"],"version_compatibility":{"refuse_at_or_above":"1.0.93","message":"bad\nmessage"}`, "one printable line"},
		{"not native installer", `"kind":"program","bin":"copilot","via":"npm","package":"x","probe_args":["--version"],"version_compatibility":{"refuse_at_or_above":"1.0.93","message":"use container"}`, `only a native installer program takes version_compatibility`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry string
			if strings.HasPrefix(tc.declaration, `"kind"`) {
				entry = tc.declaration
			} else {
				entry = `"kind":"program","bin":"copilot","via":"installer","url":"https://example.test/install",` + tc.declaration
			}
			_, problems := Decode([]byte(`{"name":"p","contributes":[{` + entry + `}]}`))
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}
