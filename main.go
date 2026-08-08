package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codeswitch/services"
)

func main() {
	configureLibraryLogging()
	config, err := loadServerConfig()
	if err != nil {
		log.Fatalf("invalid server configuration: %v", err)
	}

	// Volume migration must finish before xdb or its write queues open the file.
	migration, err := migrateVolumeData()
	if err != nil {
		log.Fatalf("migrate data volume: %v", err)
	}
	if migration.Migrated {
		log.Printf("migrated legacy data to %s (backup: %s)", migration.Database, migration.Backup)
	}

	// Database initialization must remain ahead of every service that uses xdb.
	if err := services.InitDatabase(); err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	if err := services.InitGlobalDBQueue(); err != nil {
		log.Fatalf("initialize database queues: %v", err)
	}

	providerService := services.NewProviderService()
	providerRPC := newProviderRPCService(providerService)
	settingsService := services.NewSettingsService()
	appSettings, err := services.NewAppSettingsService()
	if err != nil {
		shutdownDatabaseQueues()
		log.Fatalf("initialize application settings: %v", err)
	}
	events := newEventHub()
	notificationService := services.NewNotificationService(appSettings)
	notificationService.SetEventEmitter(events)
	defaultModelPolicy := services.NewDefaultModelPolicy()
	modelSyncService, err := services.NewModelSyncService(appSettings, defaultModelPolicy)
	if err != nil {
		notificationService.Stop()
		shutdownDatabaseQueues()
		log.Fatalf("initialize model sync: %v", err)
	}
	modelSyncService.SetEventEmitter(events)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	requestEventService := services.NewRequestEventService()
	providerRelay := services.NewProviderRelayService(
		providerService,
		blacklistService,
		notificationService,
		appSettings,
		config.RelayAddr(),
		requestEventService,
	)
	logService := services.NewLogService(providerService)
	dailyLimitService := services.NewDailyCostLimitService(providerService, appSettings, logService)
	dailyLimitService.SetBlacklistService(blacklistService)
	blacklistService.SetBlacklistObserver(dailyLimitService.OnProviderBlacklisted)
	providerRelay.SetDailyCostLimitService(dailyLimitService)
	speedTestService := services.NewSpeedTestService()
	connectivityTestService := services.NewConnectivityTestService(
		providerService,
		blacklistService,
		settingsService,
		defaultModelPolicy,
	)
	connectivityTestService.SetDailyCostLimitService(dailyLimitService)
	healthCheckService := services.NewHealthCheckService(
		providerService,
		blacklistService,
		settingsService,
		defaultModelPolicy,
	)
	healthCheckService.SetDailyCostLimitService(dailyLimitService)
	if err := healthCheckService.Start(); err != nil {
		shutdownDatabaseQueues()
		log.Fatalf("initialize health checks: %v", err)
	}
	maintenanceService := services.NewMaintenanceService(appSettings, logService, healthCheckService)

	registry := newRPCRegistry()
	registerWebServices(registry, webServices{
		providers:    providerRPC,
		settings:     settingsService,
		appSettings:  appSettings,
		blacklist:    blacklistService,
		dailyLimits:  dailyLimitService,
		logs:         logService,
		modelSync:    modelSyncService,
		speedTest:    speedTestService,
		connectivity: connectivityTestService,
		health:       healthCheckService,
		relay:        providerRelay,
		maintenance:  maintenanceService,
	})

	webServer := newWebServer(config.WebAddr(), frontendAssets(), registry, events, providerRelay)
	if err := webServer.Start(); err != nil {
		_ = healthCheckService.Stop()
		shutdownDatabaseQueues()
		log.Fatalf("start WebUI: %v", err)
	}

	if err := providerRelay.Start(); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = webServer.Stop(shutdownCtx)
		cancel()
		_ = healthCheckService.Stop()
		shutdownDatabaseQueues()
		log.Fatalf("start Codex relay on %s: %v", config.RelayAddr(), err)
	}
	modelSyncService.Start()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	backgroundDone := startBackgroundServices(
		ctx,
		appSettings,
		blacklistService,
		healthCheckService,
		maintenanceService,
	)

	log.Printf("WebUI: http://%s", config.WebAddr())
	log.Printf("Codex relay: http://%s/responses", config.RelayAddr())
	<-ctx.Done()
	log.Printf("shutdown requested: %v", ctx.Err())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer shutdownCancel()
	notificationService.Stop()
	if err := webServer.Stop(shutdownCtx); err != nil {
		log.Printf("stop WebUI: %v", err)
	}
	if err := healthCheckService.Stop(); err != nil {
		log.Printf("stop health checks: %v", err)
	}
	if err := connectivityTestService.Stop(); err != nil {
		log.Printf("stop connectivity checks: %v", err)
	}
	modelSyncService.Stop()
	if err := providerRelay.Stop(); err != nil {
		log.Printf("stop relay: %v", err)
	}
	<-backgroundDone

	shutdownDatabaseQueues()
}

func startBackgroundServices(
	ctx context.Context,
	appSettings *services.AppSettingsService,
	blacklist *services.BlacklistService,
	health *services.HealthCheckService,
	maintenance *services.MaintenanceService,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		if _, err := maintenance.CleanupConfiguredHistory(); err != nil {
			log.Printf("initial history cleanup: %v", err)
		}

		settings, err := appSettings.GetAppSettings()
		if err != nil {
			log.Printf("load application settings: %v", err)
		} else if settings.AutoConnectivityTest {
			health.SetAutoAvailabilityPolling(true)
		}

		blacklistTicker := time.NewTicker(time.Minute)
		cleanupTicker := time.NewTicker(24 * time.Hour)
		defer blacklistTicker.Stop()
		defer cleanupTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-blacklistTicker.C:
				if err := blacklist.AutoRecoverExpired(); err != nil {
					log.Printf("recover expired blacklist entries: %v", err)
				}
			case <-cleanupTicker.C:
				if result, err := maintenance.CleanupConfiguredHistory(); err != nil {
					log.Printf("scheduled history cleanup: %v", err)
				} else {
					log.Printf("history cleanup removed %d request logs and %d health checks",
						result.RequestLogs, result.HealthChecks)
				}
			}
		}
	}()
	return done
}

func shutdownDatabaseQueues() {
	if err := services.ShutdownGlobalDBQueue(10 * time.Second); err != nil {
		log.Printf("shutdown database queues: %v", err)
	}
}

func registerWebServices(registry *rpcRegistry, svc webServices) {
	registry.Register("codeswitch/services.ProviderService", svc.providers,
		"LoadProviders", "SaveProviders", "SaveModelGroups", "RevealProviderAPIKey", "DuplicateProvider", "RenameProvider")
	registry.Register("codeswitch/services.SettingsService", svc.settings,
		"GetBlacklistSettingsStruct", "UpdateBlacklistSettings", "GetLevelBlacklistEnabled",
		"SetLevelBlacklistEnabled", "IsBlacklistEnabled", "UpdateBlacklistEnabled",
		"GetBlacklistLevelConfig", "UpdateBlacklistLevelConfig")
	registry.Register("codeswitch/services.AppSettingsService", svc.appSettings,
		"GetAppSettings", "SaveAppSettings")
	registry.Register("codeswitch/services.BlacklistService", svc.blacklist,
		"GetBlacklistStatus", "ManualUnblock", "ManualUnblockAndReset", "ManualResetLevel")
	registry.Register("codeswitch/services.DailyCostLimitService", svc.dailyLimits,
		"GetStatuses", "SetActualUsage", "ManualBlock", "TemporaryUnblock")
	registry.Register("codeswitch/services.LogService", svc.logs,
		"CostSince", "ListRequestLogs", "ListRequestLogsFiltered", "GetRequestLogDetail", "ListProviders", "ListModelGroups",
		"HeatmapStats", "StatsSince", "ProviderDailyStats", "ListRequestEvents", "ListRequestEventsFiltered")
	registry.Register("codeswitch/services.ModelSyncService", svc.modelSync,
		"SyncNow", "GetSyncStatus", "GetDefaultModels", "RestoreBuiltinPricing")
	registry.Register("codeswitch/services.SpeedTestService", svc.speedTest, "TestEndpoints")
	registry.Register("codeswitch/services.ConnectivityTestService", svc.connectivity,
		"TestAll", "GetResults", "GetAllResults", "RunSingleTest", "SetAutoTestEnabled",
		"GetAutoTestEnabled", "TestProviderManual")
	registry.Register("codeswitch/services.HealthCheckService", svc.health,
		"GetLatestResults", "GetHistory", "RunSingleCheck", "RunAllChecks",
		"StartBackgroundPolling", "StopBackgroundPolling", "IsPollingRunning", "SetAutoAvailabilityPolling",
		"SetAvailabilityMonitorEnabled", "SetConnectivityAutoBlacklist", "SaveAvailabilityConfig",
		"CleanupOldRecords")
	registry.Register("codeswitch/services.ProviderRelayService", svc.relay,
		"GetLastUsedProvider", "GetAllLastUsedProviders", "GetLastUsedProvidersByGroup", "GetRequestCapture", "SetRequestCapture",
		"ListCaptureSessions", "GetCaptureTotalBytes", "GetCaptureSessionLogs",
		"DeleteCaptureSession", "ClearCapturedRequests")
	registry.Register("codeswitch/services.MaintenanceService", svc.maintenance,
		"CleanupConfiguredHistory")
}

type webServices struct {
	providers    *providerRPCService
	settings     *services.SettingsService
	appSettings  *services.AppSettingsService
	blacklist    *services.BlacklistService
	dailyLimits  *services.DailyCostLimitService
	logs         *services.LogService
	modelSync    *services.ModelSyncService
	speedTest    *services.SpeedTestService
	connectivity *services.ConnectivityTestService
	health       *services.HealthCheckService
	relay        *services.ProviderRelayService
	maintenance  *services.MaintenanceService
}
