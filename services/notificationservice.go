package services

import (
	"log"
	"sync"
	"time"
)

// EventEmitter decouples long-running services from the HTTP/SSE transport.
type EventEmitter interface {
	Emit(name string, value any)
}

// NotificationService emits browser events for relay state changes. Native
// desktop notifications intentionally do not exist in the headless server.
type NotificationService struct {
	appSettings          *AppSettingsService
	emitter              EventEmitter
	mu                   sync.Mutex
	lastNotifyTime       time.Time
	minInterval          time.Duration
	pendingSwitch        *SwitchNotification
	pendingTimer         *time.Timer
	lastNotifyByGroup    map[int64]time.Time
	pendingSwitchByGroup map[int64]SwitchNotification
	pendingTimerByGroup  map[int64]*time.Timer
	stopped              bool
}

type SwitchNotification struct {
	FromProvider   string
	ToProvider     string
	Reason         string
	Platform       string
	ModelGroupID   int64
	ModelGroupName string
}

func NewNotificationService(appSettings *AppSettingsService) *NotificationService {
	return &NotificationService{
		appSettings:          appSettings,
		minInterval:          3 * time.Second,
		lastNotifyByGroup:    make(map[int64]time.Time),
		pendingSwitchByGroup: make(map[int64]SwitchNotification),
		pendingTimerByGroup:  make(map[int64]*time.Timer),
	}
}

func (ns *NotificationService) SetEventEmitter(emitter EventEmitter) {
	if emitter == nil {
		return
	}
	ns.mu.Lock()
	ns.emitter = emitter
	ns.mu.Unlock()
}

func (ns *NotificationService) currentEmitter() EventEmitter {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.stopped {
		return nil
	}
	return ns.emitter
}

func (ns *NotificationService) Stop() {
	ns.mu.Lock()
	ns.stopped = true
	ns.pendingSwitch = nil
	if ns.pendingTimer != nil {
		ns.pendingTimer.Stop()
		ns.pendingTimer = nil
	}
	for _, timer := range ns.pendingTimerByGroup {
		timer.Stop()
	}
	ns.pendingSwitchByGroup = make(map[int64]SwitchNotification)
	ns.pendingTimerByGroup = make(map[int64]*time.Timer)
	ns.mu.Unlock()
}

func (ns *NotificationService) isEnabled() bool {
	if ns.appSettings == nil {
		return true
	}
	settings, err := ns.appSettings.GetAppSettings()
	if err != nil {
		return true
	}
	return settings.EnableSwitchNotify
}

func (ns *NotificationService) NotifyProviderSwitch(info SwitchNotification) {
	if requireCodexPlatform(info.Platform) != nil || !ns.isEnabled() {
		return
	}
	info.Platform = CodexPlatform
	if info.ModelGroupID != 0 {
		ns.notifyProviderSwitchForGroup(info)
		return
	}

	ns.mu.Lock()
	if ns.stopped {
		ns.mu.Unlock()
		return
	}
	now := time.Now()
	sinceLast := now.Sub(ns.lastNotifyTime)
	if ns.lastNotifyTime.IsZero() || ns.minInterval <= 0 || sinceLast >= ns.minInterval {
		ns.lastNotifyTime = now
		ns.pendingSwitch = nil
		if ns.pendingTimer != nil {
			ns.pendingTimer.Stop()
			ns.pendingTimer = nil
		}
		ns.mu.Unlock()
		ns.emitProviderSwitch(info)
		return
	}

	pending := info
	ns.pendingSwitch = &pending
	if ns.pendingTimer == nil {
		ns.pendingTimer = time.AfterFunc(ns.minInterval-sinceLast, ns.flushPendingSwitch)
	}
	ns.mu.Unlock()
	log.Printf("[Notification] switch event coalesced after %v", sinceLast)
}

func (ns *NotificationService) notifyProviderSwitchForGroup(info SwitchNotification) {
	ns.mu.Lock()
	if ns.stopped {
		ns.mu.Unlock()
		return
	}
	if ns.lastNotifyByGroup == nil {
		ns.lastNotifyByGroup = make(map[int64]time.Time)
	}
	if ns.pendingSwitchByGroup == nil {
		ns.pendingSwitchByGroup = make(map[int64]SwitchNotification)
	}
	if ns.pendingTimerByGroup == nil {
		ns.pendingTimerByGroup = make(map[int64]*time.Timer)
	}
	now := time.Now()
	last := ns.lastNotifyByGroup[info.ModelGroupID]
	sinceLast := now.Sub(last)
	if last.IsZero() || ns.minInterval <= 0 || sinceLast >= ns.minInterval {
		ns.lastNotifyByGroup[info.ModelGroupID] = now
		delete(ns.pendingSwitchByGroup, info.ModelGroupID)
		if timer := ns.pendingTimerByGroup[info.ModelGroupID]; timer != nil {
			timer.Stop()
			delete(ns.pendingTimerByGroup, info.ModelGroupID)
		}
		ns.mu.Unlock()
		ns.emitProviderSwitch(info)
		return
	}
	ns.pendingSwitchByGroup[info.ModelGroupID] = info
	if ns.pendingTimerByGroup[info.ModelGroupID] == nil {
		groupID := info.ModelGroupID
		ns.pendingTimerByGroup[groupID] = time.AfterFunc(ns.minInterval-sinceLast, func() {
			ns.flushPendingGroupSwitch(groupID)
		})
	}
	ns.mu.Unlock()
}

func (ns *NotificationService) flushPendingGroupSwitch(groupID int64) {
	ns.mu.Lock()
	info, exists := ns.pendingSwitchByGroup[groupID]
	delete(ns.pendingSwitchByGroup, groupID)
	delete(ns.pendingTimerByGroup, groupID)
	if ns.stopped || !exists {
		ns.mu.Unlock()
		return
	}
	ns.lastNotifyByGroup[groupID] = time.Now()
	ns.mu.Unlock()
	if ns.isEnabled() {
		ns.emitProviderSwitch(info)
	}
}

func (ns *NotificationService) flushPendingSwitch() {
	ns.mu.Lock()
	if ns.stopped || ns.pendingSwitch == nil {
		ns.pendingTimer = nil
		ns.mu.Unlock()
		return
	}
	info := *ns.pendingSwitch
	ns.pendingSwitch = nil
	ns.pendingTimer = nil
	ns.lastNotifyTime = time.Now()
	ns.mu.Unlock()

	if ns.isEnabled() {
		ns.emitProviderSwitch(info)
	}
}

func (ns *NotificationService) emitProviderSwitch(info SwitchNotification) {
	if emitter := ns.currentEmitter(); emitter != nil {
		emitter.Emit("provider:switched", map[string]any{
			"platform":       info.Platform,
			"modelGroupId":   info.ModelGroupID,
			"modelGroupName": info.ModelGroupName,
			"fromProvider":   info.FromProvider,
			"toProvider":     info.ToProvider,
			"reason":         info.Reason,
			"timestamp":      time.Now().UnixMilli(),
		})
	}
}

func (ns *NotificationService) NotifyProviderBlacklisted(platform, providerName string, level, durationMinutes int) {
	ns.NotifyGroupProviderBlacklisted(platform, 0, "", providerName, level, durationMinutes)
}

func (ns *NotificationService) NotifyGroupProviderBlacklisted(platform string, modelGroupID int64, modelGroupName, providerName string, level, durationMinutes int) {
	if requireCodexPlatform(platform) != nil || !ns.isEnabled() {
		return
	}
	if emitter := ns.currentEmitter(); emitter != nil {
		emitter.Emit("provider:blacklisted", map[string]any{
			"platform":        CodexPlatform,
			"modelGroupId":    modelGroupID,
			"modelGroupName":  modelGroupName,
			"providerName":    providerName,
			"level":           level,
			"durationMinutes": durationMinutes,
			"timestamp":       time.Now().UnixMilli(),
		})
	}
}

func (ns *NotificationService) NotifyProviderRecovered(platform, providerName, reason string) {
	ns.NotifyGroupProviderRecovered(platform, 0, "", providerName, reason)
}

func (ns *NotificationService) NotifyGroupProviderRecovered(platform string, modelGroupID int64, modelGroupName, providerName, reason string) {
	if requireCodexPlatform(platform) != nil || !ns.isEnabled() {
		return
	}
	if emitter := ns.currentEmitter(); emitter != nil {
		emitter.Emit("provider:recovered", map[string]any{
			"platform":       CodexPlatform,
			"modelGroupId":   modelGroupID,
			"modelGroupName": modelGroupName,
			"providerName":   providerName,
			"reason":         reason,
			"timestamp":      time.Now().UnixMilli(),
		})
	}
}
