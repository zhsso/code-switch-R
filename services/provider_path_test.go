package services

import (
	"path/filepath"
	"testing"
)

func TestProviderFilePathAcceptsCodexOnly(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	path, err := providerFilePath(CodexPlatform)
	if err != nil {
		t.Fatalf("Codex path should be accepted: %v", err)
	}
	if filepath.Base(path) != "codex.json" {
		t.Fatalf("provider path = %q, want codex.json", path)
	}
	for _, platform := range []string{"claude", "gemini", "custom:tool"} {
		if _, err := providerFilePath(platform); err == nil {
			t.Errorf("removed platform %q should be rejected", platform)
		}
	}
}
