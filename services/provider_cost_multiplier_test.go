package services

import (
	"math"
	"strings"
	"testing"
)

func TestProviderEffectiveCostMultiplier(t *testing.T) {
	tests := []struct {
		name       string
		configured float64
		want       float64
	}{
		{name: "legacy missing value", configured: 0, want: 1},
		{name: "discount", configured: 0.75, want: 0.75},
		{name: "markup", configured: 1.25, want: 1.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := Provider{CostMultiplier: tt.configured}
			if got := provider.EffectiveCostMultiplier(); got != tt.want {
				t.Fatalf("EffectiveCostMultiplier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderRejectsInvalidCostMultiplier(t *testing.T) {
	for _, multiplier := range []float64{-1, 0.001, 100.01, math.Inf(1), math.NaN()} {
		provider := Provider{Name: "priced", CostMultiplier: multiplier}
		errors := provider.ValidateConfiguration()
		if len(errors) == 0 || !strings.Contains(strings.Join(errors, " "), "费用倍率") {
			t.Errorf("multiplier %v should produce a cost multiplier error, got %v", multiplier, errors)
		}
	}
}

func TestProviderAcceptsLegacyAndValidCostMultiplier(t *testing.T) {
	for _, multiplier := range []float64{0, 0.01, 0.75, 1, 100} {
		provider := Provider{Name: "priced", CostMultiplier: multiplier}
		if errors := provider.ValidateConfiguration(); len(errors) != 0 {
			t.Errorf("multiplier %v should be valid, got %v", multiplier, errors)
		}
	}
}
