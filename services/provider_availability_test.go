package services

import (
	"strings"
	"testing"
	"time"
)

func TestProviderAvailabilityPollIntervalDefaultsAndValidation(t *testing.T) {
	if got := (Provider{}).EffectiveAvailabilityPollIntervalSeconds(); got != DefaultAvailabilityPollIntervalSeconds {
		t.Fatalf("legacy provider interval=%d want %d", got, DefaultAvailabilityPollIntervalSeconds)
	}

	for _, interval := range []int{MinAvailabilityPollIntervalSeconds, 60, MaxAvailabilityPollIntervalSeconds} {
		provider := Provider{AvailabilityConfig: &AvailabilityConfig{PollIntervalSeconds: interval}}
		if errors := provider.ValidateConfiguration(); len(errors) != 0 {
			t.Fatalf("valid interval %d rejected: %v", interval, errors)
		}
		if got := provider.EffectiveAvailabilityPollIntervalSeconds(); got != interval {
			t.Fatalf("effective interval=%d want %d", got, interval)
		}
	}

	for _, interval := range []int{1, MinAvailabilityPollIntervalSeconds - 1, MaxAvailabilityPollIntervalSeconds + 1} {
		provider := Provider{AvailabilityConfig: &AvailabilityConfig{PollIntervalSeconds: interval}}
		errors := provider.ValidateConfiguration()
		if len(errors) == 0 || !strings.Contains(strings.Join(errors, " "), "可用性检测间隔") {
			t.Fatalf("invalid interval %d was not rejected: %v", interval, errors)
		}
		if got := provider.EffectiveAvailabilityPollIntervalSeconds(); got != DefaultAvailabilityPollIntervalSeconds {
			t.Fatalf("invalid interval fallback=%d want %d", got, DefaultAvailabilityPollIntervalSeconds)
		}
	}
}

func TestAvailabilityCheckStatePreventsOverlapAndRunsAfterConfigChange(t *testing.T) {
	health := NewHealthCheckService(NewProviderService(), nil, nil, nil)
	key := availabilityCheckKey(CodexPlatform, "42")
	now := time.Now()

	if !health.beginScheduledCheck(key, time.Minute, now) {
		t.Fatal("first scheduled check should start immediately")
	}
	if health.beginManualCheck(key, time.Minute) {
		t.Fatal("manual check must not overlap an active scheduled check")
	}
	if health.beginScheduledCheck(key, 15*time.Second, now) {
		t.Fatal("interval update must not overlap the active check")
	}

	health.finishProviderCheck(key)
	if !health.beginScheduledCheck(key, 15*time.Second, time.Now()) {
		t.Fatal("interval change during a check should schedule one immediate follow-up")
	}
	if health.beginScheduledCheck(key, 15*time.Second, time.Now().Add(time.Hour)) {
		t.Fatal("same provider must remain single-flight")
	}
	health.finishProviderCheck(key)

	health.mu.RLock()
	state := *health.checkStates[key]
	health.mu.RUnlock()
	remaining := time.Until(state.NextDue)
	if remaining < 14*time.Second || remaining > 16*time.Second {
		t.Fatalf("next check should be based on completion: remaining=%s", remaining)
	}
}

func TestAvailabilitySchedulerOnlyScansWhenConfigChangesOrCheckIsDue(t *testing.T) {
	providerService := NewProviderService()
	health := NewHealthCheckService(providerService, nil, nil, nil)
	now := time.Now()
	if !health.shouldScanProviderSchedules(now) {
		t.Fatal("scheduler must load configuration on its first pass")
	}

	health.markScheduleConfigLoaded(providerService.configGeneration(), now)
	if health.shouldScanProviderSchedules(now.Add(time.Second)) {
		t.Fatal("scheduler should not reload an unchanged config before a check is due")
	}

	health.scheduleProviderCheckNow(CodexPlatform, "7")
	if !health.shouldScanProviderSchedules(time.Now()) {
		t.Fatal("an explicitly scheduled check should wake the scheduler")
	}

	health.removeProviderCheckSchedule(CodexPlatform, "7")
	health.markScheduleConfigLoaded(providerService.configGeneration(), time.Now())
	providerService.configGen.Add(1)
	if !health.shouldScanProviderSchedules(time.Now()) {
		t.Fatal("provider config generation changes should wake the scheduler")
	}
}
