package config

import "testing"

func TestValidateExplicitRawReadableScopesPreservesLegacyProviderAndSharedFactScopes(t *testing.T) {
	ctx := &editValidationContext{
		rawUser: decode(t, `{}`),
		rawWorkspace: decode(t, `{"providers":{
			"legacy":{"region":"us-east-1","models":{"custom":{"id":"custom-model","context_window":8192}}},
			"canonical":{"every_model":{"context":16384}}
		}}`),
	}
	if err := validateExplicitRawReadableScopes(ctx); err != nil {
		t.Fatalf("legacy provider fields or both-scope canonical facts were refused: %v", err)
	}
}
