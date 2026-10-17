<script setup lang="ts">
import { computed } from 'vue'

import {
  appFontPresets,
  editorFontPresets,
  type EditorTokenKey,
  tableFontPresets,
  themePresets,
} from '@/stores/themePresets'
import { useSettingsStore, type FontsSurface } from '@/stores/settings'

const settings = useSettingsStore()
const snap = computed(() => settings.snapshot)

const systemThemes = computed(() => themePresets.filter((p) => p.group === 'system'))
const builtinThemes = computed(() => themePresets.filter((p) => p.group === 'builtin'))

const surfaces: { id: FontsSurface; label: string }[] = [
  { id: 'editor', label: 'YAML & Code Editor' },
  { id: 'table', label: 'Resource Table' },
  { id: 'application', label: 'Application' },
]

const typography = computed(() => {
  if (settings.fontsSurface === 'editor') return snap.value.editorTypography
  if (settings.fontsSurface === 'table') return snap.value.tableTypography
  return snap.value.appTypography
})

const fontPresets = computed(() => {
  if (settings.fontsSurface === 'editor') return editorFontPresets
  if (settings.fontsSurface === 'table') return tableFontPresets
  return appFontPresets
})

function setFontPreset(id: string) {
  const target = settings.fontsSurface
  if (target === 'editor') snap.value.editorTypography.fontPresetId = id
  else if (target === 'table') snap.value.tableTypography.fontPresetId = id
  else snap.value.appTypography.fontPresetId = id
  settings.apply()
}

function bumpFontSize(delta: number) {
  const t = typography.value
  const min = settings.fontsSurface === 'application' ? 12 : settings.fontsSurface === 'table' ? 11 : 10
  const max = settings.fontsSurface === 'application' ? 16 : settings.fontsSurface === 'table' ? 20 : 28
  t.fontSize = Math.min(max, Math.max(min, t.fontSize + delta))
  settings.apply()
}

function bumpLineHeight(delta: number) {
  const t = typography.value
  t.lineHeight = Math.min(2.2, Math.max(1, Math.round((t.lineHeight + delta) * 10) / 10))
  settings.apply()
}

const tokenEntries = computed(() =>
  (Object.keys(settings.editorTokenLabels) as EditorTokenKey[]).map((key) => ({
    key,
    label: settings.editorTokenLabels[key],
    value: settings.editorTokens[key],
  })),
)

function onColorInput(key: EditorTokenKey, event: Event) {
  const value = (event.target as HTMLInputElement).value
  settings.setTokenOverride(key, value.replace('#', ''))
}
</script>

<template>
  <div class="grid h-full min-h-[460px] grid-cols-[13.5rem_1fr] gap-4">
    <!-- Left theme list card (TablePlus style) -->
    <aside class="flex min-h-0 flex-col overflow-y-auto rounded-xl border border-line bg-surface-2/60 p-3">
      <div>
        <p class="mb-2 px-1 text-[10px] font-semibold uppercase tracking-wider text-ink-faint">System</p>
        <ul class="space-y-1">
          <li v-for="theme in systemThemes" :key="theme.id">
            <button
              type="button"
              class="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-xs transition-colors"
              :class="settings.themePreset === theme.id ? 'bg-surface-3 text-ink ring-1 ring-line-strong' : 'text-ink-muted hover:bg-surface-2 hover:text-ink'"
              @click="settings.setThemePreset(theme.id)"
            >
              <!-- Swatch -->
              <span
                v-if="theme.id === 'auto'"
                class="relative size-6 shrink-0 overflow-hidden rounded border border-line"
                style="background: linear-gradient(135deg, #1c1d1f 50%, #ffffff 50%)"
              >
                <span class="mt-0.5 ml-0.5 flex gap-0.5">
                  <span class="size-1 rounded-full bg-bad" />
                  <span class="size-1 rounded-full bg-warn" />
                  <span class="size-1 rounded-full bg-ok" />
                </span>
              </span>
              <span
                v-else
                class="size-6 shrink-0 rounded border border-line p-1"
                :style="{ background: theme.swatch[0] }"
              >
                <span class="flex gap-0.5">
                  <span class="size-1 rounded-full bg-bad" />
                  <span class="size-1 rounded-full bg-warn" />
                  <span class="size-1 rounded-full bg-ok" />
                </span>
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate font-medium">{{ theme.label }}</span>
                <span class="block truncate text-[10px] text-ink-faint">{{ theme.description }}</span>
              </span>
            </button>
          </li>
        </ul>
      </div>

      <div class="mt-4">
        <p class="mb-2 px-1 text-[10px] font-semibold uppercase tracking-wider text-ink-faint">Built-in</p>
        <ul class="space-y-1">
          <li v-for="theme in builtinThemes" :key="theme.id">
            <button
              type="button"
              class="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-xs transition-colors"
              :class="settings.themePreset === theme.id ? 'bg-surface-3 text-ink ring-1 ring-line-strong' : 'text-ink-muted hover:bg-surface-2 hover:text-ink'"
              @click="settings.setThemePreset(theme.id)"
            >
              <span class="size-6 shrink-0 rounded border border-line p-1" :style="{ background: theme.swatch[0] }">
                <span class="flex gap-0.5">
                  <span class="size-1 rounded-full bg-bad" />
                  <span class="size-1 rounded-full bg-warn" />
                  <span class="size-1 rounded-full bg-ok" />
                </span>
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate font-medium">{{ theme.label }}</span>
                <span class="block truncate text-[10px] text-ink-faint">{{ theme.description }}</span>
              </span>
            </button>
          </li>
        </ul>
      </div>
    </aside>

    <!-- Right configuration & preview card -->
    <div class="flex min-h-0 min-w-0 flex-col gap-3.5 overflow-y-auto rounded-xl border border-line bg-surface-2/60 p-4">
      <div class="flex justify-center">
        <div class="inline-flex rounded-xl border border-line bg-surface-1 p-1">
          <button
            v-for="surface in surfaces"
            :key="surface.id"
            type="button"
            class="rounded-lg px-3.5 py-1 text-xs font-medium transition-colors"
            :class="
              settings.fontsSurface === surface.id
                ? 'bg-brand text-surface-1 shadow-xs'
                : 'text-ink-muted hover:text-ink'
            "
            @click="settings.fontsSurface = surface.id"
          >
            {{ surface.label }}
          </button>
        </div>
      </div>

      <div class="grid grid-cols-[5.5rem_1fr_auto] items-center gap-x-3 gap-y-2.5 text-xs">
        <span class="text-right text-ink-muted">Font</span>
        <select
          class="w-full rounded-lg border border-line bg-surface-1 px-2.5 py-1.5 text-xs text-ink outline-none focus:border-brand"
          :value="typography.fontPresetId"
          @change="setFontPreset(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="preset in fontPresets" :key="preset.id" :value="preset.id">{{ preset.label }}</option>
        </select>
        <div class="flex items-center gap-1">
          <button
            type="button"
            class="rounded-lg border border-line bg-surface-1 px-2 py-1 text-xs text-ink-muted hover:text-ink"
            @click="bumpFontSize(-1)"
          >
            −
          </button>
          <span class="w-9 text-center font-mono text-xs text-ink">{{ typography.fontSize }}</span>
          <button
            type="button"
            class="rounded-lg border border-line bg-surface-1 px-2 py-1 text-xs text-ink-muted hover:text-ink"
            @click="bumpFontSize(1)"
          >
            +
          </button>
        </div>

        <span class="text-right text-ink-muted">Line Height</span>
        <div class="col-span-2 flex items-center gap-2">
          <div class="flex-1 rounded-lg border border-line bg-surface-1 px-3 py-1.5 text-right font-mono text-xs text-ink">
            {{ typography.lineHeight.toFixed(1) }}
          </div>
          <div class="flex items-center gap-1">
            <button
              type="button"
              class="rounded-lg border border-line bg-surface-1 px-2 py-1 text-xs text-ink-muted hover:text-ink"
              @click="bumpLineHeight(-0.1)"
            >
              −
            </button>
            <button
              type="button"
              class="rounded-lg border border-line bg-surface-1 px-2 py-1 text-xs text-ink-muted hover:text-ink"
              @click="bumpLineHeight(0.1)"
            >
              +
            </button>
          </div>
        </div>
      </div>

      <!-- Live preview + color tokens unified container -->
      <div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-line bg-surface-1">
        <div
          class="shrink-0 border-b border-line px-4 py-3"
          :style="{
            fontFamily:
              settings.fontsSurface === 'editor'
                ? settings.editorFontFamily()
                : settings.fontsSurface === 'table'
                  ? settings.tableFontFamily() || 'inherit'
                  : settings.appFontFamily(),
            fontSize: `${typography.fontSize}px`,
            lineHeight: typography.lineHeight,
            background:
              settings.fontsSurface === 'editor'
                ? `#${settings.editorTokens.editorBackground}`
                : 'var(--color-surface-1)',
            color:
              settings.fontsSurface === 'editor'
                ? `#${settings.editorTokens.editorForeground}`
                : 'var(--color-ink)',
          }"
        >
          <template v-if="settings.fontsSurface === 'editor'">
            <p>
              <span :style="{ color: `#${settings.editorTokens.commentText}` }"># Sample Kubernetes manifest</span>
            </p>
            <p>
              <span :style="{ color: `#${settings.editorTokens.keywordText}` }">apiVersion</span>:
              <span :style="{ color: `#${settings.editorTokens.singleQuotedText}` }">'apps/v1'</span>
            </p>
            <p>
              <span :style="{ color: `#${settings.editorTokens.keywordText}` }">kind</span>:
              <span :style="{ color: `#${settings.editorTokens.doubleQuotedText}` }">"Deployment"</span>
            </p>
            <p>
              <span :style="{ color: `#${settings.editorTokens.identifierText}` }">replicas</span>:
              <span :style="{ color: `#${settings.editorTokens.numberText}` }">3</span>
            </p>
          </template>
          <template v-else-if="settings.fontsSurface === 'table'">
            <table class="w-full text-left">
              <thead class="text-ink-faint">
                <tr>
                  <th class="pb-1">Name</th>
                  <th class="pb-1">Namespace</th>
                  <th class="pb-1">Status</th>
                </tr>
              </thead>
              <tbody class="text-ink-muted">
                <tr>
                  <td class="text-ink">api-gateway-7d9f8b</td>
                  <td>production</td>
                  <td :style="{ color: `var(--color-ok)` }">Running</td>
                </tr>
                <tr>
                  <td class="text-ink">worker-queue-4c2a1</td>
                  <td>staging</td>
                  <td>Pending</td>
                </tr>
              </tbody>
            </table>
          </template>
          <template v-else>
            <div class="flex flex-wrap items-center gap-2">
              <span class="rounded-lg bg-brand/20 px-2 py-0.5 text-xs text-ink">Production</span>
              <button type="button" class="rounded-lg border border-line px-2.5 py-1 text-xs">Inspect</button>
              <button type="button" class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1">
                Connect
              </button>
            </div>
            <p class="mt-2 text-ink-muted">Application chrome preview with surfaces and brand accents.</p>
          </template>
        </div>

        <div class="flex min-h-0 flex-1 flex-col">
          <div class="flex shrink-0 items-center justify-between border-b border-line/60 px-4 py-2">
            <p class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Color tokens</p>
            <button
              type="button"
              class="text-xs text-brand hover:text-brand-strong"
              @click="settings.resetTokenOverrides()"
            >
              Reset to theme defaults
            </button>
          </div>
          <ul class="min-h-0 flex-1 divide-y divide-line/40 overflow-y-auto px-4 py-1">
            <li
              v-for="entry in tokenEntries"
              :key="entry.key"
              class="flex items-center justify-between gap-3 py-2"
            >
              <span class="text-xs text-ink">{{ entry.label }}</span>
              <span class="flex items-center gap-3">
                <span class="font-mono text-xs uppercase text-warn">{{ entry.value }}</span>
                <label
                  class="relative block h-5 w-11 cursor-pointer rounded-full border border-line-strong shadow-inner"
                  :style="{ backgroundColor: `#${entry.value}` }"
                >
                  <input
                    type="color"
                    class="sr-only"
                    :value="`#${entry.value}`"
                    @input="onColorInput(entry.key, $event)"
                  />
                </label>
              </span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
