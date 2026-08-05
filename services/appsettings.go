package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const (
	appSettingsFile = "app.json"

	defaultHistoryRetentionDays = 30
	maxHistoryRetentionDays     = 3650
	defaultAppTimezone          = "Asia/Shanghai"
)

// AppSettings contains only settings that are meaningful to the headless
// relay and its browser UI. Desktop startup, updater and CLI-file settings are
// deliberately absent.
type AppSettings struct {
	ShowHeatmap          bool   `json:"show_heatmap"`
	ShowHomeTitle        bool   `json:"show_home_title"`
	AutoSyncModels       bool   `json:"auto_sync_models"`
	AutoConnectivityTest bool   `json:"auto_connectivity_test"`
	EnableSwitchNotify   bool   `json:"enable_switch_notify"`
	EnableRoundRobin     bool   `json:"enable_round_robin"`
	HistoryRetentionDays int    `json:"history_retention_days"`
	Timezone             string `json:"timezone"`
}

type AppSettingsService struct {
	path string
	mu   sync.Mutex
}

func NewAppSettingsService() (*AppSettingsService, error) {
	dir, err := getUserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve application settings directory: %w", err)
	}
	return &AppSettingsService{path: filepath.Join(dir, appSettingsFile)}, nil
}

func (as *AppSettingsService) defaultSettings() AppSettings {
	return AppSettings{
		ShowHeatmap:          true,
		ShowHomeTitle:        true,
		AutoSyncModels:       true,
		AutoConnectivityTest: true,
		EnableSwitchNotify:   true,
		EnableRoundRobin:     false,
		HistoryRetentionDays: defaultHistoryRetentionDays,
		Timezone:             defaultAppTimezone,
	}
}

func (as *AppSettingsService) GetAppSettings() (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.loadLocked()
}

func (as *AppSettingsService) SaveAppSettings(settings AppSettings) (AppSettings, error) {
	if strings.TrimSpace(settings.Timezone) == "" {
		settings.Timezone = defaultAppTimezone
	} else {
		settings.Timezone = strings.TrimSpace(settings.Timezone)
	}
	if err := validateAppSettings(settings); err != nil {
		return settings, err
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if err := as.saveLocked(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func validateAppSettings(settings AppSettings) error {
	if settings.HistoryRetentionDays < 1 || settings.HistoryRetentionDays > maxHistoryRetentionDays {
		return fmt.Errorf("history retention must be between 1 and %d days", maxHistoryRetentionDays)
	}
	timezone := strings.TrimSpace(settings.Timezone)
	if timezone == "" {
		return fmt.Errorf("timezone must not be empty")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}

func (as *AppSettingsService) mutateAppSettings(mutate func(*AppSettings)) (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	settings, err := as.loadLocked()
	if err != nil {
		return settings, err
	}
	mutate(&settings)
	if err := validateAppSettings(settings); err != nil {
		return settings, err
	}
	if err := as.saveLocked(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (as *AppSettingsService) loadLocked() (AppSettings, error) {
	settings := as.defaultSettings()
	data, err := os.ReadFile(as.path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	if settings.HistoryRetentionDays == 0 {
		settings.HistoryRetentionDays = defaultHistoryRetentionDays
	}
	if strings.TrimSpace(settings.Timezone) == "" {
		settings.Timezone = defaultAppTimezone
	}
	settings.Timezone = strings.TrimSpace(settings.Timezone)
	return settings, validateAppSettings(settings)
}

func (as *AppSettingsService) saveLocked(settings AppSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(as.path, data, 0o600)
}
