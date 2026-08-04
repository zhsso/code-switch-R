package services

import "testing"

func TestValidateBlacklistSettingsAcceptsManualDuration(t *testing.T) {
	for _, duration := range []int{1, 5, 37, 60, 10080} {
		if err := validateBlacklistSettings(3, duration); err != nil {
			t.Errorf("duration %d should be valid: %v", duration, err)
		}
	}
}

func TestValidateBlacklistSettingsRejectsDurationOutsideRange(t *testing.T) {
	for _, duration := range []int{-1, 0, 10081} {
		if err := validateBlacklistSettings(3, duration); err == nil {
			t.Errorf("duration %d should be rejected", duration)
		}
	}
}
