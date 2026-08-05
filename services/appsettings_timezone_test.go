package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppSettingsTimezoneDefaultsAndValidates(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)
	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, appSettingsFile),
		[]byte(`{"show_heatmap":true,"history_retention_days":30}`),
		0o600,
	); err != nil {
		t.Fatalf("write legacy app settings: %v", err)
	}

	service, err := NewAppSettingsService()
	if err != nil {
		t.Fatalf("create app settings service: %v", err)
	}
	settings, err := service.GetAppSettings()
	if err != nil {
		t.Fatalf("load app settings: %v", err)
	}
	if settings.Timezone != defaultAppTimezone {
		t.Fatalf("legacy settings timezone = %q, want %q", settings.Timezone, defaultAppTimezone)
	}

	settings.Timezone = "America/New_York"
	if _, err := service.SaveAppSettings(settings); err != nil {
		t.Fatalf("save valid IANA timezone: %v", err)
	}
	settings.Timezone = "UTC+08:00"
	if _, err := service.SaveAppSettings(settings); err == nil {
		t.Fatal("fixed offset pseudo-timezone should be rejected")
	}
}
