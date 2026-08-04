package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryHasNoDesktopRuntimeSurface(t *testing.T) {
	for _, path := range []string{"build", "frontend/bindings"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("desktop runtime path must not exist: %s", path)
		}
	}

	for _, path := range []string{"go.mod", "frontend/package.json", "frontend/package-lock.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{"wailsapp", "@wailsio/runtime", "gen2brain/beeep"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s still contains desktop dependency %q", path, forbidden)
			}
		}
	}
}

func TestComposeUsesHostNetworkAndNamedDataVolume(t *testing.T) {
	data, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)
	for _, required := range []string{
		"network_mode: host",
		"codeswitch-data:/data",
		"codeswitch-data:",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("compose.yaml must contain %q", required)
		}
	}
	if strings.Contains(compose, "./:/data") || strings.Contains(compose, filepath.Clean(os.Getenv("HOME"))) {
		t.Fatal("compose.yaml must not bind-mount a host directory into /data")
	}
}
