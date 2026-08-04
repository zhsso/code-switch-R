<template>
  <div class="main-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <h1 class="app-page-title" v-if="showHomeTitle">{{ t('components.main.hero.title') }}</h1>
      </div>
      <div class="app-page-actions">
        <button
          class="ghost-icon github-icon"
          :title="getGithubTooltip()"
          :aria-label="getGithubTooltip()"
          @click="handleGithubClick"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d="M9 19c-4.5 1.5-4.5-2.5-6-3m12 5v-3.87a3.37 3.37 0 00-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0018 3.77 5.07 5.07 0 0017.91 1S16.73.65 14 2.48a13.38 13.38 0 00-5 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 005 3.77a5.44 5.44 0 00-1.5 3.76c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 009 18.13V22"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
        <button
          class="ghost-icon"
          :title="t('components.main.controls.settings')"
          :aria-label="t('components.main.controls.settings')"
          @click="goToSettings"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d="M12 15a3 3 0 100-6 3 3 0 000 6z"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              fill="none"
            />
            <path
              d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              fill="none"
            />
          </svg>
        </button>
      </div>
    </header>
    <div class="app-page-container contrib-page">
      <section
        v-if="showHeatmap"
        ref="heatmapContainerRef"
        class="contrib-wall"
        :aria-label="t('components.main.heatmap.ariaLabel')"
      >
        <div class="contrib-legend">
          <span>{{ t('components.main.heatmap.legendLow') }}</span>
          <span v-for="level in 5" :key="level" :class="['legend-box', intensityClass(level - 1)]" />
          <span>{{ t('components.main.heatmap.legendHigh') }}</span>
        </div>

        <div class="contrib-grid">
          <div
            v-for="(week, weekIndex) in usageHeatmap"
            :key="weekIndex"
            class="contrib-column"
          >
            <div
              v-for="(day, dayIndex) in week"
              :key="dayIndex"
              class="contrib-cell"
              :class="intensityClass(day.intensity)"
              @mouseenter="showUsageTooltip(day, $event)"
              @mousemove="showUsageTooltip(day, $event)"
              @mouseleave="hideUsageTooltip"
            />
          </div>
        </div>
        <div
          v-if="usageTooltip.visible"
          ref="tooltipRef"
          class="contrib-tooltip"
          :class="usageTooltip.placement"
          :style="{ left: `${usageTooltip.left}px`, top: `${usageTooltip.top}px` }"
        >
          <p class="tooltip-heading">{{ formattedTooltipLabel }}</p>
          <ul class="tooltip-metrics">
            <li v-for="metric in usageTooltipMetrics" :key="metric.key">
              <span class="metric-label">{{ metric.label }}</span>
              <span class="metric-value">{{ metric.value }}</span>
            </li>
          </ul>
        </div>
      </section>

      <section class="automation-section">
      <div class="section-header">
        <div class="section-controls">
          <button
            class="ghost-icon"
            :data-tooltip="t('components.main.controls.addCard')"
            @click="openCreateModal"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 5v14M5 12h14"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
          <button
            class="ghost-icon"
            :class="{ 'rotating': refreshing }"
            :data-tooltip="t('components.main.controls.refresh')"
            @click="refreshAllData"
            :disabled="refreshing"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0118.8-4.3M22 12.5a10 10 0 01-18.8 4.2"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
        </div>
      </div>



      <TransitionGroup tag="div" name="card-flip" class="automation-list">
        <article
          v-for="card in activeCards"
          :key="card.id"
          :ref="el => { if (card.name === highlightedProvider) scrollToCard(el as HTMLElement) }"
          :class="[
            'automation-card',
            { dragging: draggingId === card.id },
            { 'drop-before': dropIndicator.id === card.id && dropIndicator.before },
            { 'drop-after': dropIndicator.id === card.id && !dropIndicator.before },
            { 'is-last-used': isLastUsedProvider(card.name) },
            { 'is-highlighted': highlightedProvider === card.name }
          ]"
          draggable="true"
          @dragstart="onDragStart(card.id)"
          @dragend="onDragEnd"
          @dragover="onCardDragOver(card, $event)"
          @drop="onDrop(card.id, $event)"
        >
          <!-- 正在使用标签 -->
          <span v-if="isLastUsedProvider(card.name)" class="last-used-badge">
            ✓ {{ t('components.main.providers.lastUsed') }}
          </span>
          <div class="card-leading">
            <div class="card-icon" :style="{ backgroundColor: card.tint, color: card.accent }">
              <span
                v-if="!iconSvg(card.icon)"
                class="icon-fallback"
              >
                {{ vendorInitials(card.name) }}
              </span>
              <span
                v-else
                class="icon-svg"
                v-html="iconSvg(card.icon)"
                aria-hidden="true"
              ></span>
            </div>
            <div class="card-text">
              <div class="card-title-row">
                <p class="card-title">{{ card.name }}</p>
                <!-- 连通性状态指示器 -->
                <span
                  v-if="card.availabilityMonitorEnabled"
                  class="connectivity-dot"
                  :class="getConnectivityIndicatorClass(card.id)"
                  :title="getConnectivityTooltip(card.id)"
                ></span>
                <span v-if="card.level" class="level-badge scheduling-level" :class="`level-${card.level}`">
                  L{{ card.level }}
                </span>
                <!-- 黑名单等级徽章（始终显示，包括 L0） -->
                <span
                  v-if="getProviderBlacklistStatus(card.name)"
                  :class="[
                    'blacklist-level-badge',
                    `bl-level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`,
                    { dark: resolvedTheme === 'dark' }
                  ]"
                  :title="t('components.main.blacklist.levelTitle', { level: getProviderBlacklistStatus(card.name)!.blacklistLevel })"
                >
                  BL{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <button
                  v-if="card.officialSite"
                  class="card-site"
                  type="button"
                  @click.stop="openOfficialSite(card.officialSite)"
                >
                  {{ formatOfficialSite(card.officialSite) }}
                </button>
              </div>
              <!-- <p class="card-subtitle">{{ card.apiUrl }}</p> -->
              <p
                v-for="stats in [providerStatDisplay(card.name)]"
                :key="`metrics-${card.id}`"
                class="card-metrics"
              >
                <template v-if="stats.state !== 'ready'">
                  {{ stats.message }}
                </template>
                <template v-else>
                  <span
                    v-if="stats.successRateLabel"
                    class="card-success-rate"
                    :class="stats.successRateClass"
                  >
                    {{ stats.successRateLabel }}
                  </span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span >{{ stats.requests }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.tokens }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.cost }}</span>
                </template>
              </p>
              <!-- 黑名单横幅 -->
              <div
                v-if="getProviderBlacklistStatus(card.name)?.isBlacklisted"
                :class="['blacklist-banner', { dark: resolvedTheme === 'dark' }]"
              >
                <div class="blacklist-info">
                  <span class="blacklist-icon">⛔</span>
                  <!-- 等级徽章（L1-L5，黑色/红色） -->
                  <span
                    v-if="getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                    :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                  >
                    L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                  </span>
                  <span class="blacklist-text">
                    {{ t('components.main.blacklist.blocked') }} |
                    {{ t('components.main.blacklist.remaining') }}:
                    {{ formatBlacklistCountdown(getProviderBlacklistStatus(card.name)!.remainingSeconds) }}
                  </span>
                </div>
                <div class="blacklist-actions">
                  <button
                    class="unblock-btn primary"
                    type="button"
                    @click.stop="handleUnblockAndReset(card.name)"
                    :title="t('components.main.blacklist.unblockAndResetHint')"
                  >
                    {{ t('components.main.blacklist.unblockAndReset') }}
                  </button>
                  <button
                    class="unblock-btn secondary"
                    type="button"
                    @click.stop="handleResetLevel(card.name)"
                    :title="t('components.main.blacklist.resetLevelHint')"
                  >
                    {{ t('components.main.blacklist.resetLevel') }}
                  </button>
                </div>
              </div>
              <!-- 等级徽章（未拉黑但有等级） -->
              <div
                v-else-if="getProviderBlacklistStatus(card.name) && getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                class="level-badge-standalone"
              >
                <span
                  :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                >
                  L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <span class="level-hint">{{ t('components.main.blacklist.levelHint') }}</span>
                <button
                  class="reset-level-mini"
                  type="button"
                  @click.stop="handleResetLevel(card.name)"
                  :title="t('components.main.blacklist.resetLevelHint')"
                >
                  ✕
                </button>
              </div>
            </div>
          </div>
          <div class="card-actions">
            <label class="mac-switch sm">
              <input type="checkbox" v-model="card.enabled" @change="persistProviders" />
              <span></span>
            </label>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.editTitle')" @click="configure(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M11.983 2.25a1.125 1.125 0 011.077.81l.563 2.101a7.482 7.482 0 012.326 1.343l2.08-.621a1.125 1.125 0 011.356.651l1.313 3.207a1.125 1.125 0 01-.442 1.339l-1.86 1.205a7.418 7.418 0 010 2.686l1.86 1.205a1.125 1.125 0 01.442 1.339l-1.313 3.207a1.125 1.125 0 01-1.356.651l-2.08-.621a7.482 7.482 0 01-2.326 1.343l-.563 2.101a1.125 1.125 0 01-1.077.81h-2.634a1.125 1.125 0 01-1.077-.81l-.563-2.101a7.482 7.482 0 01-2.326-1.343l-2.08.621a1.125 1.125 0 01-1.356-.651l-1.313-3.207a1.125 1.125 0 01.442-1.339l1.86-1.205a7.418 7.418 0 010-2.686l-1.86-1.205a1.125 1.125 0 01-.442-1.339l1.313-3.207a1.125 1.125 0 011.356-.651l2.08.621a7.482 7.482 0 012.326-1.343l.563-2.101a1.125 1.125 0 011.077-.81h2.634z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
                <path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.controls.duplicate')" @click="handleDuplicate(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.actions.delete')" @click="requestRemove(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </article>
      </TransitionGroup>

      </section>

      <BaseModal
      :open="modalState.open"
      :title="modalState.editingId ? t('components.main.form.editTitle') : t('components.main.form.createTitle')"
      @close="closeModal"
    >
      <form class="vendor-form" @submit.prevent="submitModal">
                <div v-if="!modalState.editingId" class="provider-preset-row">
                  <span class="provider-preset-label">{{ t('components.main.form.presets.label') }}</span>
                  <button
                    type="button"
                    class="provider-preset-button"
                    @click="applyDeepSeekV4FlashPreset"
                  >
                    <span class="provider-preset-icon" v-html="iconSvg('deepseek')" aria-hidden="true"></span>
                    <span>{{ t('components.main.form.presets.deepseekV4Flash') }}</span>
                  </button>
                </div>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.name') }}</span>
                  <BaseInput
                    v-model="modalState.form.name"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.name')"
                    required
                  />
                </label>

                <label class="form-field">
                  <span class="label-row">
                    {{ t('components.main.form.labels.apiUrl') }}
                    <span v-if="modalState.errors.apiUrl" class="field-error">
                      {{ modalState.errors.apiUrl }}
                    </span>
                  </span>
                  <BaseInput
                    v-model="modalState.form.apiUrl"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiUrl')"
                    required
                    :class="{ 'has-error': !!modalState.errors.apiUrl }"
                  />
                </label>

                <!-- 备用 API 地址（多入口容灾） -->
                <label class="form-field">
                  <span class="label-row">
                    {{ t('components.main.form.labels.fallbackApiUrls') }}
                    <span v-if="modalState.errors.fallbackApiUrls" class="field-error">
                      {{ modalState.errors.fallbackApiUrls }}
                    </span>
                  </span>
                  <textarea
                    v-model="modalState.form.fallbackApiUrlsText"
                    class="fallback-urls-input"
                    :class="{ 'has-error': !!modalState.errors.fallbackApiUrls }"
                    rows="2"
                    :placeholder="t('components.main.form.placeholders.fallbackApiUrls')"
                  ></textarea>
                  <span class="field-hint">{{ t('components.main.form.hints.fallbackApiUrls') }}</span>
                </label>

                <!-- 最大并发请求数（0=不限） -->
                <label class="form-field">
                  <span>{{ t('components.main.form.labels.maxConcurrency') }}</span>
                  <input
                    v-model.number="modalState.form.maxConcurrency"
                    type="number"
                    min="0"
                    class="fallback-urls-input"
                    :placeholder="t('components.main.form.placeholders.maxConcurrency')"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.maxConcurrency') }}</span>
                </label>

                <label class="form-field">
                  <span class="label-row">
                    {{ t('components.main.form.labels.costMultiplier') }}
                    <span v-if="modalState.errors.costMultiplier" class="field-error">
                      {{ modalState.errors.costMultiplier }}
                    </span>
                  </span>
                  <input
                    v-model.number="modalState.form.costMultiplier"
                    type="number"
                    min="0.01"
                    max="100"
                    step="0.01"
                    :class="{ 'has-error': !!modalState.errors.costMultiplier }"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.costMultiplier') }}</span>
                </label>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.officialSite') }}</span>
                  <BaseInput
                    v-model="modalState.form.officialSite"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.officialSite')"
                  />
                </label>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.apiKey') }}</span>
                  <div class="api-key-control">
                    <BaseInput
                      :model-value="modalState.form.apiKey"
                      :type="apiKeyVisible ? 'text' : 'password'"
                      :placeholder="apiKeyPlaceholder"
                      autocomplete="new-password"
                      @update:model-value="handleAPIKeyInput"
                    />
                    <button
                      type="button"
                      class="api-key-visibility"
                      :disabled="revealingAPIKey"
                      :title="apiKeyVisible
                        ? t('components.main.form.actions.hideApiKey')
                        : t('components.main.form.actions.revealApiKey')"
                      :aria-label="apiKeyVisible
                        ? t('components.main.form.actions.hideApiKey')
                        : t('components.main.form.actions.revealApiKey')"
                      @click="toggleAPIKeyVisibility"
                    >
                      <svg v-if="apiKeyVisible" viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M3 3l18 18M10.6 10.7a2 2 0 002.7 2.7M9.9 4.2A10.8 10.8 0 0112 4c5 0 8.5 4 9.5 8a12.4 12.4 0 01-2 3.9M6.2 6.3A12.4 12.4 0 002.5 12c1 4 4.5 8 9.5 8a10.8 10.8 0 004.1-.8" />
                      </svg>
                      <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M2.5 12S5.5 5 12 5s9.5 7 9.5 7-3 7-9.5 7-9.5-7-9.5-7z" />
                        <circle cx="12" cy="12" r="2.5" />
                      </svg>
                    </button>
                  </div>
                </label>

                <!-- API 端点（可选）-->
                <label class="form-field">
                  <span>{{ t('components.main.form.labels.apiEndpoint') }}</span>
                  <BaseInput
                    v-model="modalState.form.apiEndpoint"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiEndpoint')"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.apiEndpoint') }}</span>
                </label>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.compatibilityMode') }}</span>
                  <select v-model="modalState.form.compatibilityMode">
                    <option value="">{{ t('components.main.form.compatibility.standard') }}</option>
                    <option value="deepseek-codex">{{ t('components.main.form.compatibility.deepseekCodex') }}</option>
                  </select>
                </label>

                <!-- 认证方式 -->
                <div class="form-field">
                  <span>{{ t('components.main.form.labels.connectivityAuthType') }}</span>
                  <Listbox v-model="selectedAuthType" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ authTypeOptions.find((item) => item.value === selectedAuthType)?.label || selectedAuthType }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in authTypeOptions"
                          :key="option.value"
                          :value="option.value"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-name">{{ option.label }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <BaseInput
                    v-model="customAuthHeader"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.customAuthHeader')"
                    class="mt-2"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.connectivityAuthType') }}</span>
                </div>

                <!-- 跳过 TLS 证书验证 -->
                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.insecureSkipVerify') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.insecureSkipVerify" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.insecureSkipVerify ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.insecureSkipVerify') }}</span>
                </div>

                <!-- 请求清理 -->
                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.requestSanitize') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.requestSanitizeEnabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.requestSanitizeEnabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.requestSanitize') }}</span>
                </div>

                <!-- 请求清理高级配置 -->
                <div v-if="modalState.form.requestSanitizeEnabled" class="form-field">
                  <SanitizeConfigEditor v-model="modalState.form.sanitizeConfig" />
                </div>

                <div class="form-field">
                  <span>{{ t('components.main.form.labels.icon') }}</span>
                  <Listbox v-model="modalState.form.icon" v-slot="{ open }" class="w-full">
                    <div class="icon-select">
                      <ListboxButton class="icon-select-button">
                        <span class="icon-preview" v-html="iconSvg(modalState.form.icon)" aria-hidden="true"></span>
                        <span class="icon-select-label">{{ modalState.form.icon }}</span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="icon-select-options">
                        <div class="icon-search-wrapper">
                          <input
                            v-model="iconSearchQuery"
                            type="text"
                            class="icon-search-input"
                            :placeholder="t('components.main.form.placeholders.searchIcon')"
                            @click.stop
                            @keydown.stop
                          />
                        </div>
                        <ListboxOption
                          v-for="iconName in filteredIconOptions"
                          :key="iconName"
                          :value="iconName"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['icon-option', { active, selected }]">
                            <span class="icon-preview" v-html="iconSvg(iconName)" aria-hidden="true"></span>
                            <span class="icon-name">{{ iconName }}</span>
                          </div>
                        </ListboxOption>
                        <div v-if="filteredIconOptions.length === 0" class="icon-no-results">
                          {{ t('components.main.form.noIconResults') }}
                        </div>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                </div>

                <div class="form-field">
                  <span>{{ t('components.main.form.labels.level') }}</span>
                  <Listbox v-model="modalState.form.level" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-badge" :class="`level-${modalState.form.level || 1}`">
                          L{{ modalState.form.level || 1 }}
                        </span>
                        <span class="level-label">
                          Level {{ modalState.form.level || 1 }} - {{ getLevelDescription(modalState.form.level || 1) }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="lvl in 10"
                          :key="lvl"
                          :value="lvl"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-badge" :class="`level-${lvl}`">L{{ lvl }}</span>
                            <span class="level-name">Level {{ lvl }} - {{ getLevelDescription(lvl) }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <span class="field-hint">{{ t('components.main.form.hints.level') }}</span>
                </div>

                <div class="form-field">
                  <ModelWhitelistEditor v-model="modalState.form.supportedModels" />
                </div>

                <div class="form-field">
                  <ModelMappingEditor v-model="modalState.form.modelMapping" />
                </div>

                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.enabled') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.enabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.enabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                </div>

                <!-- 可用性监控配置 -->
                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.availabilityMonitor') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.availabilityMonitorEnabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.availabilityMonitorEnabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.availabilityMonitor') }}</span>
                </div>

                <!-- 连通性自动拉黑 -->
                <div v-if="modalState.form.availabilityMonitorEnabled" class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.connectivityAutoBlacklist') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.connectivityAutoBlacklist" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.connectivityAutoBlacklist ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.connectivityAutoBlacklist') }}</span>
                </div>

                <!-- 高级配置提示 -->
                <div v-if="modalState.form.availabilityMonitorEnabled" class="form-field">
                  <span class="field-hint" style="color: #6b7280;">
                    💡 {{ t('components.main.form.hints.availabilityAdvancedConfig') }}
                  </span>
                </div>

                <footer class="form-actions">
                  <BaseButton variant="outline" type="button" @click="closeModal">
                    {{ t('components.main.form.actions.cancel') }}
                  </BaseButton>
                  <BaseButton type="submit">
                    {{ t('components.main.form.actions.save') }}
                  </BaseButton>
                </footer>
      </form>
      </BaseModal>
      <BaseModal
      :open="confirmState.open"
      :title="t('components.main.form.confirmDeleteTitle')"
      variant="confirm"
      @close="closeConfirm"
    >
      <div class="confirm-body">
        <p>
          {{ t('components.main.form.confirmDeleteMessage', { name: confirmState.card?.name ?? '' }) }}
        </p>
      </div>
      <footer class="form-actions confirm-actions">
        <BaseButton variant="outline" type="button" @click="closeConfirm">
          {{ t('components.main.form.actions.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" type="button" @click="confirmRemove">
          {{ t('components.main.form.actions.delete') }}
        </BaseButton>
      </footer>
      </BaseModal>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Listbox, ListboxButton, ListboxOptions, ListboxOption } from '@headlessui/vue'
import { Call, Events } from '../../runtime'
import { type UsageHeatmapDay } from '../../data/usageHeatmap'
import { useAdaptiveHeatmap } from '../../composables/useAdaptiveHeatmap'
import {
  automationCardGroups,
  createAutomationCards,
  type AutomationCard,
  type ProviderCompatibilityMode,
} from '../../data/cards'
import { createDeepSeekV4FlashPreset } from '../../data/providerPresets'
import lobeIcons from '../../icons/lobeIconMap'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import BaseInput from '../common/BaseInput.vue'
import ModelWhitelistEditor from '../common/ModelWhitelistEditor.vue'
import ModelMappingEditor from '../common/ModelMappingEditor.vue'
import SanitizeConfigEditor from '../common/SanitizeConfigEditor.vue'
import {
  LoadProviders,
  SaveProviders,
  RevealProviderAPIKey,
  DuplicateProvider,
  RenameProvider,
} from '../../services/providers'
import { fetchProviderDailyStats, type ProviderDailyStat } from '../../services/logs'
import { fetchAppSettings, type AppSettings } from '../../services/appSettings'
import { useTheme } from '../../composables/useTheme'
import { useRouter } from 'vue-router'
import { showToast } from '../../utils/toast'
import { extractErrorMessage } from '../../utils/error'
import { getBlacklistStatus, manualUnblock, type BlacklistStatus } from '../../services/blacklist'
import {
  getConnectivityResults,
  StatusAvailable,
  StatusDegraded,
  StatusUnavailable,
  StatusMissing,
  getStatusColorClass,
  type ConnectivityResult,
} from '../../services/connectivity'
import {
  getLatestResults,
  HealthStatus,
  type ProviderTimeline,
} from '../../services/healthcheck'

const { t, locale } = useI18n()
const router = useRouter()
const { isDark } = useTheme()
const resolvedTheme = computed(() => (isDark.value ? 'dark' : 'light'))
const releasePageUrl = 'https://github.com/Rogers-F/code-switch-R/releases'

const heatmapContainerRef = ref<HTMLElement | null>(null)
// 使用自适应热力图 composable
const {
  displayData: usageHeatmap,
  init: initHeatmap,
  cleanup: cleanupHeatmap,
  reload: reloadHeatmap,
} = useAdaptiveHeatmap(heatmapContainerRef)
const tooltipRef = ref<HTMLElement | null>(null)

const providerStatsMap = reactive<Record<ProviderTab, Record<string, ProviderDailyStat>>>({
  codex: {},
})
const providerStatsLoading = reactive<Record<ProviderTab, boolean>>({
  codex: false,
})
const providerStatsLoaded = reactive<Record<ProviderTab, boolean>>({
  codex: false,
})
let providerStatsTimer: number | undefined
const showHeatmap = ref(true)
const showHomeTitle = ref(true)

// 黑名单状态
const blacklistStatusMap = reactive<Record<ProviderTab, Record<string, BlacklistStatus>>>({
  codex: {},
})
let blacklistTimer: number | undefined

// 连通性状态（已废弃，保留用于兼容）
const connectivityResultsMap = reactive<Record<ProviderTab, Record<number, ConnectivityResult>>>({
  codex: {},
})

// 可用性监控状态（新）
const availabilityResultsMap = reactive<Record<ProviderTab, Record<number, ProviderTimeline>>>({
  codex: {},
})

// 最后使用的供应商（用于高亮显示）
// @author sm
interface LastUsedProvider {
  platform: string
  provider_name: string
  updated_at: number
}
const lastUsedProviders = reactive<Record<string, LastUsedProvider | null>>({
  codex: null,
})
// 高亮闪烁的供应商名称
const highlightedProvider = ref<string | null>(null)
let highlightTimer: number | undefined

const intensityClass = (value: number) => `gh-level-${value}`

type TooltipPlacement = 'above' | 'below'

const usageTooltip = reactive({
  visible: false,
  label: '',
  dateKey: '',
  left: 0,
  top: 0,
  placement: 'above' as TooltipPlacement,
  requests: 0,
  inputTokens: 0,
  outputTokens: 0,
  reasoningTokens: 0,
  cost: 0,
})

const formatMetric = (value: number) => value.toLocaleString()

/**
 * 格式化 token 数值，支持 k/M/B 单位换算
 * @author sm
 */
const formatTokenNumber = (value: number) => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  }
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}k`
  }
  return value.toLocaleString()
}

const tooltipDateFormatter = computed(() =>
  new Intl.DateTimeFormat(locale.value || 'en', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
)

const currencyFormatter = computed(() =>
  new Intl.NumberFormat(locale.value || 'en', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
)

const formattedTooltipLabel = computed(() => {
  if (!usageTooltip.dateKey) return usageTooltip.label
  const date = new Date(usageTooltip.dateKey)
  if (Number.isNaN(date.getTime())) {
    return usageTooltip.label
  }
  return tooltipDateFormatter.value.format(date)
})

const formattedTooltipAmount = computed(() =>
  currencyFormatter.value.format(Math.max(usageTooltip.cost, 0))
)

const usageTooltipMetrics = computed(() => [
  {
    key: 'cost',
    label: t('components.main.heatmap.metrics.cost'),
    value: formattedTooltipAmount.value,
  },
  {
    key: 'requests',
    label: t('components.main.heatmap.metrics.requests'),
    value: formatMetric(usageTooltip.requests),
  },
  {
    key: 'inputTokens',
    label: t('components.main.heatmap.metrics.inputTokens'),
    value: formatTokenNumber(usageTooltip.inputTokens),
  },
  {
    key: 'outputTokens',
    label: t('components.main.heatmap.metrics.outputTokens'),
    value: formatTokenNumber(usageTooltip.outputTokens),
  },
  {
    key: 'reasoningTokens',
    label: t('components.main.heatmap.metrics.reasoningTokens'),
    value: formatTokenNumber(usageTooltip.reasoningTokens),
  },
])

const clamp = (value: number, min: number, max: number) => {
  if (max <= min) return min
  return Math.min(Math.max(value, min), max)
}

const TOOLTIP_DEFAULT_WIDTH = 220
const TOOLTIP_DEFAULT_HEIGHT = 120
const TOOLTIP_VERTICAL_OFFSET = 12
const TOOLTIP_HORIZONTAL_MARGIN = 20
const TOOLTIP_VERTICAL_MARGIN = 24

const getTooltipSize = () => {
  const rect = tooltipRef.value?.getBoundingClientRect()
  return {
    width: rect?.width ?? TOOLTIP_DEFAULT_WIDTH,
    height: rect?.height ?? TOOLTIP_DEFAULT_HEIGHT,
  }
}

const viewportSize = () => {
  if (typeof window !== 'undefined') {
    return { width: window.innerWidth, height: window.innerHeight }
  }
  if (typeof document !== 'undefined' && document.documentElement) {
    return {
      width: document.documentElement.clientWidth,
      height: document.documentElement.clientHeight,
    }
  }
  return {
    width: heatmapContainerRef.value?.clientWidth ?? 0,
    height: heatmapContainerRef.value?.clientHeight ?? 0,
  }
}

const showUsageTooltip = (day: UsageHeatmapDay, event: MouseEvent) => {
  const target = event.currentTarget as HTMLElement | null
  const cellRect = target?.getBoundingClientRect()
  if (!cellRect) return
  usageTooltip.label = day.label
  usageTooltip.dateKey = day.dateKey
  usageTooltip.requests = day.requests
  usageTooltip.inputTokens = day.inputTokens
  usageTooltip.outputTokens = day.outputTokens
  usageTooltip.reasoningTokens = day.reasoningTokens
  usageTooltip.cost = day.cost
  const { width: tooltipWidth, height: tooltipHeight } = getTooltipSize()
  const { width: viewportWidth, height: viewportHeight } = viewportSize()
  const centerX = cellRect.left + cellRect.width / 2
  const halfWidth = tooltipWidth / 2
  const minLeft = TOOLTIP_HORIZONTAL_MARGIN + halfWidth
  const maxLeft = viewportWidth > 0 ? viewportWidth - halfWidth - TOOLTIP_HORIZONTAL_MARGIN : centerX
  usageTooltip.left = clamp(centerX, minLeft, maxLeft)

  const anchorTop = cellRect.top
  const anchorBottom = cellRect.bottom
  const canShowAbove = anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET >= TOOLTIP_VERTICAL_MARGIN
  const viewportBottomLimit = viewportHeight > 0 ? viewportHeight - tooltipHeight - TOOLTIP_VERTICAL_MARGIN : anchorBottom
  const shouldPlaceBelow = !canShowAbove
  usageTooltip.placement = shouldPlaceBelow ? 'below' : 'above'
  const desiredTop = shouldPlaceBelow
    ? anchorBottom + TOOLTIP_VERTICAL_OFFSET
    : anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET
  usageTooltip.top = clamp(desiredTop, TOOLTIP_VERTICAL_MARGIN, viewportBottomLimit)
  usageTooltip.visible = true
}

const hideUsageTooltip = () => {
  usageTooltip.visible = false
}

const loadAppSettings = async () => {
  try {
    const data: AppSettings = await fetchAppSettings()
    showHeatmap.value = data?.show_heatmap ?? true
    showHomeTitle.value = data?.show_home_title ?? true
  } catch (error) {
    console.error('failed to load app settings', error)
    showHeatmap.value = true
    showHomeTitle.value = true
    // 加载应用设置失败时提示用户
    showToast(t('components.main.errors.loadAppSettingsFailed'), 'warning')
  }
}

const handleAppSettingsUpdated = () => {
  void loadAppSettings()
}

const normalizeProviderKey = (value: string) => value?.trim().toLowerCase() ?? ''

type ProviderTab = 'codex'
const activeTab = 'codex' as const

const cards = ref(createAutomationCards(automationCardGroups.codex))
const draggingId = ref<number | null>(null)

// 空对象转 undefined，避免写入无意义的空配置。
const emptyRecordToUndefined = <T extends Record<string, any>>(obj?: T | null): T | undefined =>
  obj && Object.keys(obj).length > 0 ? obj : undefined

const serializeProviders = (providers: AutomationCard[]) =>
  providers.map((provider) => ({
    ...provider,
    // 备用地址：空数组不落盘
    fallbackApiUrls: provider.fallbackApiUrls && provider.fallbackApiUrls.length > 0
      ? provider.fallbackApiUrls
      : undefined,
    // 最大并发：0 不落盘
    maxConcurrency: provider.maxConcurrency && provider.maxConcurrency > 0
      ? provider.maxConcurrency
      : undefined,
    // 倍率 1 使用后端兼容默认值，不写入冗余字段
    costMultiplier: provider.costMultiplier && provider.costMultiplier !== 1
      ? provider.costMultiplier
      : undefined,
    // 跳过 TLS 验证与请求清理
    insecureSkipVerify: !!provider.insecureSkipVerify,
    requestSanitizeEnabled: !!provider.requestSanitizeEnabled,
    sanitizeConfig: provider.sanitizeConfig && Object.keys(provider.sanitizeConfig).length > 0
      ? provider.sanitizeConfig
      : undefined,
    // 确保可用性配置正确序列化
    availabilityMonitorEnabled: !!provider.availabilityMonitorEnabled,
    connectivityAutoBlacklist: !!provider.connectivityAutoBlacklist,
    availabilityConfig: provider.availabilityConfig
      ? {
          testModel: provider.availabilityConfig.testModel || '',
          testEndpoint: provider.availabilityConfig.testEndpoint || '',
          timeout: provider.availabilityConfig.timeout || 15000,
        }
      : undefined,
    // 清除旧连通性字段（避免再次写入配置文件）
    connectivityCheck: false,
    connectivityTestModel: '',
    connectivityTestEndpoint: '',
    // 保留认证方式配置（已从废弃字段升级为活跃字段）
    connectivityAuthType: provider.connectivityAuthType || '',
  }))

const persistProvidersNow = async (): Promise<{ ok: boolean; error?: string }> => {
  try {
    await SaveProviders('codex', serializeProviders(cards.value))
    return { ok: true }
  } catch (error) {
    console.error('Failed to save providers', error)
    const errorMsg = extractErrorMessage(error)
    showToast(t('components.main.form.saveFailed') + ': ' + errorMsg, 'error')
    return { ok: false, error: errorMsg }
  }
}

// persistProviders：所有直接保存（开关、编辑、删除等）先排到在途拖拽保存之后，
// 避免"新的直接保存先落盘、排队中的旧拖拽快照随后覆盖"的丢改动竞态
const persistProviders = async (): Promise<{ ok: boolean; error?: string }> => {
  await dragPersistChain.catch(() => {})
  return persistProvidersNow()
}

const replaceProviders = (data: AutomationCard[]) => {
  cards.value = createAutomationCards(data)
}

const loadProvidersFromDisk = async () => {
  // 与拖拽保存互斥到"链静止"：重载若跑在拖拽保存前面，会用旧磁盘状态替换
  // 乐观排序；等待期间新入队的拖拽也要继续等。读盘过程中又有新拖拽入队时
  // 整体重来（有界重试），避免 replaceProviders 吃掉刚拖出来的顺序
  for (let attempt = 0; attempt < 3; attempt++) {
    let settled = dragPersistChain
    for (;;) {
      await settled.catch(() => {})
      if (settled === dragPersistChain) break
      settled = dragPersistChain
    }
    const token = dragPersistEnqueues
    await loadProvidersFromDiskOnce()
    if (token === dragPersistEnqueues) return
  }
}

const loadProvidersFromDiskOnce = async () => {
  try {
      const saved = await LoadProviders('codex')
      if (Array.isArray(saved)) {
        replaceProviders(saved as AutomationCard[])
        sortProvidersByLevel(cards.value)
      } else {
        await persistProviders()
      }
  } catch (error) {
      console.error('Failed to load providers', error)
      // 加载供应商失败时提示用户
      showToast(t('components.main.errors.loadProvidersFailed', { tab: 'codex' }), 'error')
  }
}

const loadProviderStats = async (tab: ProviderTab) => {
  providerStatsLoading[tab] = true
  try {
    const stats = await fetchProviderDailyStats('codex')
    const mapped: Record<string, ProviderDailyStat> = {}
    ;(stats ?? []).forEach((stat) => {
      mapped[normalizeProviderKey(stat.provider)] = stat
    })
    providerStatsMap[tab] = mapped
    providerStatsLoaded[tab] = true
  } catch (error) {
    console.error(`Failed to load provider stats for ${tab}`, error)
    if (!providerStatsLoaded[tab]) {
      providerStatsLoaded[tab] = true
    }
  } finally {
    providerStatsLoading[tab] = false
  }
}

// 加载黑名单状态
const loadBlacklistStatus = async (tab: ProviderTab) => {
  try {
    const statuses = await getBlacklistStatus()
    const map: Record<string, BlacklistStatus> = {}
    statuses.forEach(status => {
      map[status.providerName] = status
    })
    blacklistStatusMap[tab] = map
  } catch (err) {
    console.error(`加载 ${tab} 黑名单状态失败:`, err)
  }
}

// 手动解禁并重置（完全重置）
const handleUnblockAndReset = async (providerName: string) => {
  try {
    await Call.ByName('codeswitch/services.BlacklistService.ManualUnblockAndReset', activeTab, providerName)
    showToast(t('components.main.blacklist.unblockSuccess', { name: providerName }), 'success')
    await loadBlacklistStatus(activeTab)
  } catch (err) {
    console.error('解除拉黑失败:', err)
    showToast(t('components.main.blacklist.unblockFailed'), 'error')
  }
}

// 手动清零等级（仅重置等级）
const handleResetLevel = async (providerName: string) => {
  try {
    await Call.ByName('codeswitch/services.BlacklistService.ManualResetLevel', activeTab, providerName)
    showToast(t('components.main.blacklist.resetLevelSuccess', { name: providerName }), 'success')
    await loadBlacklistStatus(activeTab)
  } catch (err) {
    console.error('清零等级失败:', err)
    showToast(t('components.main.blacklist.resetLevelFailed'), 'error')
  }
}

// 手动解禁（向后兼容，调用 handleUnblockAndReset）
const handleUnblock = handleUnblockAndReset

// 格式化倒计时
const formatBlacklistCountdown = (remainingSeconds: number): string => {
  const minutes = Math.floor(remainingSeconds / 60)
  const seconds = remainingSeconds % 60
  return `${minutes}${t('components.main.blacklist.minutes')}${seconds}${t('components.main.blacklist.seconds')}`
}

// 获取 provider 黑名单状态
const getProviderBlacklistStatus = (providerName: string): BlacklistStatus | null => {
  return blacklistStatusMap[activeTab][providerName] || null
}

// 加载连通性测试结果（已废弃，保留兼容）
const loadConnectivityResults = async (tab: ProviderTab) => {
  try {
    const results = await getConnectivityResults()
    const map: Record<number, ConnectivityResult> = {}
    results.forEach((result) => {
      map[result.providerId] = result
    })
    connectivityResultsMap[tab] = map
  } catch (err) {
    console.error(`加载 ${tab} 连通性结果失败:`, err)
  }
}

// 加载可用性监控结果（新）
const loadAvailabilityResults = async () => {
  try {
    const allResults = await getLatestResults()

    // 转换为按平台和 ID 索引的格式
    for (const platform of Object.keys(allResults)) {
      const timelines = allResults[platform] || []
      const map: Record<number, ProviderTimeline> = {}
      timelines.forEach((timeline) => {
        map[timeline.providerId] = timeline
      })
      availabilityResultsMap[platform as ProviderTab] = map
    }
  } catch (err) {
    console.error('加载可用性监控结果失败:', err)
  }
}

// 获取 provider 连通性状态（已废弃）
const getProviderConnectivityResult = (providerId: number): ConnectivityResult | null => {
  return connectivityResultsMap[activeTab][providerId] || null
}

// 获取 provider 可用性状态（新）
const getProviderAvailabilityResult = (providerId: number): ProviderTimeline | null => {
  return availabilityResultsMap[activeTab][providerId] || null
}

// 获取连通性状态指示器样式（改用可用性监控结果）
const getConnectivityIndicatorClass = (providerId: number): string => {
  const result = getProviderAvailabilityResult(providerId)
  if (!result || !result.latest) return 'connectivity-gray'

  // 根据可用性监控状态返回样式
  switch (result.latest.status) {
    case HealthStatus.OPERATIONAL:
      return 'connectivity-green'
    case HealthStatus.DEGRADED:
      return 'connectivity-yellow'
    case HealthStatus.FAILED:
    case HealthStatus.VALIDATION_ERROR:
      return 'connectivity-red'
    default:
      return 'connectivity-gray'
  }
}

// 获取连通性状态提示文本（改用可用性监控结果）
const getConnectivityTooltip = (providerId: number): string => {
  const result = getProviderAvailabilityResult(providerId)
  if (!result || !result.latest) return t('components.main.connectivity.noData')

  let statusText = ''
  switch (result.latest.status) {
    case HealthStatus.OPERATIONAL:
      statusText = t('components.main.connectivity.available')
      break
    case HealthStatus.DEGRADED:
      statusText = t('components.main.connectivity.degraded')
      break
    case HealthStatus.FAILED:
    case HealthStatus.VALIDATION_ERROR:
      statusText = t('components.main.connectivity.unavailable')
      break
    default:
      statusText = t('components.main.connectivity.noData')
  }

  const latencyText = result.latest.latencyMs > 0 ? ` (${result.latest.latencyMs}ms)` : ''
  const uptimeText = result.uptime > 0 ? ` - ${result.uptime.toFixed(1)}%` : ''
  return statusText + latencyText + uptimeText
}

// 刷新所有数据
const refreshing = ref(false)
const refreshAllData = async () => {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([
      reloadHeatmap(),
      loadProvidersFromDisk(),
      loadProviderStats(activeTab),
      loadBlacklistStatus(activeTab),
      loadAvailabilityResults(),
    ])
  } catch (error) {
    console.error('Failed to refresh data', error)
  } finally {
    refreshing.value = false
  }
}

type ProviderStatDisplay =
  | { state: 'loading' | 'empty'; message: string }
  | {
      state: 'ready'
      requests: string
      tokens: string
      cost: string
      successRateLabel: string
      successRateClass: string
    }

const SUCCESS_RATE_THRESHOLDS = {
  healthy: 0.95,
  warning: 0.8,
} as const

const formatSuccessRateLabel = (value: number) => {
  const percent = clamp(value, 0, 1) * 100
  const decimals = percent >= 99.5 || percent === 0 ? 0 : 1
  return `${t('components.main.providers.successRate')}: ${percent.toFixed(decimals)}%`
}

const successRateClassName = (value: number) => {
  const rate = clamp(value, 0, 1)
  if (rate >= SUCCESS_RATE_THRESHOLDS.healthy) {
    return 'success-good'
  }
  if (rate >= SUCCESS_RATE_THRESHOLDS.warning) {
    return 'success-warn'
  }
  return 'success-bad'
}

const providerStatDisplay = (providerName: string): ProviderStatDisplay => {
  const tab = activeTab
  if (!providerStatsLoaded[tab]) {
    return { state: 'loading', message: t('components.main.providers.loading') }
  }
  const stat = providerStatsMap[tab]?.[normalizeProviderKey(providerName)]
  if (!stat) {
    return { state: 'empty', message: t('components.main.providers.noData') }
  }
  const totalTokens = stat.input_tokens + stat.output_tokens
  const successRateValue = Number.isFinite(stat.success_rate) ? clamp(stat.success_rate, 0, 1) : null
  const successRateLabel = successRateValue !== null ? formatSuccessRateLabel(successRateValue) : ''
  const successRateClass = successRateValue !== null ? successRateClassName(successRateValue) : ''
  return {
    state: 'ready',
    requests: `${t('components.main.providers.requests')}: ${formatMetric(stat.total_requests)}`,
    tokens: `${t('components.main.providers.tokens')}: ${formatTokenNumber(totalTokens)}`,
    cost: `${t('components.main.providers.cost')}: ${currencyFormatter.value.format(Math.max(stat.cost_total, 0))}`,
    successRateLabel,
    successRateClass,
  }
}

const normalizeUrlWithScheme = (value: string) => {
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.toString()
  } catch {
    return `https://${value}`
  }
}

const openOfficialSite = (site: string) => {
  const target = normalizeUrlWithScheme(site)
  if (!target) return
  window.open(target, '_blank', 'noopener,noreferrer')
}

const formatOfficialSite = (site: string) => {
  if (!site) return ''
  try {
    const url = new URL(normalizeUrlWithScheme(site))
    return url.hostname.replace(/^www\./, '')
  } catch {
    return site
  }
}

const startProviderStatsTimer = () => {
  stopProviderStatsTimer()
  providerStatsTimer = window.setInterval(() => {
    void loadProviderStats(activeTab)
    void loadAvailabilityResults() // 同步刷新可用性监控状态（改用新服务）
  }, 60_000)
}

const stopProviderStatsTimer = () => {
  if (providerStatsTimer) {
    clearInterval(providerStatsTimer)
    providerStatsTimer = undefined
  }
}

// 加载最后使用的供应商
// @author sm
const loadLastUsedProviders = async () => {
  try {
    const result = await Call.ByName('codeswitch/services.ProviderRelayService.GetAllLastUsedProviders')
    if (result) {
      Object.keys(result).forEach(platform => {
        if (result[platform]) {
          lastUsedProviders[platform] = result[platform]
        }
      })
    }
  } catch (err) {
    console.error('加载最后使用的供应商失败:', err)
  }
}

// 切换到指定平台的 Tab 并高亮供应商
// @author sm
const switchToTabAndHighlight = (platform: string, providerName: string) => {
  // 切换到对应的 Tab
  if (platform !== activeTab) return

  // 更新最后使用的供应商
  lastUsedProviders[platform] = {
    platform,
    provider_name: providerName,
    updated_at: Date.now(),
  }

  // 高亮闪烁供应商卡片
  highlightedProvider.value = providerName

  // 清除之前的高亮计时器
  if (highlightTimer) {
    clearTimeout(highlightTimer)
  }

  // 3 秒后取消高亮
  highlightTimer = window.setTimeout(() => {
    highlightedProvider.value = null
  }, 3000)

  // 刷新黑名单状态
  void loadBlacklistStatus(platform as ProviderTab)
}

// 处理供应商切换事件
// @author sm
const handleProviderSwitched = (event: { data: { platform: string; toProvider: string } }) => {
  const { platform, toProvider } = event.data
  console.log('[Event] provider:switched', platform, toProvider)
  switchToTabAndHighlight(platform, toProvider)
}

// 处理供应商拉黑事件
// @author sm
const handleProviderBlacklisted = (event: { data: { platform: string; providerName: string } }) => {
  const { platform, providerName } = event.data
  console.log('[Event] provider:blacklisted', platform, providerName)
  switchToTabAndHighlight(platform, providerName)
}

// 判断供应商是否是最后使用的
// @author sm
const isLastUsedProvider = (providerName: string): boolean => {
  const lastUsed = lastUsedProviders[activeTab]
  return lastUsed?.provider_name === providerName
}

// 滚动到指定卡片
// @author sm
const scrollToCard = (el: HTMLElement | null) => {
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
}

// 事件取消订阅函数
let unsubscribeSwitched: (() => void) | undefined
let unsubscribeBlacklisted: (() => void) | undefined

onMounted(async () => {
  void initHeatmap()
  await loadProvidersFromDisk()
  await loadProviderStats(activeTab)
  await loadAppSettings()
  startProviderStatsTimer()

  // 加载初始黑名单状态
  await loadBlacklistStatus(activeTab)

  // 加载初始可用性监控结果（改用新服务）
  await loadAvailabilityResults()

  // 每秒更新黑名单倒计时
  blacklistTimer = window.setInterval(() => {
    const tab = activeTab
    Object.keys(blacklistStatusMap[tab]).forEach(providerName => {
      const status = blacklistStatusMap[tab][providerName]
      if (status && status.isBlacklisted && status.remainingSeconds > 0) {
        status.remainingSeconds--
        if (status.remainingSeconds <= 0) {
          loadBlacklistStatus(tab)
        }
      }
    })
  }, 1000)

  // 窗口焦点事件：从最小化恢复时立即刷新黑名单状态
  const handleWindowFocus = () => {
    void loadBlacklistStatus(activeTab)
  }
  window.addEventListener('focus', handleWindowFocus)

  // 定期轮询黑名单状态（每 10 秒）
  const blacklistPollingTimer = window.setInterval(() => {
    void loadBlacklistStatus(activeTab)
  }, 10_000)

  // 存储定时器 ID 以便清理
  ;(window as any).__blacklistPollingTimer = blacklistPollingTimer
  ;(window as any).__handleWindowFocus = handleWindowFocus

  window.addEventListener('app-settings-updated', handleAppSettingsUpdated)

  // 监听可用性页面的 Provider 更新事件
  const handleProvidersUpdated = () => {
    void loadProvidersFromDisk()
  }
  window.addEventListener('providers-updated', handleProvidersUpdated)
  ;(window as any).__handleProvidersUpdated = handleProvidersUpdated

  // 加载最后使用的供应商
  await loadLastUsedProviders()

  // 监听供应商切换和拉黑事件
  unsubscribeSwitched = Events.On('provider:switched', handleProviderSwitched)
  unsubscribeBlacklisted = Events.On('provider:blacklisted', handleProviderBlacklisted)
})

onUnmounted(() => {
  cleanupHeatmap()
  stopProviderStatsTimer()
  window.removeEventListener('app-settings-updated', handleAppSettingsUpdated)

  // 清理黑名单相关定时器和事件监听
  if (blacklistTimer) {
    window.clearInterval(blacklistTimer)
  }
  if ((window as any).__blacklistPollingTimer) {
    window.clearInterval((window as any).__blacklistPollingTimer)
  }
  if ((window as any).__handleWindowFocus) {
    window.removeEventListener('focus', (window as any).__handleWindowFocus)
  }
  if ((window as any).__handleProvidersUpdated) {
    window.removeEventListener('providers-updated', (window as any).__handleProvidersUpdated)
  }

  // 清理高亮计时器
  if (highlightTimer) {
    clearTimeout(highlightTimer)
  }

  // 取消事件订阅
  if (unsubscribeSwitched) {
    unsubscribeSwitched()
  }
  if (unsubscribeBlacklisted) {
    unsubscribeBlacklisted()
  }
})

const activeCards = computed(() => cards.value)

// 连通性测试端点选项
const connectivityEndpointOptions = [
  { value: '/responses', label: '/responses (Codex)' },
]

// 连通性测试状态
const testingConnectivity = ref(false)
const connectivityTestResult = ref<{ success: boolean; message: string } | null>(null)

// 获取平台默认端点
const getDefaultEndpoint = (platform: string) => {
  const defaults: Record<string, string> = {
    codex: '/responses',
  }
  return defaults.codex
}

// 获取平台默认认证方式（默认 Bearer，与 v2.2.x 保持一致）
const getDefaultAuthType = (_platform: string) => 'bearer'

// 手动测试连通性
const handleTestConnectivity = async () => {
  testingConnectivity.value = true
  connectivityTestResult.value = null

  try {
    if (editingCard.value?.apiKeyConfigured && !modalState.form.apiKey) {
      connectivityTestResult.value = {
        success: false,
        message: t('components.main.form.connectivity.revealToTest'),
      }
      return
    }
    const platform = modalState.tabId
    // connectivityTestModel / connectivityTestEndpoint 是已废弃字段：表单里没有任何输入绑定，
    // 保存时后端还会把它们清零，恒为空串。真正的用户配置在可用性高级配置与 API 端点里，
    // 这里必须按同一优先级取值，否则自定义端点的供应商永远被测到默认端点上、必然误报失败。
    const testModel = modalState.form.availabilityConfig?.testModel?.trim()
      || modalState.form.connectivityTestModel
      || ''
    const testEndpoint = modalState.form.availabilityConfig?.testEndpoint?.trim()
      || modalState.form.connectivityTestEndpoint
      || modalState.form.apiEndpoint?.trim()
      || getDefaultEndpoint(platform)

    const result = await Call.ByName(
      'codeswitch/services.ConnectivityTestService.TestProviderManual',
      platform,
      modalState.form.apiUrl,
      modalState.form.apiKey,
      testModel,
      testEndpoint,
      resolveEffectiveAuthType(),
      !!modalState.form.insecureSkipVerify
    )

    connectivityTestResult.value = {
      success: result.success,
      message: result.success
        ? t('components.main.form.connectivity.success', { latency: result.latencyMs })
        : result.message || t('components.main.form.connectivity.failed')
    }
  } catch (error) {
    connectivityTestResult.value = {
      success: false,
      message: t('components.main.form.connectivity.error', { error: extractErrorMessage(error) })
    }
  } finally {
    testingConnectivity.value = false
  }
}

const goToSettings = () => {
  router.push('/settings')
}

const handleGithubClick = () => {
  window.open(releasePageUrl, '_blank', 'noopener,noreferrer')
}

// 获取 GitHub 图标的 tooltip
const getGithubTooltip = () => {
  return t('components.main.controls.github')
}

type VendorForm = {
  name: string
  apiUrl: string
  apiKey: string
  officialSite: string
  icon: string
  enabled: boolean
  supportedModels?: Record<string, boolean>
  modelMapping?: Record<string, string>
  level?: number
  apiEndpoint?: string
  // 备用地址编辑框原文（每行一个）
  fallbackApiUrlsText?: string
  // 最大并发请求数（0=不限）
  maxConcurrency?: number
  // 费用统计倍率（缺失时为 1）
  costMultiplier?: number
  // === 可用性监控配置（新） ===
  availabilityMonitorEnabled?: boolean
  connectivityAutoBlacklist?: boolean
  availabilityConfig?: {
    testModel?: string
    testEndpoint?: string
    timeout?: number
  }
  // === 旧连通性字段（已废弃） ===
  /** @deprecated */
  connectivityCheck?: boolean
  /** @deprecated */
  connectivityTestModel?: string
  /** @deprecated */
  connectivityTestEndpoint?: string
  /** @deprecated */
  connectivityAuthType?: string
  // 上游协议类型
  // 跳过 TLS 证书验证（仅该供应商）
  insecureSkipVerify?: boolean
  // 请求清理
  requestSanitizeEnabled?: boolean
  sanitizeConfig?: {
    blockedBodyFields?: string[]
    blockedHeaders?: string[]
  }
  compatibilityMode?: ProviderCompatibilityMode
}

const iconOptions = Object.keys(lobeIcons).sort((a, b) => a.localeCompare(b))
const defaultIconKey = iconOptions[0] ?? 'aicoding'

// 图标搜索筛选
const iconSearchQuery = ref('')
const filteredIconOptions = computed(() => {
  const query = iconSearchQuery.value.toLowerCase().trim()
  if (!query) return iconOptions
  return iconOptions.filter(name => name.toLowerCase().includes(query))
})

const defaultFormValues = (platform?: string): VendorForm => ({
  name: '',
  apiUrl: '',
  apiKey: '',
  officialSite: '',
  icon: defaultIconKey,
  level: 1,
  enabled: true,
  supportedModels: {},
  modelMapping: {},
  apiEndpoint: '', // API 端点（可选）
  fallbackApiUrlsText: '',
  maxConcurrency: 0,
  costMultiplier: 1,
  insecureSkipVerify: false, // 默认严格验证上游 TLS 证书
  requestSanitizeEnabled: false, // 请求清理默认关闭
  sanitizeConfig: {},
  compatibilityMode: '',
  // 可用性监控配置（新）
  availabilityMonitorEnabled: false,
  connectivityAutoBlacklist: false,
  availabilityConfig: {
    testModel: '',
    testEndpoint: getDefaultEndpoint(platform || 'codex'),
    timeout: 15000,
  },
  // 旧连通性字段（已废弃，置空）
  connectivityCheck: false,
  connectivityTestModel: '',
  connectivityTestEndpoint: '',
  connectivityAuthType: '',
})

// Level 描述文本映射（1-10）
const getLevelDescription = (level: number) => {
  const descriptions: Record<number, string> = {
    1: t('components.main.levelDesc.highest'),
    2: t('components.main.levelDesc.high'),
    3: t('components.main.levelDesc.mediumHigh'),
    4: t('components.main.levelDesc.medium'),
    5: t('components.main.levelDesc.normal'),
    6: t('components.main.levelDesc.mediumLow'),
    7: t('components.main.levelDesc.low'),
    8: t('components.main.levelDesc.lower'),
    9: t('components.main.levelDesc.veryLow'),
    10: t('components.main.levelDesc.lowest'),
  }
  return descriptions[level] || t('components.main.levelDesc.normal')
}

// 归一化最大并发：空/非法/负数视为 0（不限），取整
const normalizeMaxConcurrency = (value: number | string | undefined): number => {
  const num = Number(value)
  if (!Number.isFinite(num) || num <= 0) return 0
  return Math.floor(num)
}

// 归一化 level：空/非法视为 1（最高优先级），范围限制 1-10
const normalizeLevel = (level: number | string | undefined): number => {
  const num = Number(level)
  if (!Number.isFinite(num) || num < 1) return 1
  if (num > 10) return 10
  return Math.floor(num)  // 确保返回整数
}

// 按 enabled 和 level 排序：启用的排在前面，同启用状态下按 level 升序排序
const sortProvidersByLevel = (list: AutomationCard[]) => {
  if (!Array.isArray(list)) return
  list.sort((a, b) => {
    // 第一优先级：启用状态（enabled: true 排在前面）
    if (a.enabled !== b.enabled) {
      return a.enabled ? -1 : 1
    }
    // 第二优先级：Level 升序（1 -> 10）
    return normalizeLevel(a.level) - normalizeLevel(b.level)
  })
}

const modalState = reactive({
  open: false,
  tabId: activeTab,
  editingId: null as number | null,
  form: defaultFormValues(),
  errors: {
    apiUrl: '',
    fallbackApiUrls: '',
    costMultiplier: '',
  },
})

const editingCard = ref<AutomationCard | null>(null)
const apiKeyVisible = ref(false)
const apiKeyChanged = ref(false)
const revealingAPIKey = ref(false)
const apiKeyPlaceholder = computed(() =>
  editingCard.value?.apiKeyConfigured && !apiKeyChanged.value
    ? '********'
    : t('components.main.form.placeholders.apiKey')
)

const handleAPIKeyInput = (value: string) => {
  modalState.form.apiKey = value
  apiKeyChanged.value = true
}

const toggleAPIKeyVisibility = async () => {
  if (apiKeyVisible.value) {
    apiKeyVisible.value = false
    return
  }
  if (editingCard.value?.apiKeyConfigured && !apiKeyChanged.value && !modalState.form.apiKey) {
    revealingAPIKey.value = true
    try {
      modalState.form.apiKey = await RevealProviderAPIKey(modalState.tabId, editingCard.value.id)
    } catch (error) {
      showToast(t('components.main.form.errors.revealApiKeyFailed') + ': ' + extractErrorMessage(error), 'error')
      return
    } finally {
      revealingAPIKey.value = false
    }
  }
  apiKeyVisible.value = true
}

// 认证方式相关状态
const selectedAuthType = ref<string>('bearer')
const customAuthHeader = ref<string>('')
const authTypeOptions = computed(() => [
  { value: 'bearer', label: 'Bearer' },
  { value: 'x-api-key', label: 'X-API-Key' },
])

const applyDeepSeekV4FlashPreset = () => {
  const apiKey = modalState.form.apiKey
  Object.assign(modalState.form, createDeepSeekV4FlashPreset(), { apiKey })
  selectedAuthType.value = 'bearer'
  customAuthHeader.value = ''
  connectivityTestResult.value = null
}

// 上游协议类型选项

const resolveEffectiveAuthType = () =>
  customAuthHeader.value.trim() || selectedAuthType.value || getDefaultAuthType(modalState.tabId)

const confirmState = reactive({ open: false, card: null as AutomationCard | null, tabId: activeTab })

const openCreateModal = () => {
  modalState.tabId = activeTab
  modalState.editingId = null
  editingCard.value = null
  Object.assign(modalState.form, defaultFormValues(activeTab))
  // 初始化认证方式为平台默认
  selectedAuthType.value = getDefaultAuthType(activeTab)
  customAuthHeader.value = ''
  connectivityTestResult.value = null
  apiKeyVisible.value = false
  apiKeyChanged.value = false
  modalState.errors.apiUrl = ''
  modalState.errors.fallbackApiUrls = ''
  modalState.errors.costMultiplier = ''
  modalState.open = true
}

const openEditModal = (card: AutomationCard) => {
  modalState.tabId = activeTab
  modalState.editingId = card.id
  editingCard.value = card
  Object.assign(modalState.form, {
    name: card.name,
    apiUrl: card.apiUrl,
    apiKey: '',
    officialSite: card.officialSite,
    icon: card.icon,
    level: card.level || 1,
    enabled: card.enabled,
    supportedModels: card.supportedModels || {},
    modelMapping: card.modelMapping || {},
    apiEndpoint: card.apiEndpoint || '',
    fallbackApiUrlsText: (card.fallbackApiUrls || []).join('\n'),
    maxConcurrency: card.maxConcurrency || 0,
    costMultiplier: card.costMultiplier && card.costMultiplier > 0 ? card.costMultiplier : 1,
    insecureSkipVerify: card.insecureSkipVerify ?? false,
    requestSanitizeEnabled: card.requestSanitizeEnabled ?? false,
    sanitizeConfig: card.sanitizeConfig || {},
    compatibilityMode: card.compatibilityMode || '',
    // 可用性监控配置（新）- 兼容从旧字段迁移
    availabilityMonitorEnabled:
      card.availabilityMonitorEnabled ?? card.connectivityCheck ?? false,
    connectivityAutoBlacklist: card.connectivityAutoBlacklist ?? false,
    availabilityConfig: {
      testModel:
        card.availabilityConfig?.testModel || card.connectivityTestModel || '',
      testEndpoint:
        card.availabilityConfig?.testEndpoint ||
        card.connectivityTestEndpoint ||
        getDefaultEndpoint(activeTab),
      timeout: card.availabilityConfig?.timeout || 15000,
    },
    // 旧连通性字段不再写入表单
    connectivityCheck: false,
    connectivityTestModel: '',
    connectivityTestEndpoint: '',
    connectivityAuthType: card.connectivityAuthType || '',
  })
  // 初始化认证方式状态
  const storedAuth = (card.connectivityAuthType || '').trim()
  const lower = storedAuth.toLowerCase()
  if (!storedAuth) {
    selectedAuthType.value = getDefaultAuthType(activeTab)
    customAuthHeader.value = ''
  } else if (lower === 'bearer' || lower === 'x-api-key') {
    selectedAuthType.value = lower
    customAuthHeader.value = ''
  } else {
    // 自定义 Header 名
    selectedAuthType.value = getDefaultAuthType(activeTab)
    customAuthHeader.value = storedAuth
  }
  connectivityTestResult.value = null
  apiKeyVisible.value = false
  apiKeyChanged.value = false
  modalState.errors.apiUrl = ''
  modalState.errors.fallbackApiUrls = ''
  modalState.errors.costMultiplier = ''
  modalState.open = true
}

const closeModal = () => {
  modalState.form.apiKey = ''
  apiKeyVisible.value = false
  apiKeyChanged.value = false
  modalState.open = false
}

const closeConfirm = () => {
  confirmState.open = false
  confirmState.card = null
}

const submitModal = async (): Promise<boolean> => {
  const list = cards.value
  const name = modalState.form.name.trim()
  const apiUrl = modalState.form.apiUrl.trim()
  const apiKey = modalState.form.apiKey.trim()
  const officialSite = modalState.form.officialSite.trim()
  const icon = (modalState.form.icon || defaultIconKey).toString().trim().toLowerCase() || defaultIconKey
  const costMultiplier = Number(modalState.form.costMultiplier)
  modalState.errors.apiUrl = ''
  modalState.errors.fallbackApiUrls = ''
  modalState.errors.costMultiplier = ''
  if (!Number.isFinite(costMultiplier) || costMultiplier < 0.01 || costMultiplier > 100) {
    modalState.errors.costMultiplier = t('components.main.form.errors.invalidCostMultiplier')
    return false
  }
  try {
    const parsed = new URL(apiUrl)
    if (!/^https?:/.test(parsed.protocol)) throw new Error('protocol')
  } catch {
    modalState.errors.apiUrl = t('components.main.form.errors.invalidUrl')
    return false
  }

  // 备用地址：按行拆分、去重、上限 4，逐条校验 http/https 绝对地址
  let fallbackApiUrls: string[] | undefined
  const lines = (modalState.form.fallbackApiUrlsText || '')
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
  const deduped = Array.from(new Set(lines))
  if (deduped.length > 4) {
    modalState.errors.fallbackApiUrls = t('components.main.form.errors.tooManyFallbacks')
    return false
  }
  for (const u of deduped) {
    try {
      const parsed = new URL(u)
      if (!/^https?:/.test(parsed.protocol)) throw new Error('protocol')
    } catch {
      modalState.errors.fallbackApiUrls = t('components.main.form.errors.invalidFallbackUrl')
      return false
    }
  }
  fallbackApiUrls = deduped.length > 0 ? deduped : undefined

  if (editingCard.value) {
    if (name && name !== editingCard.value.name) {
      try {
        await RenameProvider(modalState.tabId, editingCard.value.id, name)
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        showToast(msg || 'Rename failed', 'error')
        return false
      }
    }

    // 仅当 level 变化时才重新排序，避免破坏同级拖拽顺序
    const prevLevel = normalizeLevel(editingCard.value.level)
    const nextLevel = normalizeLevel(modalState.form.level)
    Object.assign(editingCard.value, {
      name: name || editingCard.value.name,
      apiUrl: apiUrl || editingCard.value.apiUrl,
      apiKey: apiKeyChanged.value ? apiKey : '',
      apiKeyConfigured: apiKeyChanged.value ? apiKey !== '' : !!editingCard.value.apiKeyConfigured,
      apiKeyChanged: apiKeyChanged.value,
      officialSite,
      icon,
      level: nextLevel,
      enabled: modalState.form.enabled,
      supportedModels: modalState.form.supportedModels || {},
      modelMapping: modalState.form.modelMapping || {},
      apiEndpoint: modalState.form.apiEndpoint || '',
      fallbackApiUrls,
      maxConcurrency: normalizeMaxConcurrency(modalState.form.maxConcurrency),
      costMultiplier,
      insecureSkipVerify: !!modalState.form.insecureSkipVerify,
      requestSanitizeEnabled: !!modalState.form.requestSanitizeEnabled,
      sanitizeConfig: modalState.form.sanitizeConfig || undefined,
      compatibilityMode: modalState.form.compatibilityMode || '',
      // 可用性监控配置（新）
      availabilityMonitorEnabled: !!modalState.form.availabilityMonitorEnabled,
      connectivityAutoBlacklist: !!modalState.form.connectivityAutoBlacklist,
      availabilityConfig: {
        testModel: modalState.form.availabilityConfig?.testModel || '',
        testEndpoint:
          modalState.form.availabilityConfig?.testEndpoint ||
          getDefaultEndpoint(modalState.tabId),
        timeout: modalState.form.availabilityConfig?.timeout || 15000,
      },
      // 旧连通性字段清空（避免再次写入）
      connectivityCheck: false,
      connectivityTestModel: '',
      connectivityTestEndpoint: '',
      connectivityAuthType: resolveEffectiveAuthType(),
    })
    if (prevLevel !== nextLevel) {
      sortProvidersByLevel(list)
    }
    const saveResult = await persistProviders()
    if (!saveResult.ok) {
      // 保存失败，不关闭弹窗，让用户修正配置
      return false
    }
  } else {
    const newCard: AutomationCard = {
      id: Date.now(),
      name: name || 'Untitled vendor',
      apiUrl,
      apiKey,
      apiKeyConfigured: apiKey !== '',
      apiKeyChanged: true,
      officialSite,
      icon,
      accent: '#0a84ff',
      tint: 'rgba(15, 23, 42, 0.12)',
      level: normalizeLevel(modalState.form.level),
      enabled: modalState.form.enabled,
      supportedModels: modalState.form.supportedModels || {},
      modelMapping: modalState.form.modelMapping || {},
      apiEndpoint: modalState.form.apiEndpoint || '',
      fallbackApiUrls,
      maxConcurrency: normalizeMaxConcurrency(modalState.form.maxConcurrency),
      costMultiplier,
      insecureSkipVerify: !!modalState.form.insecureSkipVerify,
      requestSanitizeEnabled: !!modalState.form.requestSanitizeEnabled,
      sanitizeConfig: modalState.form.sanitizeConfig || undefined,
      compatibilityMode: modalState.form.compatibilityMode || '',
      // 可用性监控配置（新）
      availabilityMonitorEnabled: !!modalState.form.availabilityMonitorEnabled,
      connectivityAutoBlacklist: !!modalState.form.connectivityAutoBlacklist,
      availabilityConfig: {
        testModel: modalState.form.availabilityConfig?.testModel || '',
        testEndpoint:
          modalState.form.availabilityConfig?.testEndpoint ||
          getDefaultEndpoint(modalState.tabId),
        timeout: modalState.form.availabilityConfig?.timeout || 15000,
      },
      // 旧连通性字段清空
      connectivityCheck: false,
      connectivityTestModel: '',
      connectivityTestEndpoint: '',
      connectivityAuthType: resolveEffectiveAuthType(),
    }
    list.push(newCard)
    sortProvidersByLevel(list)
    const saveResult = await persistProviders()
    if (!saveResult.ok) {
      // 保存失败，从列表中移除刚添加的卡片，不关闭弹窗
      const idx = list.indexOf(newCard)
      if (idx !== -1) list.splice(idx, 1)
      return false
    }
  }

  await loadProvidersFromDisk()
  closeModal()

  // 通知可用性页面刷新
  window.dispatchEvent(new CustomEvent('providers-updated'))
  void loadProviderStats(activeTab)
  return true
}

const configure = (card: AutomationCard) => {
  openEditModal(card)
}

const remove = async (id: number) => {
  const list = cards.value
  const index = list.findIndex((card) => card.id === id)
  if (index > -1) {
    const [removed] = list.splice(index, 1)
    const saveResult = await persistProviders()
    if (!saveResult.ok) {
      // 保存失败时把被删卡片放回原位，避免界面与磁盘状态分叉（与新建路径的失败回滚保持一致）
      list.splice(Math.min(index, list.length), 0, removed)
    }
  }
}

const requestRemove = (card: AutomationCard) => {
  confirmState.card = card
  confirmState.tabId = activeTab
  confirmState.open = true
}

// 复制供应商
const handleDuplicate = async (card: AutomationCard) => {
  try {
    const tab = activeTab
    const newProvider = await DuplicateProvider(tab, card.id)
    if (!newProvider) {
      console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
      showToast(t('components.main.controls.duplicateFailed'), 'error')
      return
    }
    console.log(`[Duplicate] Provider "${card.name}" duplicated as "${newProvider.name}"`)
    await loadProvidersFromDisk()
  } catch (error) {
    console.error('[Duplicate] Failed to duplicate provider:', error)
    showToast(t('components.main.controls.duplicateFailed') + ': ' + extractErrorMessage(error), 'error')
  }
}

const confirmRemove = async () => {
  if (!confirmState.card) return
  await remove(confirmState.card.id)
  closeConfirm()
}

const onDragStart = (id: number) => {
  draggingId.value = id
}

// 拖拽指示器状态：目标卡片 + 落点在其前/后
const dropIndicator = reactive<{ id: number | null; before: boolean }>({ id: null, before: false })

// 只允许在同一调度组（启用状态 + Level 相同）内重排：跨组落点在
// 重载后的稳定排序（启用优先、Level 升序）下必然回弹，指示器不能撒谎
const sameDragGroup = (a: AutomationCard, b: AutomationCard) =>
  a.enabled === b.enabled && normalizeLevel(a.level) === normalizeLevel(b.level)

const onCardDragOver = (card: AutomationCard, event: DragEvent) => {
  if (draggingId.value === null) return
  const list = cards.value
  const dragging = list?.find(c => c.id === draggingId.value)
  if (!dragging || !list) return
  if (card.id === dragging.id || !sameDragGroup(dragging, card)) {
    // 无条件清掉指示器：从合法目标滑到非法目标时，残留的旧指示线会撒谎
    dropIndicator.id = null
    return
  }
  // 只有合法落点才 preventDefault（标记可放置）；非法落点浏览器显示禁止光标
  event.preventDefault()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  dropIndicator.id = card.id
  dropIndicator.before = event.clientY < rect.top + rect.height / 2
}

const clearDragState = () => {
  draggingId.value = null
  dropIndicator.id = null
}

// 拖拽保存串行链：载荷一律在入队时刻同步定格（即拖拽刚落下的乐观状态，
// 不受后续编辑/重载影响）；修订号按保存目标（tab / 自定义工具）分别计数，
// 失败回滚只在"该目标没有更新的拖拽"且目标未被切换时执行。
// 直接保存（persistProviders）与磁盘重载（loadProvidersFromDisk）都会等链静止
let dragPersistChain: Promise<void> = Promise.resolve()
const dragPersistRevisions = new Map<string, number>()
// dragPersistEnqueues 全局入队计数：loadProvidersFromDisk 用它检测
// "读盘期间又有新拖拽"并整体重来
let dragPersistEnqueues = 0

const queueDragPersist = (tabId: ProviderTab) => {
  const targetKey = tabId
  if (!targetKey) return
  const revision = (dragPersistRevisions.get(targetKey) ?? 0) + 1
  dragPersistRevisions.set(targetKey, revision)
  dragPersistEnqueues++

  const payload = JSON.parse(JSON.stringify(serializeProviders(cards.value)))

  const job = async () => {
    await SaveProviders(targetKey, payload)
  }

  dragPersistChain = dragPersistChain.then(job).catch(async (error) => {
    console.error('Failed to persist drag order', error)
    showToast(t('components.main.form.saveFailed') + ': ' + extractErrorMessage(error), 'error')
    if (dragPersistRevisions.get(targetKey) !== revision) return
    try {
      const saved = await LoadProviders(targetKey)
      if (dragPersistRevisions.get(targetKey) !== revision) return
      if (Array.isArray(saved)) {
        cards.value = createAutomationCards(saved as AutomationCard[])
        sortProvidersByLevel(cards.value)
      }
    } catch (reloadError) {
      console.error('Failed to restore order from disk', reloadError)
    }
  })
}

const onDrop = (targetId: number, event: DragEvent) => {
  event.preventDefault()
  if (draggingId.value === null || dropIndicator.id === null) {
    clearDragState()
    return
  }
  const currentTab = activeTab
  const list = cards.value
  const fromIndex = list.findIndex(card => card.id === draggingId.value)
  const targetIndex = list.findIndex(card => card.id === (dropIndicator.id ?? targetId))
  if (fromIndex === -1 || targetIndex === -1 || fromIndex === targetIndex) {
    clearDragState()
    return
  }
  // 显式落点插入：目标前 = 目标下标，目标后 = 目标下标 + 1；
  // 移除拖拽项后若目标在其后方，插入点整体左移一位。
  // （旧实现固定 toIndex-1，把"拖到下一张卡"变成无操作）
  let insertIndex = targetIndex + (dropIndicator.before ? 0 : 1)
  const [moved] = list.splice(fromIndex, 1)
  if (fromIndex < insertIndex) insertIndex--
  list.splice(insertIndex, 0, moved)
  clearDragState()
  queueDragPersist(currentTab)
}

const onDragEnd = () => {
  clearDragState()
}

const iconSvg = (name: string) => {
  if (!name) return ''
  return lobeIcons[name.toLowerCase()] ?? ''
}

const vendorInitials = (name: string) => {
  if (!name) return 'AI'
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}


</script>

<style scoped>
.api-key-control {
  display: flex;
  align-items: stretch;
  gap: 8px;
}

.api-key-control :deep(.base-input) {
  min-width: 0;
  flex: 1;
}

.api-key-visibility {
  width: 40px;
  min-width: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  cursor: pointer;
}

.api-key-visibility:hover:not(:disabled) {
  color: var(--mac-text);
  border-color: var(--mac-accent);
}

.api-key-visibility:disabled {
  cursor: wait;
  opacity: 0.55;
}

.api-key-visibility svg {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.provider-preset-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 36px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--mac-border);
}

.provider-preset-label {
  flex: 0 0 auto;
  color: var(--mac-text-secondary);
  font-size: 12px;
  font-weight: 600;
}

.provider-preset-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-width: 172px;
  height: 34px;
  padding: 0 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.provider-preset-button:hover {
  border-color: var(--mac-accent);
  background: var(--mac-surface-strong);
}

.provider-preset-button:focus-visible {
  outline: 2px solid var(--mac-accent);
  outline-offset: 2px;
}

.provider-preset-icon {
  display: inline-flex;
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
}

.provider-preset-icon :deep(svg) {
  width: 100%;
  height: 100%;
}

@media (max-width: 520px) {
  .provider-preset-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .provider-preset-button {
    width: 100%;
  }
}

.fallback-urls-input {
  width: 100%;
  padding: 8px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  font-family: monospace;
  resize: vertical;
  transition: border-color 0.2s;
}

.fallback-urls-input:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.fallback-urls-input.has-error {
  border-color: #ef4444;
}

/* 正在使用的供应商卡片样式 */
/* @author sm */
.automation-card.is-last-used {
  position: relative;
  border: 2px solid rgb(16, 185, 129);
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.3);
}

/* 正在使用标签 */
.last-used-badge {
  position: absolute;
  top: -10px;
  right: 12px;
  background: rgb(16, 185, 129);
  color: white;
  font-size: 10px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  z-index: 1;
}

/* 高亮闪烁的供应商卡片（切换/拉黑时） */
.automation-card.is-highlighted {
  animation: highlight-pulse 0.6s ease-in-out 3;
  border-color: rgb(245, 158, 11);
  box-shadow: 0 0 12px rgba(245, 158, 11, 0.5);
}

@keyframes highlight-pulse {
  0%, 100% {
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.3);
  }
  50% {
    box-shadow: 0 0 20px rgba(245, 158, 11, 0.7);
  }
}

/* 暗色模式适配 */
:global(.dark) .automation-card.is-last-used {
  border-color: rgb(52, 211, 153);
  box-shadow: 0 0 8px rgba(52, 211, 153, 0.3);
}

:global(.dark) .last-used-badge {
  background: rgb(52, 211, 153);
  color: rgb(6, 78, 59);
}

:global(.dark) .automation-card.is-highlighted {
  border-color: rgb(251, 191, 36);
  box-shadow: 0 0 12px rgba(251, 191, 36, 0.5);
}

/* Level Badge 样式 */
.level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  text-align: center;
  transition: all 0.2s ease;
}

/* Card title row badge 定位 */
.card-title-row .level-badge {
  margin-left: 8px;
}

/* 黑名单等级徽章与调度等级徽章的间距 */
.card-title-row .blacklist-level-badge {
  margin-left: 4px;
}

/* Level 配色方案：从绿色（高优先级）到红色（低优先级）*/
.level-badge.level-1 {
  background: rgba(16, 185, 129, 0.12);
  color: rgb(5, 150, 105);
}

.level-badge.level-2 {
  background: rgba(34, 197, 94, 0.12);
  color: rgb(22, 163, 74);
}

.level-badge.level-3 {
  background: rgba(132, 204, 22, 0.12);
  color: rgb(101, 163, 13);
}

.level-badge.level-4 {
  background: rgba(234, 179, 8, 0.12);
  color: rgb(161, 98, 7);
}

.level-badge.level-5 {
  background: rgba(245, 158, 11, 0.12);
  color: rgb(180, 83, 9);
}

.level-badge.level-6 {
  background: rgba(249, 115, 22, 0.12);
  color: rgb(194, 65, 12);
}

.level-badge.level-7 {
  background: rgba(239, 68, 68, 0.12);
  color: rgb(185, 28, 28);
}

.level-badge.level-8 {
  background: rgba(220, 38, 38, 0.12);
  color: rgb(153, 27, 27);
}

.level-badge.level-9 {
  background: rgba(190, 18, 60, 0.12);
  color: rgb(136, 19, 55);
}

.level-badge.level-10 {
  background: rgba(159, 18, 57, 0.12);
  color: rgb(112, 26, 52);
}

/* 暗色模式适配 */
:global(.dark) .level-badge.level-1 {
  background: rgba(16, 185, 129, 0.18);
  color: rgb(52, 211, 153);
}

:global(.dark) .level-badge.level-2 {
  background: rgba(34, 197, 94, 0.18);
  color: rgb(74, 222, 128);
}

:global(.dark) .level-badge.level-3 {
  background: rgba(132, 204, 22, 0.18);
  color: rgb(163, 230, 53);
}

:global(.dark) .level-badge.level-4 {
  background: rgba(234, 179, 8, 0.18);
  color: rgb(250, 204, 21);
}

:global(.dark) .level-badge.level-5 {
  background: rgba(245, 158, 11, 0.18);
  color: rgb(251, 191, 36);
}

:global(.dark) .level-badge.level-6 {
  background: rgba(249, 115, 22, 0.18);
  color: rgb(251, 146, 60);
}

:global(.dark) .level-badge.level-7 {
  background: rgba(239, 68, 68, 0.18);
  color: rgb(248, 113, 113);
}

:global(.dark) .level-badge.level-8 {
  background: rgba(220, 38, 38, 0.18);
  color: rgb(239, 68, 68);
}

:global(.dark) .level-badge.level-9 {
  background: rgba(190, 18, 60, 0.18);
  color: rgb(244, 63, 94);
}

:global(.dark) .level-badge.level-10 {
  background: rgba(159, 18, 57, 0.18);
  color: rgb(236, 72, 153);
}

/* Level Select Dropdown 样式 */
.level-select {
  position: relative;
}

.level-select-button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  font-size: 14px;
  color: var(--color-text-primary);
  cursor: pointer;
  transition: all 0.2s ease;
}

.level-select-button:hover {
  border-color: var(--color-border-hover);
  background: var(--color-bg-tertiary);
}

.level-select-button:focus {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.level-select-button svg {
  width: 16px;
  height: 16px;
  margin-left: auto;
  opacity: 0.5;
}

.level-label {
  flex: 1;
  text-align: left;
}

.level-select-options {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 280px;
  overflow-y: auto;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  z-index: 50;
  padding: 4px;
}

:global(.dark) .level-select-options {
  background: var(--mac-surface);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

.level-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.level-option:hover,
.level-option.active {
  background: var(--mac-surface-strong);
}

.level-option.selected {
  background: rgba(10, 132, 255, 0.12); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-accent) 12%, transparent);
  font-weight: 500;
}

.level-option .level-name {
  flex: 1;
  font-size: 14px;
  color: var(--mac-text);
}

.level-option.selected .level-name {
  color: var(--mac-accent);
}

/* 黑名单横幅 */
.blacklist-banner {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  margin-top: 8px;
  background: rgba(239, 68, 68, 0.1);
  border-left: 3px solid #ef4444;
  border-radius: 6px;
  font-size: 13px;
  color: #dc2626;
}

.blacklist-banner.dark {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

.blacklist-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.blacklist-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.blacklist-text {
  flex: 1;
  font-weight: 500;
}

.blacklist-actions {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unblock-btn {
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 500;
  color: #fff;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
}

.unblock-btn.primary {
  background: #ef4444;
  flex: 1;
}

.unblock-btn.primary:hover {
  background: #dc2626;
}

.unblock-btn.secondary {
  background: #6b7280;
  flex: 1;
}

.unblock-btn.secondary:hover {
  background: #4b5563;
}

.unblock-btn:active {
  transform: scale(0.98);
}

/* 等级徽章（黑名单模式：黑色/红色） */
.blacklist-banner .level-badge,
.level-badge-standalone .level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  min-width: 28px;
  font-size: 11px;
  font-weight: 700;
  border-radius: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  line-height: 1;
  flex-shrink: 0;
  text-align: center;
}

.blacklist-banner .level-badge.level-1,
.level-badge-standalone .level-badge.level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-banner .level-badge.level-2,
.level-badge-standalone .level-badge.level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-banner .level-badge.level-3,
.level-badge-standalone .level-badge.level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-banner .level-badge.level-4,
.level-badge-standalone .level-badge.level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-banner .level-badge.level-5,
.level-badge-standalone .level-badge.level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-banner .level-badge.dark.level-1,
.level-badge-standalone .level-badge.dark.level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-banner .level-badge.dark.level-2,
.level-badge-standalone .level-badge.dark.level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-banner .level-badge.dark.level-3,
.level-badge-standalone .level-badge.dark.level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-banner .level-badge.dark.level-4,
.level-badge-standalone .level-badge.dark.level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-banner .level-badge.dark.level-5,
.level-badge-standalone .level-badge.dark.level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 独立等级徽章（未拉黑但有等级） */
.level-badge-standalone {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin-top: 8px;
  background: rgba(156, 163, 175, 0.1);
  border-left: 3px solid #9ca3af;
  border-radius: 6px;
  font-size: 12px;
  color: #6b7280;
}

.level-hint {
  flex: 1;
  font-weight: 500;
}

.reset-level-mini {
  padding: 2px 6px;
  font-size: 11px;
  font-weight: 700;
  color: #6b7280;
  background: transparent;
  border: 1px solid #d1d5db;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.2s;
  line-height: 1;
}

.reset-level-mini:hover {
  background: #f3f4f6;
  color: #374151;
  border-color: #9ca3af;
}

.reset-level-mini:active {
  transform: scale(0.95);
}

/* 黑名单等级徽章（卡片标题行） */
.blacklist-level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  transition: all 0.2s ease;
  margin-left: 4px;
}

.blacklist-level-badge.bl-level-0 {
  background: #e5e7eb;
  color: #6b7280;
}

.blacklist-level-badge.bl-level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-level-badge.bl-level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-level-badge.bl-level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-level-badge.bl-level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-level-badge.bl-level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-level-badge.dark.bl-level-0 {
  background: rgba(107, 114, 128, 0.2);
  color: #9ca3af;
}

.blacklist-level-badge.dark.bl-level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-level-badge.dark.bl-level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-level-badge.dark.bl-level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-level-badge.dark.bl-level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-level-badge.dark.bl-level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 连通性状态指示器 */
.connectivity-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-left: 6px;
  flex-shrink: 0;
  transition: background-color 0.2s ease;
}

.connectivity-dot.connectivity-green {
  background-color: #22c55e;
  box-shadow: 0 0 4px rgba(34, 197, 94, 0.5);
}

.connectivity-dot.connectivity-yellow {
  background-color: #eab308;
  box-shadow: 0 0 4px rgba(234, 179, 8, 0.5);
}

.connectivity-dot.connectivity-red {
  background-color: #ef4444;
  box-shadow: 0 0 4px rgba(239, 68, 68, 0.5);
}

.connectivity-dot.connectivity-gray {
  background-color: #9ca3af;
}

:global(.dark) .connectivity-dot.connectivity-green {
  background-color: #4ade80;
  box-shadow: 0 0 6px rgba(74, 222, 128, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-yellow {
  background-color: #facc15;
  box-shadow: 0 0 6px rgba(250, 204, 21, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-red {
  background-color: #f87171;
  box-shadow: 0 0 6px rgba(248, 113, 113, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-gray {
  background-color: #6b7280;
}

/* 测试连通性按钮 */
.test-connectivity-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  padding: 10px 16px;
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.test-connectivity-btn:hover:not(:disabled) {
  filter: brightness(1.1);
}

.test-connectivity-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.test-result {
  margin-top: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
}

.test-result.success {
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
  border-left: 3px solid #22c55e;
}

.test-result.error {
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  border-left: 3px solid #ef4444;
}

:global(.dark) .test-result.success {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

:global(.dark) .test-result.error {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

</style>
