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
	appSettings    *AppSettingsService
	emitter        EventEmitter
	mu             sync.Mutex
	lastNotifyTime time.Time
	minInterval    time.Duration
	pendingSwitch  *SwitchNotification
	pendingTimer   *time.Timer
	stopped        bool
}

type SwitchNotification struct {
	FromProvider string
	ToProvider   string
	Reason       string
	Platform     string
}

func NewNotificationService(appSettings *AppSettingsService) *NotificationService {
	return &NotificationService{
		appSettings: appSettings,
		minInterval: 3 * time.Second,
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
			"platform":     info.Platform,
			"fromProvider": info.FromProvider,
			"toProvider":   info.ToProvider,
			"reason":       info.Reason,
			"timestamp":    time.Now().UnixMilli(),
		})
	}
}

func (ns *NotificationService) NotifyProviderBlacklisted(platform, providerName string, level, durationMinutes int) {
	if requireCodexPlatform(platform) != nil || !ns.isEnabled() {
		return
	}
	if emitter := ns.currentEmitter(); emitter != nil {
		emitter.Emit("provider:blacklisted", map[string]any{
			"platform":        CodexPlatform,
			"providerName":    providerName,
			"level":           level,
			"durationMinutes": durationMinutes,
			"timestamp":       time.Now().UnixMilli(),
		})
	}
}

func (ns *NotificationService) NotifyProviderRecovered(platform, providerName, reason string) {
	if requireCodexPlatform(platform) != nil || !ns.isEnabled() {
		return
	}
	if emitter := ns.currentEmitter(); emitter != nil {
		emitter.Emit("provider:recovered", map[string]any{
			"platform":     CodexPlatform,
			"providerName": providerName,
			"reason":       reason,
			"timestamp":    time.Now().UnixMilli(),
		})
	}
}
