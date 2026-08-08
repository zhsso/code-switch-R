<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ListRow from '../Setting/ListRow.vue'
import LanguageSwitcher from '../Setting/LanguageSwitcher.vue'
import ThemeSetting from '../Setting/ThemeSetting.vue'
import { Call, Events } from '../../runtime'
import { fetchAppSettings, saveAppSettings, type AppSettings } from '../../services/appSettings'
import {
  getBlacklistEnabled,
  getBlacklistSettings,
  getLevelBlacklistEnabled,
  setBlacklistEnabled,
  setLevelBlacklistEnabled,
  updateBlacklistSettings,
} from '../../services/settings'
import {
  GetSyncStatus,
  RestoreBuiltinPricing,
  SyncNow,
  type ModelSyncStatus,
} from '../../services/modelSync'
import { cleanupConfiguredHistory, type HistoryCleanupResult } from '../../services/maintenance'
import { extractErrorMessage } from '../../utils/error'

const { t, locale } = useI18n()

const settings = reactive<AppSettings>({
  show_heatmap: true,
  show_home_title: true,
  auto_sync_models: true,
  auto_connectivity_test: true,
  enable_switch_notify: true,
  history_retention_days: 30,
  timezone: 'Asia/Shanghai',
})

const supportedTimezones = (() => {
  try {
    return Intl.supportedValuesOf('timeZone')
  } catch {
    return [] as string[]
  }
})()
const timezoneOptions = computed(() => Array.from(new Set([
  settings.timezone,
  'Asia/Shanghai',
  'Asia/Hong_Kong',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'America/New_York',
  'America/Los_Angeles',
  'UTC',
  ...supportedTimezones,
])).filter(Boolean))

const blacklistEnabled = ref(false)
const levelBlacklistEnabled = ref(false)
const blacklistThreshold = ref(3)
const blacklistDuration = ref(30)
const modelSync = ref<ModelSyncStatus | null>(null)
const loading = ref(true)
const saving = ref(false)
const syncing = ref(false)
const cleaning = ref(false)
const notice = ref<{ kind: 'success' | 'error'; text: string } | null>(null)

const pricingCount = computed(() => {
  const pricing = modelSync.value?.pricing
  return Number(pricing?.totalModels ?? pricing?.TotalModels ?? 0)
})

const lastSync = computed(() => {
  const value = modelSync.value?.lastSuccess
  if (!value || value.startsWith('0001-')) return t('components.general.label.modelSyncNever')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale.value, { hour12: false })
})

function showNotice(kind: 'success' | 'error', text: string) {
  notice.value = { kind, text }
}

async function load() {
  loading.value = true
  try {
    const [saved, blacklist, enabled, levelEnabled, syncStatus] = await Promise.all([
      fetchAppSettings(),
      getBlacklistSettings(),
      getBlacklistEnabled(),
      getLevelBlacklistEnabled(),
      GetSyncStatus(),
    ])
    Object.assign(settings, saved)
    blacklistThreshold.value = blacklist.failureThreshold
    blacklistDuration.value = blacklist.durationMinutes
    blacklistEnabled.value = enabled
    levelBlacklistEnabled.value = levelEnabled
    modelSync.value = syncStatus
  } catch (error) {
    showNotice('error', t('components.general.label.settingsLoadFailed') + ': ' + extractErrorMessage(error))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (saving.value || loading.value) return false
  saving.value = true
  notice.value = null
  settings.history_retention_days = Math.min(3650, Math.max(1, Math.floor(settings.history_retention_days || 30)))
  blacklistThreshold.value = Math.min(10, Math.max(1, Math.floor(blacklistThreshold.value || 3)))
  blacklistDuration.value = Math.min(10080, Math.max(1, Math.floor(blacklistDuration.value || 30)))
  try {
    const saved = await saveAppSettings({ ...settings })
    Object.assign(settings, saved)
    await Promise.all([
      setBlacklistEnabled(blacklistEnabled.value),
      setLevelBlacklistEnabled(levelBlacklistEnabled.value),
      updateBlacklistSettings(blacklistThreshold.value, blacklistDuration.value),
      Call.ByName(
        'codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling',
        settings.auto_connectivity_test,
      ),
    ])
    window.dispatchEvent(new CustomEvent('app-settings-updated'))
    showNotice('success', t('components.general.label.saved'))
    return true
  } catch (error) {
    showNotice('error', t('components.general.label.settingsSaveFailed') + extractErrorMessage(error))
    return false
  } finally {
    saving.value = false
  }
}

async function syncModels() {
  if (syncing.value) return
  syncing.value = true
  try {
    modelSync.value = await SyncNow()
  } catch (error) {
    showNotice('error', t('components.general.label.modelSyncFailed') + extractErrorMessage(error))
  } finally {
    syncing.value = false
  }
}

async function restorePricing() {
  if (!window.confirm(t('components.general.label.modelRestoreConfirm'))) return
  syncing.value = true
  try {
    modelSync.value = await RestoreBuiltinPricing()
    settings.auto_sync_models = false
    showNotice('success', t('components.general.label.modelRestored'))
  } catch (error) {
    showNotice('error', t('components.general.label.modelRestoreFailed') + extractErrorMessage(error))
  } finally {
    syncing.value = false
  }
}

async function cleanHistory() {
  if (cleaning.value) return
  if (!(await save())) return
  cleaning.value = true
  try {
    const result: HistoryCleanupResult = await cleanupConfiguredHistory()
    showNotice('success', t('components.general.history.cleaned', {
      requests: result.request_logs,
      health: result.health_checks,
    }))
  } catch (error) {
    showNotice('error', t('components.general.history.cleanupFailed') + ': ' + extractErrorMessage(error))
  } finally {
    cleaning.value = false
  }
}

let stopSyncEvent: (() => void) | undefined
let stopResyncEvent: (() => void) | undefined
onMounted(async () => {
  await load()
  stopSyncEvent = Events.On('model-sync:updated', async () => {
    modelSync.value = await GetSyncStatus()
  })
  stopResyncEvent = Events.On('system:resync', async () => {
    modelSync.value = await GetSyncStatus()
  })
})
onUnmounted(() => {
  stopSyncEvent?.()
  stopResyncEvent?.()
})
</script>

<template>
  <div class="main-shell settings-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <h1 class="app-page-title">{{ t('sidebar.settings') }}</h1>
      </div>
      <div class="app-page-actions">
        <button class="primary-action" :disabled="saving || loading" @click="save">
          {{ saving ? t('components.general.label.saving') : t('components.general.label.save') }}
        </button>
      </div>
    </header>

    <div class="app-page-container settings-page" :aria-busy="loading">
      <p v-if="notice" :class="['settings-notice', notice.kind]">{{ notice.text }}</p>

      <section class="settings-section">
        <h2>{{ t('components.general.title.exterior') }}</h2>
        <ListRow :label="t('components.general.label.language')"><LanguageSwitcher /></ListRow>
        <ListRow :label="t('components.general.label.theme')"><ThemeSetting /></ListRow>
        <ListRow :label="t('components.general.label.heatmap')">
          <label class="mac-switch"><input v-model="settings.show_heatmap" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.homeTitle')">
          <label class="mac-switch"><input v-model="settings.show_home_title" type="checkbox"><span /></label>
        </ListRow>
      </section>

      <section class="settings-section">
        <h2>{{ t('components.general.title.relay') }}</h2>
        <ListRow :label="t('components.general.label.autoConnectivityTest')" :sub-label="t('components.general.label.autoConnectivityTestHint')">
          <label class="mac-switch"><input v-model="settings.auto_connectivity_test" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.switchNotify')" :sub-label="t('components.general.label.switchNotifyHintWeb')">
          <label class="mac-switch"><input v-model="settings.enable_switch_notify" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.timezone')" :sub-label="t('components.general.label.timezoneHint')">
          <select v-model="settings.timezone" class="timezone-select">
            <option v-for="timezone in timezoneOptions" :key="timezone" :value="timezone">
              {{ timezone }}
            </option>
          </select>
        </ListRow>
      </section>

      <section class="settings-section">
        <h2>{{ t('components.general.title.blacklist') }}</h2>
        <ListRow :label="t('components.general.label.enableBlacklist')" :sub-label="t('components.general.label.enableBlacklistHint')">
          <label class="mac-switch"><input v-model="blacklistEnabled" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.enableLevelBlacklist')" :sub-label="t('components.general.label.enableLevelBlacklistHint')">
          <label class="mac-switch"><input v-model="levelBlacklistEnabled" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.blacklistThreshold')">
          <div class="number-control"><input v-model.number="blacklistThreshold" type="number" min="1" max="10"><span>{{ t('components.general.label.times') }}</span></div>
        </ListRow>
        <ListRow v-if="!levelBlacklistEnabled" :label="t('components.general.label.blacklistDuration')">
          <div class="number-control"><input v-model.number="blacklistDuration" type="number" min="1" max="10080"><span>{{ t('components.general.label.minutes') }}</span></div>
        </ListRow>
      </section>

      <section class="settings-section">
        <h2>{{ t('components.general.title.modelData') }}</h2>
        <ListRow :label="t('components.general.label.autoSyncModels')" :sub-label="t('components.general.label.autoSyncModelsHint')">
          <label class="mac-switch"><input v-model="settings.auto_sync_models" type="checkbox"><span /></label>
        </ListRow>
        <ListRow :label="t('components.general.label.modelSyncStatus')" :sub-label="t('components.general.label.modelSyncSummary', { time: lastSync, count: pricingCount })">
          <div class="button-row">
            <button class="secondary-action" :disabled="syncing" @click="syncModels">{{ syncing ? t('components.general.label.modelSyncRunning') : t('components.general.label.modelSyncNow') }}</button>
            <button class="secondary-action danger" :disabled="syncing" @click="restorePricing">{{ t('components.general.label.modelRestoreBuiltin') }}</button>
          </div>
        </ListRow>
      </section>

      <section class="settings-section">
        <h2>{{ t('components.general.title.history') }}</h2>
        <ListRow :label="t('components.general.history.retention')" :sub-label="t('components.general.history.retentionHint')">
          <div class="number-control"><input v-model.number="settings.history_retention_days" type="number" min="1" max="3650"><span>{{ t('components.general.history.days') }}</span></div>
        </ListRow>
        <div class="section-actions">
          <button class="secondary-action danger" :disabled="cleaning || saving" @click="cleanHistory">
            {{ cleaning ? t('components.general.history.cleaning') : t('components.general.history.cleanNow') }}
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  width: min(920px, 100%);
  min-width: 0;
  margin: 0 auto;
  padding-bottom: 48px;
}

.settings-section {
  border-bottom: 1px solid var(--mac-border);
  padding: 22px 0;
}

.settings-section h2 {
  margin: 0 18px 8px;
  color: var(--mac-text-secondary);
  font-size: 0.78rem;
  font-weight: 700;
  text-transform: uppercase;
}

.settings-notice {
  margin: 14px 18px 0;
  padding: 10px 12px;
  border-left: 3px solid currentColor;
  font-size: 0.86rem;
}

.settings-notice.success { color: #15803d; background: rgba(34, 197, 94, 0.08); }
.settings-notice.error { color: #dc2626; background: rgba(239, 68, 68, 0.08); }

.primary-action,
.secondary-action {
  min-height: 34px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  padding: 0 14px;
  color: var(--mac-text);
  background: var(--mac-card);
  cursor: pointer;
}

.primary-action { color: #fff; border-color: #0a84ff; background: #0a84ff; }
.secondary-action.danger { color: #dc2626; }
.primary-action:disabled,
.secondary-action:disabled { opacity: 0.55; cursor: not-allowed; }

.button-row,
.number-control,
.section-actions { display: flex; align-items: center; gap: 8px; }
.section-actions { justify-content: flex-end; padding: 8px 18px 0; }

.number-control input {
  width: 88px;
  min-height: 34px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  padding: 0 9px;
  color: var(--mac-text);
  background: var(--mac-card);
}

.number-control span { color: var(--mac-text-secondary); font-size: 0.82rem; }

.timezone-select {
  width: min(280px, 100%);
  min-height: 34px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  padding: 0 30px 0 9px;
  color: var(--mac-text);
  background: var(--mac-card);
}

@media (max-width: 680px) {
  .button-row {
    width: 100%;
    align-items: stretch;
    flex-direction: column;
  }

  .button-row .secondary-action {
    width: 100%;
  }

  .timezone-select {
    width: 100%;
  }

  .settings-section h2,
  .settings-notice {
    margin-right: 14px;
    margin-left: 14px;
  }

  .section-actions {
    padding-right: 14px;
    padding-left: 14px;
  }
}
</style>
