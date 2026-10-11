package sensitivename

import "testing"

func TestIsMatchesWholeTokensInEveryNamingStyle(t *testing.T) {
	for name, want := range map[string]bool{
		"TAVILY_API_KEY": true, "apiKeyHelper": true, "OAuthToken": true, "--api-key": true,
		"APIKey": true, "monkey": false, "theme": false, "AWS_PROFILE": false, "ultracode": false,
	} {
		if got := Is(name); got != want {
			t.Errorf("Is(%q) = %v, want %v", name, got, want)
		}
	}
}
