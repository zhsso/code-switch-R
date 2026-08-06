package services

import (
	"bytes"
	"os"
	"testing"
)

func TestRemovedCompatibilityModeIsIgnoredAndDropped(t *testing.T) {
	setupRenameTestEnv(t)
	path, err := providerFilePath(CodexPlatform)
	if err != nil {
		t.Fatalf("resolve provider path: %v", err)
	}
	legacy := []byte(`{"providers":[{"id":1,"name":"Legacy","apiUrl":"https://example.com","apiKey":"key","compatibilityMode":"deepseek-codex"}]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy provider config: %v", err)
	}

	service := NewProviderService()
	providers, err := service.LoadProviders(CodexPlatform)
	if err != nil {
		t.Fatalf("load config with removed compatibility mode: %v", err)
	}
	if len(providers) != 1 || providers[0].Name != "Legacy" {
		t.Fatalf("unexpected providers: %+v", providers)
	}
	if err := service.SaveProviders(CodexPlatform, providers); err != nil {
		t.Fatalf("save config after removing compatibility mode: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved provider config: %v", err)
	}
	if bytes.Contains(saved, []byte("compatibilityMode")) {
		t.Fatalf("removed compatibility mode was persisted: %s", saved)
	}
}
