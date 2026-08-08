package services

import (
	"errors"
	"testing"
)

func TestUpdateProvidersRejectsStaleGeneration(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	if err := service.SaveProviders(CodexPlatform, []Provider{{ID: "1", Name: "one", APIURL: "https://example.test"}}); err != nil {
		t.Fatal(err)
	}

	providers, generation, err := service.LoadProvidersWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("providers = %#v", providers)
	}

	_, nextGeneration, err := service.UpdateProviders(CodexPlatform, generation, func(current []Provider) ([]Provider, error) {
		current[0].APIURL = "https://first.example.test"
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if nextGeneration <= generation {
		t.Fatalf("generation did not advance: before=%d after=%d", generation, nextGeneration)
	}

	_, _, err = service.UpdateProviders(CodexPlatform, generation, func(current []Provider) ([]Provider, error) {
		current[0].APIURL = "https://stale.example.test"
		return current, nil
	})
	if !errors.Is(err, ErrProviderConfigConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	stored, err := service.LoadProviders(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored[0].APIURL; got != "https://first.example.test" {
		t.Fatalf("stale update overwrote current value: %q", got)
	}
}
