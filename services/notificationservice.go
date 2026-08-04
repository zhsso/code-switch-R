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
	mu             sync.RWMutex
	lastNotifyTime time.Time
	minInterval    time.Duration
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
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.emitter
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
	sinceLast := time.Since(ns.lastNotifyTime)
	if sinceLast < ns.minInterval {
		ns.mu.Unlock()
		log.Printf("[Notification] switch event throttled after %v", sinceLast)
		return
	}
	ns.lastNotifyTime = time.Now()
	ns.mu.Unlock()

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
