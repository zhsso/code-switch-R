package services

import "testing"

func TestBlacklistStateIsIndependentBetweenModelGroups(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	setAppSetting(t, "blacklist_level_enabled", "false")
	setAppSetting(t, "blacklist_failure_threshold", "1")

	blacklist := NewBlacklistService(NewSettingsService(), nil)
	const providerName = "shared-provider"

	if err := blacklist.RecordGroupFailureWithReason(CodexPlatform, 11, "fast", providerName, "group 11 failed"); err != nil {
		t.Fatalf("blacklist group 11: %v", err)
	}
	if blocked, _ := blacklist.IsGroupBlacklisted(CodexPlatform, 11, providerName); !blocked {
		t.Fatal("provider should be blacklisted in group 11")
	}
	if blocked, _ := blacklist.IsGroupBlacklisted(CodexPlatform, 22, providerName); blocked {
		t.Fatal("group 11 blacklist leaked into group 22")
	}
	if blocked, _ := blacklist.IsBlacklisted(CodexPlatform, providerName); blocked {
		t.Fatal("group 11 blacklist leaked into the legacy ungrouped scope")
	}

	statuses, err := blacklist.GetGroupBlacklistStatus(CodexPlatform, 11)
	if err != nil {
		t.Fatalf("get group 11 status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].ModelGroupID != 11 || statuses[0].ModelGroupName != "fast" || statuses[0].ProviderName != providerName {
		t.Fatalf("unexpected group 11 status: %+v", statuses)
	}

	if err := blacklist.RecordGroupFailureWithReason(CodexPlatform, 22, "fallback", providerName, "group 22 failed"); err != nil {
		t.Fatalf("blacklist group 22: %v", err)
	}
	if err := blacklist.ManualUnblockGroupAndReset(CodexPlatform, 11, providerName); err != nil {
		t.Fatalf("unblock group 11: %v", err)
	}
	if blocked, _ := blacklist.IsGroupBlacklisted(CodexPlatform, 11, providerName); blocked {
		t.Fatal("group 11 should be unblocked")
	}
	if blocked, _ := blacklist.IsGroupBlacklisted(CodexPlatform, 22, providerName); !blocked {
		t.Fatal("unblocking group 11 must not unblock group 22")
	}
}
