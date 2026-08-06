package services

import (
	"database/sql"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
)

func TestHealthCheckAutoUnblockRequiresTwoConsecutiveSuccesses(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get database: %v", err)
	}
	until := time.Now().Add(time.Hour)
	if _, err := db.Exec(`
		INSERT INTO provider_blacklist (
			platform, provider_name, failure_count, blacklisted_at,
			blacklisted_until, blacklist_level, last_failure_window_start, auto_recovered
		) VALUES (?, ?, 4, ?, ?, 3, ?, 0)
	`, CodexPlatform, "recover-me", time.Now(), until, time.Now()); err != nil {
		t.Fatalf("seed active blacklist: %v", err)
	}

	emitter := &recordingEventEmitter{}
	notifications := NewNotificationService(nil)
	notifications.SetEventEmitter(emitter)
	blacklist := NewBlacklistService(NewSettingsService(), notifications)
	health := NewHealthCheckService(NewProviderService(), blacklist, NewSettingsService(), nil)
	provider := &Provider{
		Name:                    "recover-me",
		AvailabilityAutoUnblock: true,
	}
	result := func(status string) *HealthCheckResult {
		return &HealthCheckResult{
			Platform: CodexPlatform, ProviderName: provider.Name, Status: status, CheckedAt: time.Now(),
		}
	}

	health.handleBlacklistIntegration(provider, result(HealthStatusOperational))
	if blacklisted, _ := blacklist.IsBlacklisted(CodexPlatform, provider.Name); !blacklisted {
		t.Fatal("one successful check must not unblock")
	}

	// A validation failure breaks the streak, so the next success starts at one.
	health.handleBlacklistIntegration(provider, result(HealthStatusValidationError))
	health.handleBlacklistIntegration(provider, result(HealthStatusOperational))
	if blacklisted, _ := blacklist.IsBlacklisted(CodexPlatform, provider.Name); !blacklisted {
		t.Fatal("success streak must reset after validation failure")
	}

	// Degraded is still a successful response and completes the second success.
	health.handleBlacklistIntegration(provider, result(HealthStatusDegraded))
	if blacklisted, _ := blacklist.IsBlacklisted(CodexPlatform, provider.Name); blacklisted {
		t.Fatal("two consecutive successful checks should auto-unblock")
	}

	var failureCount, level, autoRecovered int
	var blacklistedUntil, lastFailureWindowStart, lastRecoveredAt sql.NullTime
	if err := db.QueryRow(`
		SELECT failure_count, blacklist_level, auto_recovered, blacklisted_until,
			last_failure_window_start, last_recovered_at
		FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, CodexPlatform, provider.Name).Scan(
		&failureCount, &level, &autoRecovered, &blacklistedUntil,
		&lastFailureWindowStart, &lastRecoveredAt,
	); err != nil {
		t.Fatalf("read recovered blacklist state: %v", err)
	}
	if failureCount != 0 || level != 3 || autoRecovered != 1 {
		t.Fatalf("unexpected recovered state: failures=%d level=%d auto=%d", failureCount, level, autoRecovered)
	}
	if blacklistedUntil.Valid || lastFailureWindowStart.Valid || !lastRecoveredAt.Valid {
		t.Fatalf("recovery timestamps not reset correctly: until=%v failureWindow=%v recoveredAt=%v",
			blacklistedUntil, lastFailureWindowStart, lastRecoveredAt)
	}
	if got := emitter.count("provider:recovered"); got != 1 {
		t.Fatalf("provider:recovered events=%d want 1", got)
	}
}

func TestHealthCheckAutoUnblockDoesNotTouchExpiredBlacklist(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	db, _ := xdb.DB("default")
	if _, err := db.Exec(`
		INSERT INTO provider_blacklist (platform, provider_name, blacklisted_at, blacklisted_until, blacklist_level)
		VALUES (?, ?, ?, ?, 2)
	`, CodexPlatform, "already-expired", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("seed expired blacklist: %v", err)
	}

	blacklist := NewBlacklistService(NewSettingsService(), nil)
	recovered, err := blacklist.AutoUnblockOnAvailabilitySuccess(CodexPlatform, "already-expired")
	if err != nil {
		t.Fatalf("auto unblock expired record: %v", err)
	}
	if recovered {
		t.Fatal("expired record must not be reported as an early auto-unblock")
	}
}
