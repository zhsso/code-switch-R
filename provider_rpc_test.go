package main

import (
	"encoding/json"
	"strings"
	"testing"

	"codeswitch/services"
)

func TestMaskProviderDoesNotMarshalCredential(t *testing.T) {
	const secret = "sk-secret-provider-token"
	view := maskProvider(services.Provider{
		ID:              7,
		Name:            "provider",
		APIKey:          secret,
		APIURL:          "https://user:" + secret + "@example.test/base?api_key=" + secret,
		APIEndpoint:     "/responses?token=" + secret,
		FallbackAPIURLs: []string{"https://fallback.test?credential=" + secret},
	})
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("masked provider response contains credential: %s", encoded)
	}
	if view.APIKey != "" || !view.APIKeyConfigured {
		t.Fatalf("unexpected masked view: %#v", view)
	}
	if view.APIURL != "https://example.test/base?api_key=redacted" {
		t.Fatalf("masked provider URL = %q", view.APIURL)
	}
}

func TestMergeProviderViewsPreservesOrReplacesCredentialExplicitly(t *testing.T) {
	current := []services.Provider{{
		ID:              7,
		Name:            "provider",
		APIKey:          "existing-secret",
		APIURL:          "https://example.test?api_key=url-secret",
		APIEndpoint:     "/responses?token=endpoint-secret",
		FallbackAPIURLs: []string{"https://fallback.test?key=fallback-secret"},
	}}
	masked := maskProvider(current[0])

	masked.Name = "renamed"
	preserved := mergeProviderViews(current, []providerRPCView{masked})
	if got := preserved[0].APIKey; got != "existing-secret" {
		t.Fatalf("unchanged credential = %q, want preserved value", got)
	}
	if preserved[0].APIURL != current[0].APIURL ||
		preserved[0].APIEndpoint != current[0].APIEndpoint ||
		preserved[0].FallbackAPIURLs[0] != current[0].FallbackAPIURLs[0] {
		t.Fatalf("masked URL credentials were not preserved: %#v", preserved[0])
	}

	replaced := mergeProviderViews(current, []providerRPCView{{
		Provider:      services.Provider{ID: 7, Name: "provider"},
		APIKey:        "replacement-secret",
		APIKeyChanged: true,
	}})
	if got := replaced[0].APIKey; got != "replacement-secret" {
		t.Fatalf("changed credential = %q, want replacement", got)
	}

	cleared := mergeProviderViews(current, []providerRPCView{{
		Provider:      services.Provider{ID: 7, Name: "provider"},
		APIKeyChanged: true,
	}})
	if cleared[0].APIKey != "" {
		t.Fatalf("explicitly cleared credential = %q", cleared[0].APIKey)
	}
}
