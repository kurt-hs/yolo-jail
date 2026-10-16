package cli

// hostmodelfacts_test.go pins the host notch's CALL SITE for the shared model facts
// (docs/design/model-lists-and-pickers.md OQ-ML5, ML-D12): composedHostProviders hands the
// config's top-level `model_facts` to the composer on both of its branches, the one a launch that
// starts a service takes and the one that starts none, as the jail notch's composedProviders does.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestHostComposedProvidersLaysTheSharedModelFactsUnderARoute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	decoded, err := jsonx.Decode([]byte(`{
  "model_facts": {"big": {"max_tokens": 32000, "reasoning": true}},
  "providers": {"acme": {"api_key_env_name": "ACME_API_KEY",
    "endpoints": {"openai": {"base_url": "https://acme.example/v1"}},
    "models": {"big": {"id": "acme-big", "extends": "big", "max_tokens": 16000}}}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	providers, _, err := composedHostProviders(decoded.(*jsonx.OrderedMap), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := jsonx.DumpsCompact(providers)
	if !strings.Contains(s, `"big": {"reasoning": "true", "max_tokens": "16000", "extends": "big"}`) {
		t.Errorf("composed table = %s, want the route's own max_tokens over the shared facts", s)
	}
}
