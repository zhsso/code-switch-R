<script setup lang="ts">
import { computed, onActivated, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AutomationCard } from '../../data/cards'
import {
  LoadProviders,
  SaveModelGroups,
  type ModelGroup,
} from '../../services/providers'
import { extractErrorMessage } from '../../utils/error'

const { t } = useI18n()
const providers = ref<AutomationCard[]>([])
const groups = ref<ModelGroup[]>([])
const savedGroups = ref<ModelGroup[]>([])
const generation = ref(0)
const loading = ref(true)
const saving = ref(false)
const notice = ref('')
const modelDrafts = reactive<Record<number, string>>({})
const providerDrafts = reactive<Record<number, string>>({})
const draggedGroupId = ref<number | null>(null)
const draggedProvider = ref<{ groupId: number; providerId: number } | null>(null)
let persistRequested = false

const cloneGroups = (value: ModelGroup[]) => value.map(group => ({
  ...group,
  models: [...(group.models ?? [])],
  providerIds: [...(group.providerIds ?? [])],
}))

const orderedGroups = computed(() => groups.value
  .map((group, index) => ({ group, index }))
  .sort((a, b) => a.group.priority - b.group.priority || a.index - b.index)
  .map(item => item.group))

const providerById = computed(() => new Map(providers.value.map(provider => [provider.id, provider])))

const availableProviders = (group: ModelGroup) => providers.value.filter(
  provider => !group.providerIds.includes(provider.id),
)

const isIncomplete = (group: ModelGroup) => group.models.length === 0 || group.providerIds.length === 0

const load = async () => {
  loading.value = true
  notice.value = ''
  try {
    const snapshot = await LoadProviders<AutomationCard>('codex')
    providers.value = snapshot.providers ?? []
    groups.value = cloneGroups(snapshot.modelGroups ?? [])
    savedGroups.value = cloneGroups(groups.value)
    generation.value = snapshot.generation
  } catch (error) {
    notice.value = extractErrorMessage(error)
  } finally {
    loading.value = false
  }
}

const persist = async () => {
  if (saving.value) {
    persistRequested = true
    return true
  }

  do {
    persistRequested = false
    const rollback = cloneGroups(savedGroups.value)
    const candidate = cloneGroups(groups.value)
    saving.value = true
    notice.value = ''
    try {
      generation.value = await SaveModelGroups('codex', generation.value, candidate)
      savedGroups.value = candidate
      window.dispatchEvent(new CustomEvent('providers-updated'))
    } catch (error) {
      groups.value = rollback
      persistRequested = false
      notice.value = t('modelGroups.saveFailed', { error: extractErrorMessage(error) })
      try {
        const snapshot = await LoadProviders<AutomationCard>('codex')
        providers.value = snapshot.providers ?? []
        groups.value = cloneGroups(snapshot.modelGroups ?? [])
        savedGroups.value = cloneGroups(groups.value)
        generation.value = snapshot.generation
      } catch {
        // Keep the last confirmed local snapshot when reload also fails.
      }
      return false
    } finally {
      saving.value = false
    }
  } while (persistRequested)

  return true
}

const addGroup = async () => {
  const id = Math.max(Date.now(), ...groups.value.map(group => group.id + 1), 1)
  const used = new Set(groups.value.map(group => group.name.toLocaleLowerCase()))
  let sequence = groups.value.length + 1
  let name = t('modelGroups.defaultName', { number: sequence })
  while (used.has(name.toLocaleLowerCase())) {
    sequence++
    name = t('modelGroups.defaultName', { number: sequence })
  }
  groups.value.push({ id, name, enabled: true, priority: 100, models: [], providerIds: [] })
  await persist()
}

const deleteGroup = async (group: ModelGroup) => {
  if (!window.confirm(t('modelGroups.confirmDelete', { name: group.name }))) return
  groups.value = groups.value.filter(item => item.id !== group.id)
  await persist()
}

const commitName = async (group: ModelGroup) => {
  group.name = group.name.trim()
  const duplicate = groups.value.some(item => item.id !== group.id && item.name.trim().toLocaleLowerCase() === group.name.toLocaleLowerCase())
  if (!group.name || duplicate) {
    groups.value = cloneGroups(savedGroups.value)
    notice.value = duplicate ? t('modelGroups.duplicateName') : t('modelGroups.nameRequired')
    return
  }
  await persist()
}

const commitPriority = async (group: ModelGroup) => {
  const value = Number(group.priority)
  group.priority = Number.isFinite(value) ? Math.min(100, Math.max(1, Math.round(value))) : 100
  await persist()
}

const addModel = async (group: ModelGroup) => {
  const model = (modelDrafts[group.id] ?? '').trim()
  if (!model) return
  if ((model.match(/\*/g) ?? []).length > 1) {
    notice.value = t('modelGroups.oneWildcard')
    return
  }
  if (group.models.includes(model)) {
    notice.value = t('modelGroups.duplicateRule')
    return
  }
  group.models.push(model)
  modelDrafts[group.id] = ''
  await persist()
}

const removeModel = async (group: ModelGroup, model: string) => {
  group.models = group.models.filter(item => item !== model)
  await persist()
}

const addProvider = async (group: ModelGroup) => {
  const providerId = Number(providerDrafts[group.id])
  if (!providerId || group.providerIds.includes(providerId)) return
  group.providerIds.push(providerId)
  providerDrafts[group.id] = ''
  await persist()
}

const removeProvider = async (group: ModelGroup, providerId: number) => {
  group.providerIds = group.providerIds.filter(id => id !== providerId)
  await persist()
}

const dropProvider = async (group: ModelGroup, targetId: number) => {
  const source = draggedProvider.value
  draggedProvider.value = null
  if (!source || source.groupId !== group.id || source.providerId === targetId) return
  const from = group.providerIds.indexOf(source.providerId)
  const to = group.providerIds.indexOf(targetId)
  if (from < 0 || to < 0) return
  group.providerIds.splice(from, 1)
  group.providerIds.splice(to, 0, source.providerId)
  await persist()
}

const dropGroup = async (target: ModelGroup) => {
  const sourceId = draggedGroupId.value
  draggedGroupId.value = null
  if (sourceId === null || sourceId === target.id) return
  const source = groups.value.find(group => group.id === sourceId)
  if (!source || source.priority !== target.priority) return
  const from = groups.value.findIndex(group => group.id === sourceId)
  const to = groups.value.findIndex(group => group.id === target.id)
  groups.value.splice(from, 1)
  groups.value.splice(to, 0, source)
  await persist()
}

onMounted(load)
onActivated(() => {
  if (!loading.value) void load()
})
</script>

<template>
  <div class="groups-page">
    <header class="page-header">
      <div>
        <h1>{{ t('modelGroups.title') }}</h1>
      </div>
      <button class="primary-button" type="button" :disabled="loading || saving" @click="addGroup">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
        {{ t('modelGroups.addGroup') }}
      </button>
    </header>

    <div v-if="notice" class="notice" role="status">{{ notice }}</div>
    <div v-if="loading" class="empty-state">{{ t('modelGroups.loading') }}</div>
    <div v-else-if="orderedGroups.length === 0" class="empty-state">
      {{ t('modelGroups.empty') }}
    </div>

    <div v-else class="route-list" :aria-busy="saving">
      <section
        v-for="group in orderedGroups"
        :key="group.id"
        class="route-row"
        :class="{ disabled: !group.enabled }"
        @dragover.prevent
        @drop="dropGroup(group)"
      >
        <div class="priority-rail">
          <button
            class="icon-button drag-handle"
            type="button"
            draggable="true"
            :title="t('modelGroups.reorderGroups')"
            @dragstart="draggedGroupId = group.id"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="6" r="1" /><circle cx="15" cy="6" r="1" /><circle cx="9" cy="12" r="1" /><circle cx="15" cy="12" r="1" /><circle cx="9" cy="18" r="1" /><circle cx="15" cy="18" r="1" /></svg>
          </button>
          <label>
            <span>{{ t('modelGroups.priority') }}</span>
            <input v-model.number.lazy="group.priority" type="number" min="1" max="100" :disabled="saving" @change="commitPriority(group)">
          </label>
        </div>

        <div class="route-content">
          <div class="group-heading">
            <input class="name-input" v-model="group.name" :disabled="saving" @change="commitName(group)">
            <span v-if="isIncomplete(group)" class="warning-badge">{{ t('modelGroups.incomplete') }}</span>
            <label class="switch" :title="t('modelGroups.enabled')">
              <input v-model="group.enabled" type="checkbox" :disabled="saving" @change="persist">
              <span />
            </label>
            <button class="icon-button danger" type="button" :disabled="saving" :title="t('modelGroups.delete')" @click="deleteGroup(group)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6M10 10v6M14 10v6" /></svg>
            </button>
          </div>

          <div class="route-grid">
            <div class="rule-column">
              <span class="column-label">{{ t('modelGroups.modelRules') }}</span>
              <div class="chip-list">
                <span v-for="model in group.models" :key="model" class="model-chip">
                  {{ model }}
                  <button type="button" :title="t('modelGroups.removeRule')" @click="removeModel(group, model)">×</button>
                </span>
              </div>
              <form class="inline-add" @submit.prevent="addModel(group)">
                <input v-model="modelDrafts[group.id]" :placeholder="t('modelGroups.modelPlaceholder')" :disabled="saving">
                <button type="submit" class="icon-button" :title="t('modelGroups.addRule')" :disabled="saving">
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
                </button>
              </form>
            </div>

            <div class="route-arrow" aria-hidden="true">→</div>

            <div class="provider-column">
              <span class="column-label">{{ t('modelGroups.providerOrder') }}</span>
              <div class="provider-chain">
                <template v-for="(providerId, index) in group.providerIds" :key="providerId">
                  <div
                    class="provider-node"
                    draggable="true"
                    @dragstart="draggedProvider = { groupId: group.id, providerId }"
                    @dragover.prevent
                    @drop.stop="dropProvider(group, providerId)"
                  >
                    <svg class="grip" viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="1" /><circle cx="15" cy="8" r="1" /><circle cx="9" cy="16" r="1" /><circle cx="15" cy="16" r="1" /></svg>
                    <span class="order">{{ index + 1 }}</span>
                    <span>{{ providerById.get(providerId)?.name ?? `#${providerId}` }}</span>
                    <button type="button" :title="t('modelGroups.removeProvider')" @click="removeProvider(group, providerId)">×</button>
                  </div>
                  <span v-if="index < group.providerIds.length - 1" class="chain-arrow" aria-hidden="true">→</span>
                </template>
              </div>
              <div class="inline-add provider-add">
                <select v-model="providerDrafts[group.id]" :disabled="saving || availableProviders(group).length === 0">
                  <option value="">{{ t('modelGroups.selectProvider') }}</option>
                  <option v-for="provider in availableProviders(group)" :key="provider.id" :value="String(provider.id)">{{ provider.name }}</option>
                </select>
                <button type="button" class="icon-button" :title="t('modelGroups.addProvider')" :disabled="saving" @click="addProvider(group)">
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
                </button>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.groups-page { max-width: 1440px; margin: 0 auto; padding: 28px 32px 48px; color: var(--mac-text); }
.page-header { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 20px; }
.page-header h1 { margin: 0; font-size: 28px; letter-spacing: 0; }
.primary-button { display: inline-flex; align-items: center; gap: 8px; min-height: 38px; border: 0; border-radius: 7px; padding: 0 14px; background: var(--mac-accent); color: white; font-weight: 650; cursor: pointer; }
.primary-button svg, .icon-button svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
button:disabled, input:disabled, select:disabled { cursor: not-allowed; opacity: .55; }
.notice { margin-bottom: 14px; padding: 10px 12px; border: 1px solid rgba(245, 158, 11, .35); border-radius: 6px; background: rgba(245, 158, 11, .1); color: var(--mac-text); font-size: 13px; }
.empty-state { padding: 64px 20px; border-top: 1px solid var(--mac-divider); text-align: center; color: var(--mac-text-secondary); }
.route-list { border-top: 1px solid var(--mac-divider); }
.route-row { display: grid; grid-template-columns: 86px minmax(0, 1fr); border-bottom: 1px solid var(--mac-divider); background: color-mix(in srgb, var(--mac-surface) 78%, transparent); }
.route-row.disabled { opacity: .68; }
.priority-rail { display: flex; flex-direction: column; align-items: center; gap: 12px; padding: 18px 12px; border-right: 1px solid var(--mac-divider); background: color-mix(in srgb, var(--mac-surface-strong) 78%, transparent); }
.priority-rail label { display: grid; gap: 5px; text-align: center; color: var(--mac-text-secondary); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.priority-rail input { width: 54px; height: 32px; box-sizing: border-box; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text); text-align: center; font: 700 14px ui-monospace, SFMono-Regular, Menlo, monospace; }
.route-content { min-width: 0; padding: 18px 20px 20px; }
.group-heading { display: flex; align-items: center; gap: 10px; min-height: 34px; }
.name-input { min-width: 140px; max-width: 360px; width: 30%; border: 0; border-bottom: 1px solid transparent; background: transparent; color: var(--mac-text); font-size: 17px; font-weight: 700; }
.name-input:focus { outline: none; border-bottom-color: var(--mac-accent); }
.warning-badge { border-radius: 5px; padding: 3px 7px; background: rgba(245, 158, 11, .14); color: #b45309; font-size: 11px; font-weight: 700; }
.switch { position: relative; margin-left: auto; width: 38px; height: 22px; }
.switch input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.switch span { display: block; width: 100%; height: 100%; border-radius: 999px; background: rgba(110,110,115,.35); position: relative; cursor: pointer; }
.switch span::after { content: ''; position: absolute; width: 18px; height: 18px; left: 2px; top: 2px; border-radius: 50%; background: white; transition: transform .16s ease; box-shadow: 0 1px 3px rgba(0,0,0,.25); }
.switch input:checked + span { background: #30a46c; }
.switch input:checked + span::after { transform: translateX(16px); }
.switch input:focus-visible + span { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.icon-button { display: inline-grid; place-items: center; flex: 0 0 32px; width: 32px; height: 32px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); cursor: pointer; }
.icon-button:hover { color: var(--mac-text); background: var(--mac-surface-hover); }
.icon-button.danger:hover { color: #dc2626; border-color: rgba(220,38,38,.35); }
.drag-handle { cursor: grab; }
.drag-handle svg circle, .grip circle { fill: currentColor; stroke: none; }
.route-grid { display: grid; grid-template-columns: minmax(210px, .75fr) 30px minmax(340px, 1.5fr); align-items: start; gap: 12px; margin-top: 16px; }
.rule-column, .provider-column { min-width: 0; }
.column-label { display: block; margin-bottom: 8px; color: var(--mac-text-secondary); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.route-arrow { padding-top: 30px; color: var(--mac-text-secondary); text-align: center; }
.chip-list, .provider-chain { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; min-height: 32px; }
.model-chip { display: inline-flex; align-items: center; gap: 7px; min-height: 28px; box-sizing: border-box; border: 1px solid rgba(10,132,255,.25); border-radius: 5px; padding: 3px 7px 3px 9px; background: rgba(10,132,255,.08); font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
.model-chip button, .provider-node button { border: 0; background: transparent; color: var(--mac-text-secondary); cursor: pointer; font-size: 16px; line-height: 1; }
.inline-add { display: flex; gap: 6px; margin-top: 9px; max-width: 360px; }
.inline-add input, .inline-add select { min-width: 0; flex: 1; height: 32px; box-sizing: border-box; border: 1px solid var(--mac-border); border-radius: 6px; padding: 0 9px; background: var(--mac-surface); color: var(--mac-text); font-size: 12px; }
.provider-chain { gap: 5px; }
.provider-node { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; box-sizing: border-box; border: 1px solid var(--mac-border); border-radius: 6px; padding: 3px 7px; background: var(--mac-surface); cursor: grab; font-size: 12px; font-weight: 650; }
.provider-node .grip { width: 14px; height: 14px; color: var(--mac-text-secondary); }
.provider-node .order { display: inline-grid; place-items: center; width: 18px; height: 18px; border-radius: 4px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font: 700 10px ui-monospace, SFMono-Regular, Menlo, monospace; }
.chain-arrow { color: var(--mac-text-secondary); font-size: 12px; }
.provider-add { max-width: 420px; }
@media (max-width: 900px) {
  .groups-page { padding: 22px 18px 40px; }
  .route-grid { grid-template-columns: 1fr; }
  .route-arrow { padding: 0; transform: rotate(90deg); }
}
@media (max-width: 620px) {
  .page-header { align-items: flex-start; }
  .route-row { grid-template-columns: 58px minmax(0, 1fr); }
  .route-content { padding: 14px 12px 16px; }
  .group-heading { flex-wrap: wrap; }
  .name-input { width: 100%; min-width: 0; }
  .warning-badge { order: 2; flex-basis: 100%; }
}
</style>
