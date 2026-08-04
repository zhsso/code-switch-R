package services

import "fmt"

type HistoryCleanupResult struct {
	RetentionDays int   `json:"retention_days"`
	RequestLogs   int64 `json:"request_logs"`
	HealthChecks  int64 `json:"health_checks"`
}

type MaintenanceService struct {
	settings *AppSettingsService
	logs     *LogService
	health   *HealthCheckService
}

func NewMaintenanceService(
	settings *AppSettingsService,
	logs *LogService,
	health *HealthCheckService,
) *MaintenanceService {
	return &MaintenanceService{settings: settings, logs: logs, health: health}
}

// CleanupConfiguredHistory applies the currently persisted retention window to
// both request/cost history and provider health history.
func (s *MaintenanceService) CleanupConfiguredHistory() (HistoryCleanupResult, error) {
	settings, err := s.settings.GetAppSettings()
	if err != nil {
		return HistoryCleanupResult{}, fmt.Errorf("load retention setting: %w", err)
	}
	result := HistoryCleanupResult{RetentionDays: settings.HistoryRetentionDays}
	result.RequestLogs, err = s.logs.CleanupOldRecords(settings.HistoryRetentionDays)
	if err != nil {
		return result, err
	}
	result.HealthChecks, err = s.health.CleanupOldRecords(settings.HistoryRetentionDays)
	if err != nil {
		return result, err
	}
	return result, nil
}
