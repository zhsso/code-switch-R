<template>
  <div class="main-shell request-events-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <p class="events-eyebrow">{{ t('components.requestEvents.eyebrow') }}</p>
        <h1 class="app-page-title">{{ t('components.requestEvents.title') }}</h1>
        <p class="app-page-subtitle">{{ t('components.requestEvents.subtitle') }}</p>
      </div>
      <div class="app-page-actions">
        <span class="events-live-status" :class="{ loading }">
          <span class="events-live-dot" aria-hidden="true"></span>
          {{ t('components.requestEvents.live') }}
        </span>
        <button
          class="events-refresh-button"
          type="button"
          :disabled="loading"
          :title="t('components.requestEvents.refresh')"
          :aria-label="t('components.requestEvents.refresh')"
          @click="manualRefresh"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M20 11a8.1 8.1 0 0 0-14.8-4L3 10m0 0V4m0 6h6M4 13a8.1 8.1 0 0 0 14.8 4L21 14m0 0v6m0-6h-6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span class="events-refresh-countdown">{{ countdown }}s</span>
        </button>
      </div>
    </header>

    <div class="app-page-container request-events-page">
      <section class="events-overview" :aria-label="t('components.requestEvents.summary.ariaLabel')">
        <article class="events-overview-item events-overview-item--critical">
          <span class="events-overview-label">{{ t('components.requestEvents.summary.errors') }}</span>
          <strong>{{ summary.errors }}</strong>
          <span>{{ t('components.requestEvents.summary.currentPage') }}</span>
        </article>
        <article class="events-overview-item events-overview-item--switch">
          <span class="events-overview-label">{{ t('components.requestEvents.summary.switches') }}</span>
          <strong>{{ summary.switches }}</strong>
          <span>{{ t('components.requestEvents.summary.currentPage') }}</span>
        </article>
        <article class="events-overview-item events-overview-item--requests">
          <span class="events-overview-label">{{ t('components.requestEvents.summary.requests') }}</span>
          <strong>{{ summary.requests }}</strong>
          <span>{{ t('components.requestEvents.summary.correlated') }}</span>
        </article>
        <article class="events-overview-item events-overview-item--window">
          <span class="events-overview-label">{{ t('components.requestEvents.summary.window') }}</span>
          <strong>{{ filters.days }}</strong>
          <span>{{ t('components.requestEvents.summary.days') }}</span>
        </article>
      </section>

      <form class="events-filter-panel" @submit.prevent="applyFilters">
        <div class="events-filter-heading">
          <span class="events-section-kicker">{{ t('components.requestEvents.filters.title') }}</span>
          <span class="events-filter-hint">{{ t('components.requestEvents.filters.hint') }}</span>
        </div>

        <div class="events-filter-controls">
          <div class="events-segmented" role="group" :aria-label="t('components.requestEvents.filters.type')">
            <button
              v-for="option in eventTypeOptions"
              :key="option.value"
              type="button"
              :class="['events-segment', { active: filters.eventType === option.value }]"
              @click="selectEventType(option.value)"
            >
              {{ option.label }}
            </button>
          </div>

          <label class="events-filter-field">
            <span>{{ t('components.requestEvents.filters.provider') }}</span>
            <select v-model="filters.provider" class="mac-select">
              <option value="">{{ t('components.requestEvents.filters.allProviders') }}</option>
              <option v-for="provider in providerOptions" :key="provider" :value="provider">
                {{ provider }}
              </option>
            </select>
          </label>

          <label class="events-filter-field events-filter-field--request">
            <span>{{ t('components.requestEvents.filters.requestId') }}</span>
            <input
              v-model.trim="filters.requestId"
              class="events-request-input"
              type="search"
              :placeholder="t('components.requestEvents.filters.requestIdPlaceholder')"
              autocomplete="off"
            />
          </label>

          <label class="events-filter-field events-filter-field--days">
            <span>{{ t('components.requestEvents.filters.range') }}</span>
            <select v-model.number="filters.days" class="mac-select">
              <option :value="1">{{ t('components.requestEvents.filters.dayOption', { count: 1 }) }}</option>
              <option :value="7">{{ t('components.requestEvents.filters.dayOption', { count: 7 }) }}</option>
              <option :value="30">{{ t('components.requestEvents.filters.dayOption', { count: 30 }) }}</option>
              <option :value="90">{{ t('components.requestEvents.filters.dayOption', { count: 90 }) }}</option>
            </select>
          </label>

          <button class="events-apply-button" type="submit" :disabled="loading">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M4 5h16M7 12h10M10 19h4" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            </svg>
            {{ t('components.requestEvents.filters.apply') }}
          </button>
        </div>
      </form>

      <section class="events-list-panel">
        <div class="events-list-header">
          <div>
            <span class="events-section-kicker">{{ t('components.requestEvents.timeline') }}</span>
            <h2>{{ t('components.requestEvents.timelineTitle') }}</h2>
          </div>
          <span class="events-page-count">{{ events.length }} / {{ PAGE_SIZE }}</span>
        </div>

        <div v-if="loading && !events.length" class="events-state">
          <span class="events-state-pulse" aria-hidden="true"></span>
          {{ t('components.requestEvents.loading') }}
        </div>
        <div v-else-if="!events.length" class="events-state events-state--empty">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M7 3h10a2 2 0 0 1 2 2v14l-3-2-4 2-4-2-3 2V5a2 2 0 0 1 2-2Z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
            <path d="M8 8h8M8 12h6" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
          <span>{{ t('components.requestEvents.empty') }}</span>
        </div>
        <div v-else class="events-list">
          <article
            v-for="event in events"
            :key="event.id"
            :class="['event-row', `event-row--${eventKind(event)}`]"
          >
            <div class="event-rail" aria-hidden="true">
              <span class="event-marker">
                <svg v-if="eventKind(event) === 'error'" viewBox="0 0 24 24">
                  <path d="M12 3 2.8 19h18.4L12 3Z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
                  <path d="M12 9v4M12 16.5v.1" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
                </svg>
                <svg v-else-if="eventKind(event) === 'switch'" viewBox="0 0 24 24">
                  <path d="M4 7h13l-3-3M20 17H7l3 3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                  <path d="m17 4 3 3-3 3M7 14l-3 3 3 3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
                <svg v-else viewBox="0 0 24 24">
                  <path d="m5 12 4 4L19 6" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </span>
            </div>

            <div class="event-content">
              <div class="event-topline">
                <div class="event-kind-line">
                  <span class="event-kind-label">{{ eventLabel(event) }}</span>
                  <span class="event-time">{{ formatTime(event.created_at) }}</span>
                </div>
                <button
                  class="event-request-id"
                  type="button"
                  :title="copiedRequestId === event.request_id
                    ? t('components.requestEvents.copiedRequestId')
                    : t('components.requestEvents.copyRequestId')"
                  :aria-label="t('components.requestEvents.copyRequestId')"
                  @click="copyRequestId(event.request_id)"
                >
                  <span>{{ shortRequestId(event.request_id) }}</span>
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <rect x="9" y="9" width="10" height="10" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                    <path d="M15 9V6a1 1 0 0 0-1-1H6a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1h3" fill="none" stroke="currentColor" stroke-width="1.5" />
                  </svg>
                </button>
              </div>

              <div v-if="eventKind(event) === 'switch'" class="event-transition">
                <strong>{{ event.from_provider || '—' }}</strong>
                <span class="event-transition-arrow" aria-hidden="true">→</span>
                <strong>{{ event.to_provider || '—' }}</strong>
                <span v-if="event.message" class="event-transition-reason">{{ event.message }}</span>
              </div>
              <div v-else class="event-provider-line">
                <strong>{{ event.provider || t('components.requestEvents.unknownProvider') }}</strong>
                <span v-if="event.model" class="event-model">{{ event.model }}</span>
                <span v-if="event.message" class="event-message">{{ event.message }}</span>
              </div>

              <div class="event-meta-line">
                <span v-if="event.error_code" class="event-code">{{ event.error_code }}</span>
                <span v-if="event.http_code" class="event-http" :class="httpClass(event.http_code)">HTTP {{ event.http_code }}</span>
                <span v-if="event.attempt" class="event-meta-item">{{ t('components.requestEvents.meta.attempt', { count: event.attempt }) }}</span>
                <span v-if="event.retry" class="event-meta-item">{{ t('components.requestEvents.meta.retry', { count: event.retry }) }}</span>
                <span v-if="event.duration_sec" class="event-meta-item">{{ formatDuration(event.duration_sec) }}</span>
                <span v-if="event.outcome" :class="['event-outcome', `event-outcome--${event.outcome}`]">{{ outcomeLabel(event.outcome) }}</span>
              </div>
            </div>
          </article>
        </div>
      </section>

      <div class="events-pagination">
        <span>{{ t('components.requestEvents.pagination.page', { page }) }}</span>
        <div class="events-pagination-actions">
          <button
            class="events-page-button"
            type="button"
            :disabled="page === 1 || loading"
            :title="t('components.requestEvents.pagination.previous')"
            :aria-label="t('components.requestEvents.pagination.previous')"
            @click="previousPage"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m14 6-6 6 6 6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </button>
          <button
            class="events-page-button"
            type="button"
            :disabled="events.length < PAGE_SIZE || loading"
            :title="t('components.requestEvents.pagination.next')"
            :aria-label="t('components.requestEvents.pagination.next')"
            @click="nextPage"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m10 6 6 6-6 6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  fetchLogProviders,
  fetchRequestEvents,
  type RequestEvent,
  type RequestEventType,
} from '../../services/logs'

const { t, locale } = useI18n()

const PAGE_SIZE = 40
const REFRESH_SECONDS = 10
const events = ref<RequestEvent[]>([])
const providerOptions = ref<string[]>([])
const loading = ref(false)
const page = ref(1)
const countdown = ref(REFRESH_SECONDS)
const copiedRequestId = ref('')
let refreshTimer: number | undefined
let copyTimer: number | undefined

const filters = reactive<{
  eventType: RequestEventType
  provider: string
  requestId: string
  days: number
}>({
  eventType: 'incident',
  provider: '',
  requestId: '',
  days: 30,
})

const eventTypeOptions = computed(() => [
  { value: 'incident' as RequestEventType, label: t('components.requestEvents.filters.incidents') },
  { value: 'request_error' as RequestEventType, label: t('components.requestEvents.filters.errors') },
  { value: 'provider_switch' as RequestEventType, label: t('components.requestEvents.filters.switches') },
  { value: 'all' as RequestEventType, label: t('components.requestEvents.filters.all') },
])

const summary = computed(() => {
  const requestIds = new Set(events.value.map((event) => event.request_id).filter(Boolean))
  return {
    errors: events.value.filter((event) => event.event_type === 'request_error').length,
    switches: events.value.filter((event) => event.event_type === 'provider_switch').length,
    requests: requestIds.size,
  }
})

const parseDate = (value: string): Date | null => {
  if (!value) return null
  const utcValue = value.includes('T') || value.endsWith('Z')
    ? value
    : `${value.replace(' ', 'T')}Z`
  const candidates = [utcValue, value]
  for (const candidate of candidates) {
    const date = new Date(candidate)
    if (!Number.isNaN(date.getTime())) return date
  }
  return null
}

const formatTime = (value: string): string => {
  const date = parseDate(value)
  if (!date) return value || '—'
  return new Intl.DateTimeFormat(locale.value || 'zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

const formatDuration = (value: number): string => {
  const duration = Number(value) || 0
  if (duration < 1) return `${Math.round(duration * 1000)}ms`
  return `${duration.toFixed(2)}s`
}

const shortRequestId = (value: string): string => {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value
}

const eventKind = (event: RequestEvent): 'error' | 'switch' | 'complete' => {
  if (event.event_type === 'provider_switch') return 'switch'
  if (event.event_type === 'request_completed') return 'complete'
  return 'error'
}

const eventLabel = (event: RequestEvent): string => {
  const kind = eventKind(event)
  if (kind === 'switch') return t('components.requestEvents.event.switch')
  if (kind === 'complete') return t('components.requestEvents.event.completed')
  return t('components.requestEvents.event.error')
}

const outcomeLabel = (outcome: string): string => {
  if (outcome === 'success') return t('components.requestEvents.outcome.success')
  if (outcome === 'client_aborted') return t('components.requestEvents.outcome.clientAborted')
  if (outcome === 'continued') return t('components.requestEvents.outcome.continued')
  return t('components.requestEvents.outcome.failed')
}

const httpClass = (code: number): string => {
  if (code >= 200 && code < 300) return 'event-http--success'
  if (code >= 500) return 'event-http--server'
  return 'event-http--error'
}

const loadProviders = async () => {
  try {
    providerOptions.value = await fetchLogProviders('codex')
  } catch (error) {
    console.error('failed to load event providers', error)
  }
}

const loadEvents = async () => {
  if (loading.value) return
  loading.value = true
  try {
    const next = await fetchRequestEvents({
      platform: 'codex',
      eventType: filters.eventType,
      provider: filters.provider,
      requestId: filters.requestId,
      days: filters.days,
      limit: PAGE_SIZE,
      offset: (page.value - 1) * PAGE_SIZE,
    })
    if (!next.length && page.value > 1) {
      page.value -= 1
      events.value = await fetchRequestEvents({
        platform: 'codex',
        eventType: filters.eventType,
        provider: filters.provider,
        requestId: filters.requestId,
        days: filters.days,
        limit: PAGE_SIZE,
        offset: (page.value - 1) * PAGE_SIZE,
      })
    } else {
      events.value = next ?? []
    }
    countdown.value = REFRESH_SECONDS
  } catch (error) {
    console.error('failed to load request events', error)
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {
  page.value = 1
  void loadEvents()
}

const selectEventType = (value: RequestEventType) => {
  filters.eventType = value
  applyFilters()
}

const manualRefresh = () => {
  countdown.value = REFRESH_SECONDS
  void loadEvents()
}

const previousPage = () => {
  if (page.value <= 1) return
  page.value -= 1
  void loadEvents()
}

const nextPage = () => {
  if (events.value.length < PAGE_SIZE) return
  page.value += 1
  void loadEvents()
}

const copyRequestId = async (requestId: string) => {
  if (!requestId || !navigator.clipboard) return
  try {
    await navigator.clipboard.writeText(requestId)
    copiedRequestId.value = requestId
    if (copyTimer) window.clearTimeout(copyTimer)
    copyTimer = window.setTimeout(() => {
      copiedRequestId.value = ''
    }, 1500)
  } catch (error) {
    console.error('failed to copy request id', error)
  }
}

onMounted(() => {
  void Promise.all([loadEvents(), loadProviders()])
  refreshTimer = window.setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = REFRESH_SECONDS
      void loadEvents()
    } else {
      countdown.value -= 1
    }
  }, 1000)
})

onUnmounted(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
  if (copyTimer) window.clearTimeout(copyTimer)
})
</script>

<style scoped>
.request-events-shell {
  min-height: 100%;
}

.events-eyebrow,
.events-section-kicker {
  margin: 0;
  color: var(--mac-accent);
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.events-eyebrow {
  margin-bottom: 2px;
}

.events-live-status {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--mac-text-secondary);
  font-size: 0.78rem;
  font-weight: 600;
}

.events-live-status.loading {
  opacity: 0.65;
}

.events-live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #22c55e;
  box-shadow: 0 0 0 4px rgba(34, 197, 94, 0.14);
}

.events-refresh-button,
.events-page-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface);
  color: var(--mac-text-secondary);
  cursor: pointer;
  transition: color 0.15s ease, border-color 0.15s ease, background 0.15s ease;
}

.events-refresh-button {
  gap: 7px;
  min-height: 34px;
  padding: 0 10px;
  border-radius: 8px;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}

.events-refresh-button:hover:not(:disabled),
.events-page-button:hover:not(:disabled) {
  border-color: var(--mac-accent);
  color: var(--mac-accent);
  background: color-mix(in srgb, var(--mac-accent) 7%, var(--mac-surface));
}

.events-refresh-button:disabled,
.events-page-button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.events-refresh-button svg {
  width: 16px;
  height: 16px;
}

.request-events-page {
  gap: 18px;
}

.events-overview {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.events-overview-item {
  position: relative;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 2px 12px;
  min-height: 84px;
  padding: 15px 16px 13px 18px;
  overflow: hidden;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface);
}

.events-overview-item::before {
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  content: '';
  background: var(--mac-accent);
}

.events-overview-item--critical::before { background: #ef4444; }
.events-overview-item--switch::before { background: #f59e0b; }
.events-overview-item--requests::before { background: #06b6d4; }
.events-overview-item--window::before { background: #8b5cf6; }

.events-overview-label {
  color: var(--mac-text-secondary);
  font-size: 0.75rem;
  font-weight: 600;
}

.events-overview-item strong {
  align-self: center;
  color: var(--mac-text);
  font-size: 1.45rem;
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.events-overview-item > span:last-child {
  grid-column: 1 / -1;
  color: var(--mac-text-secondary);
  font-size: 0.7rem;
}

.events-filter-panel,
.events-list-panel {
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--mac-surface) 92%, transparent);
}

.events-filter-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px;
}

.events-filter-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 16px;
}

.events-filter-hint {
  color: var(--mac-text-secondary);
  font-size: 0.75rem;
}

.events-filter-controls {
  display: grid;
  grid-template-columns: auto minmax(130px, 0.7fr) minmax(180px, 1fr) minmax(125px, 0.6fr) auto;
  align-items: end;
  gap: 10px;
}

.events-segmented {
  display: inline-flex;
  min-height: 38px;
  align-items: stretch;
  overflow: hidden;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface-strong);
}

.events-segment {
  padding: 0 12px;
  border: 0;
  border-right: 1px solid var(--mac-border);
  background: transparent;
  color: var(--mac-text-secondary);
  font: inherit;
  font-size: 0.78rem;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
}

.events-segment:last-child { border-right: 0; }

.events-segment:hover { color: var(--mac-text); }

.events-segment.active {
  background: var(--mac-text);
  color: var(--mac-surface);
}

.events-filter-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
}

.events-filter-field > span {
  color: var(--mac-text-secondary);
  font-size: 0.7rem;
  font-weight: 700;
  text-transform: uppercase;
}

.events-filter-field .mac-select,
.events-request-input {
  box-sizing: border-box;
  width: 100%;
  min-height: 38px;
}

.events-request-input {
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
  color: var(--mac-text);
  font: inherit;
  font-size: 0.82rem;
  padding: 0 11px;
  outline: none;
}

.events-request-input:focus {
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--mac-accent) 18%, transparent);
}

.events-apply-button {
  display: inline-flex;
  min-height: 38px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 0 14px;
  border: 0;
  border-radius: 7px;
  background: var(--mac-accent);
  color: #fff;
  font: inherit;
  font-size: 0.8rem;
  font-weight: 700;
  cursor: pointer;
}

.events-apply-button:hover:not(:disabled) { filter: brightness(1.06); }
.events-apply-button:disabled { cursor: not-allowed; opacity: 0.5; }
.events-apply-button svg { width: 16px; height: 16px; }

.events-list-panel { overflow: hidden; }

.events-list-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  padding: 17px 18px 14px;
  border-bottom: 1px solid var(--mac-border);
}

.events-list-header h2 {
  margin: 4px 0 0;
  color: var(--mac-text);
  font-size: 1.05rem;
  font-weight: 700;
}

.events-page-count {
  color: var(--mac-text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  font-variant-numeric: tabular-nums;
}

.events-list { display: flex; flex-direction: column; }

.event-row {
  position: relative;
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr);
  min-width: 0;
  padding: 16px 18px 17px 16px;
  border-bottom: 1px solid var(--mac-border);
}

.event-row:last-child { border-bottom: 0; }

.event-rail {
  position: relative;
  display: flex;
  justify-content: center;
}

.event-rail::after {
  position: absolute;
  top: 30px;
  bottom: -33px;
  left: 50%;
  width: 1px;
  content: '';
  background: var(--mac-border);
}

.event-row:last-child .event-rail::after { display: none; }

.event-marker {
  z-index: 1;
  display: inline-flex;
  width: 25px;
  height: 25px;
  align-items: center;
  justify-content: center;
  border: 1px solid currentColor;
  border-radius: 50%;
  background: var(--mac-surface);
}

.event-marker svg { width: 15px; height: 15px; }
.event-row--error { color: #ef4444; }
.event-row--switch { color: #d97706; }
.event-row--complete { color: #0891b2; }

.event-content { min-width: 0; padding-left: 4px; }

.event-topline,
.event-kind-line,
.event-provider-line,
.event-transition,
.event-meta-line {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 8px;
  flex-wrap: wrap;
}

.event-topline { justify-content: space-between; gap: 12px; }
.event-kind-label { font-size: 0.78rem; font-weight: 800; }
.event-time { color: var(--mac-text-secondary); font-size: 0.75rem; font-variant-numeric: tabular-nums; }

.event-request-id {
  display: inline-flex;
  max-width: 190px;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  padding: 3px 6px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.7rem;
}

.event-request-id:hover { background: var(--mac-surface-hover); color: var(--mac-text); }
.event-request-id span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.event-request-id svg { width: 13px; height: 13px; flex-shrink: 0; }

.event-provider-line,
.event-transition { margin-top: 9px; color: var(--mac-text); font-size: 0.9rem; }
.event-provider-line strong,
.event-transition strong { font-weight: 700; }
.event-model { color: var(--mac-text-secondary); font-size: 0.78rem; }
.event-message,
.event-transition-reason { color: var(--mac-text-secondary); font-size: 0.8rem; }
.event-message::before,
.event-transition-reason::before {
  display: inline-block;
  width: 3px;
  height: 3px;
  margin-right: 8px;
  border-radius: 50%;
  background: var(--mac-border);
  content: '';
}
.event-transition-arrow { color: #d97706; font-size: 1.15rem; }

.event-meta-line { margin-top: 11px; gap: 6px; }
.event-code,
.event-http,
.event-meta-item,
.event-outcome {
  display: inline-flex;
  min-height: 21px;
  align-items: center;
  padding: 0 7px;
  border-radius: 4px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.68rem;
  font-variant-numeric: tabular-nums;
}

.event-code { color: #b45309; }
.event-http--success { color: #059669; }
.event-http--server,
.event-http--error { color: #dc2626; }
.event-outcome--success { color: #059669; }
.event-outcome--failed { color: #dc2626; }
.event-outcome--client_aborted { color: #64748b; }
.event-outcome--continued { color: #b45309; }

.events-state {
  display: flex;
  min-height: 220px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--mac-text-secondary);
  font-size: 0.85rem;
}

.events-state--empty { flex-direction: column; }
.events-state--empty svg { width: 32px; height: 32px; opacity: 0.55; }
.events-state-pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--mac-accent); animation: event-pulse 1s ease-in-out infinite; }

@keyframes event-pulse {
  0%, 100% { opacity: 0.35; transform: scale(0.85); }
  50% { opacity: 1; transform: scale(1); }
}

.events-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--mac-text-secondary);
  font-size: 0.76rem;
}

.events-pagination-actions { display: flex; gap: 7px; }
.events-page-button { width: 34px; height: 32px; border-radius: 7px; }
.events-page-button svg { width: 17px; height: 17px; }

@media (max-width: 1080px) {
  .events-filter-controls { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .events-segmented { grid-column: 1 / -1; justify-self: start; }
  .events-apply-button { min-width: 120px; }
}

@media (max-width: 700px) {
  .events-overview { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .events-filter-heading { align-items: flex-start; flex-direction: column; gap: 5px; }
  .events-filter-controls { grid-template-columns: 1fr 1fr; }
  .events-segmented { width: 100%; }
  .events-segment { flex: 1; padding: 0 7px; }
  .events-filter-field--request { grid-column: 1 / -1; }
  .events-apply-button { grid-column: 1 / -1; }
}

@media (max-width: 480px) {
  .events-overview { gap: 8px; }
  .events-overview-item { min-height: 75px; padding: 12px 12px 11px 14px; }
  .events-overview-item strong { font-size: 1.2rem; }
  .events-list-header { padding-left: 14px; padding-right: 14px; }
  .event-row { grid-template-columns: 29px minmax(0, 1fr); padding-left: 10px; padding-right: 10px; }
  .event-topline { align-items: flex-start; flex-direction: column; gap: 5px; }
  .event-request-id { max-width: 100%; padding-left: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .events-state-pulse { animation: none; }
}
</style>
