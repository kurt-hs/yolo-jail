package packdecl

import (
	"strings"
	"testing"
)

func TestVersionCompatibilityDataDecodesAndValidates(t *testing.T) {
	const valid = `{"refuse_at_or_above":"1.0.93","message":"Use a container."}`
	got, problems := DecodeVersionCompatibility([]byte(valid), []string{"--version"})
	if len(problems) != 0 || got == nil || got.RefuseAtOrAbove != "1.0.93" || got.Message != "Use a container." {
		t.Fatalf("DecodeVersionCompatibility = %+v, %v", got, problems)
	}
}

func TestVersionCompatibilityDataRejectsMalformedDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name, data, probe, want string
	}{
		{"malformed", `{`, "--version", "malformed JSON object"},
		{"duplicate field", `{"refuse_at_or_above":"1.0.93","refuse_at_or_above":"1.0.92","message":"use container"}`, "--version", "duplicate field"},
		{"unknown field", `{"refuse_at_or_above":"1.0.93","message":"use container","agent":"copilot"}`, "--version", "unknown field"},
		{"bad numeric threshold", `{"refuse_at_or_above":"1.0.9999999999","message":"use container"}`, "--version", "at most 9 digits"},
		{"missing probe", `{"refuse_at_or_above":"1.0.93","message":"use container"}`, "", "declared probe_args"},
		{"shell multiline message", `{"refuse_at_or_above":"1.0.93","message":"bad\nmessage"}`, "--version", "one printable line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, problems := DecodeVersionCompatibility([]byte(tc.data), strings.Fields(tc.probe))
			if got != nil || !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("DecodeVersionCompatibility = %+v, %v; want error containing %q", got, problems, tc.want)
			}
		})
	}
}

func TestVersionCompatibilityIsNotAProgramManifestField(t *testing.T) {
	const manifest = `{"name":"copilot","contributes":[{"kind":"program","bin":"copilot","via":"installer","url":"https://example.test/install","probe_args":["--version"],"version_compatibility":{"refuse_at_or_above":"1.0.93","message":"Use a container."}}]}`
	_, problems := Decode([]byte(manifest))
	if !strings.Contains(strings.Join(problems, "\n"), `unknown field "version_compatibility"`) {
		t.Fatalf("program manifest field unexpectedly remains declared: %v", problems)
	}
}
