<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import ResourceAuthoring from '@/components/settings/ResourceAuthoring.vue'
import SettingsFontsThemes from '@/components/settings/SettingsFontsThemes.vue'
import SettingsLocations from '@/components/settings/SettingsLocations.vue'
import { shortcutCatalog } from '@/composables/shortcutCatalog'
import { api, message } from '@/api'
import { useSettingsStore, type SettingsTab } from '@/stores/settings'
import { useUIStore } from '@/stores/ui'

defineProps<{ embedded?: boolean }>()
const emit = defineEmits<{ close: [] }>()

const settings = useSettingsStore()
const ui = useUIStore()
const snap = computed(() => settings.snapshot)

const version = ref('')
const accessInstalled = ref(false)
const checking = ref(false)
const keymapSearch = ref('')

const tabs: { id: SettingsTab; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'table', label: 'Table' },
  { id: 'editor', label: 'Editor' },
  { id: 'fonts', label: 'Fonts & Themes' },
  { id: 'locations', label: 'Locations' },
  { id: 'authoring', label: 'Authoring' },
  { id: 'keymap', label: 'Keymap' },
]

const filteredKeymap = computed(() => {
  const term = keymapSearch.value.trim().toLowerCase()
  if (!term) return shortcutCatalog
  return shortcutCatalog
    .map((group) => ({
      ...group,
      items: group.items.filter(
        (item) => item.label.toLowerCase().includes(term) || item.keys.toLowerCase().includes(term),
      ),
    }))
    .filter((group) => group.items.length)
})

async function loadMeta() {
  try {
    ;[version.value, accessInstalled.value] = await Promise.all([api.version(), api.accessInstalled()])
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function checkForUpdate() {
  checking.value = true
  try {
    await api.checkForUpdate()
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    checking.value = false
  }
}

onMounted(loadMeta)
</script>

<template>
  <!-- Top TablePlus-style Icon Toolbar -->
  <header class="relative flex shrink-0 items-center justify-center border-b border-line bg-surface-2/80 px-4 py-2.5">
    <!-- Window traffic-light style close button on the left + Esc on the right -->
    <div class="absolute left-4 top-3 flex items-center gap-2">
      <button
        type="button"
        class="group flex size-3.5 items-center justify-center rounded-full bg-bad/80 text-[9px] font-bold text-black/80 hover:bg-bad"
        title="Close Settings (Esc)"
        aria-label="Close Settings"
        @click="emit('close')"
      >
        <span class="opacity-0 group-hover:opacity-100">×</span>
      </button>
    </div>

    <nav class="flex flex-wrap items-center justify-center gap-1.5" aria-label="Settings sections">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        type="button"
        class="flex min-w-[4.75rem] flex-col items-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-medium transition-colors"
        :class="
          settings.activeTab === tab.id
            ? 'border-brand/70 bg-surface-3 text-brand shadow-xs'
            : 'border-transparent text-ink-muted hover:bg-surface-3/50 hover:text-ink'
        "
        @click="settings.activeTab = tab.id"
      >
        <!-- General: Sliders icon -->
        <svg v-if="tab.id === 'general'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
          <path d="M4 6h16M4 12h16M4 18h16" />
          <circle cx="9" cy="6" r="2" fill="currentColor" />
          <circle cx="15" cy="12" r="2" fill="currentColor" />
          <circle cx="10" cy="18" r="2" fill="currentColor" />
        </svg>

        <!-- Table: Grid icon -->
        <svg v-else-if="tab.id === 'table'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M3 9.5h18M3 14.5h18M9.5 4v16M15 4v16" />
        </svg>

        <!-- Editor: Edit/Code icon -->
        <svg v-else-if="tab.id === 'editor'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 20h9" />
          <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
        </svg>

        <!-- Fonts & Themes: Framed A icon -->
        <svg v-else-if="tab.id === 'fonts'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <rect x="4" y="4" width="16" height="16" rx="2.5" />
          <path d="m8.5 16 3.5-8 3.5 8M9.7 13.3h4.6" />
        </svg>

        <!-- Locations: Folder icon -->
        <svg v-else-if="tab.id === 'locations'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
        </svg>

        <!-- Authoring: CPU / Chip icon -->
        <svg v-else-if="tab.id === 'authoring'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <rect x="6" y="6" width="12" height="12" rx="2" />
          <rect x="9.5" y="9.5" width="5" height="5" rx="1" />
          <path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3" />
        </svg>

        <!-- Keymap: Keyboard icon -->
        <svg v-else-if="tab.id === 'keymap'" viewBox="0 0 24 24" class="size-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2.5" y="5" width="19" height="14" rx="2.5" />
          <path d="M6.5 9h1M10 9h1M13.5 9h1M17 9h1M6.5 12h1M10 12h1M13.5 12h1M17 12h1M8 15.5h8" />
        </svg>

        <span class="leading-none">{{ tab.label }}</span>
      </button>
    </nav>

    <button
      type="button"
      class="absolute right-4 top-3 rounded-lg border border-line bg-surface-1 px-2 py-1 text-[11px] text-ink-muted hover:text-ink"
      @click="emit('close')"
    >
      Esc
    </button>
  </header>

  <!-- Tab Body -->
  <div class="min-h-0 flex-1 overflow-y-auto px-6 py-6">
    <!-- GENERAL TAB: Centered macOS / TablePlus two-column form -->
    <section v-if="settings.activeTab === 'general'" class="mx-auto max-w-2xl space-y-6">
      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-0.5 text-right font-medium text-ink-muted">Application:</span>
        <div class="space-y-2.5">
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input
              v-model="snap.general.reopenClusters"
              type="checkbox"
              class="size-4 rounded accent-brand"
              @change="settings.apply()"
            />
            <span>Reopen closed cluster tabs at startup</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input
              v-model="snap.general.autoCheckUpdates"
              type="checkbox"
              class="size-4 rounded accent-brand"
            />
            <span>Automatically check for updates at startup</span>
          </label>
        </div>

        <span class="pt-1.5 text-right font-medium text-ink-muted">Appearance:</span>
        <div>
          <select
            :value="settings.themePreset"
            class="w-full max-w-xs rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
            @change="settings.setThemePreset(($event.target as HTMLSelectElement).value as any)"
          >
            <option value="auto">Auto (System theme)</option>
            <option value="dark">Dark (Default)</option>
            <option value="light">Light</option>
            <option value="silver-dark">Silver Dark</option>
            <option value="jade-dark">Jade Dark</option>
            <option value="polaris-dark">Polaris Dark</option>
            <option value="sunset-dark">Sunset Dark</option>
            <option value="sunset-light">Sunset Light</option>
          </select>
        </div>
      </div>

      <hr class="border-line" />

      <div class="grid grid-cols-[9.5rem_1fr] items-center gap-x-5 gap-y-3.5 text-sm">
        <span class="text-right font-medium text-ink-muted">Notices:</span>
        <div class="flex items-center gap-3">
          <span class="w-32 text-xs text-ink-muted">Success banner</span>
          <input
            v-model.number="snap.general.noticeOkMs"
            type="number"
            min="1000"
            max="20000"
            step="500"
            class="w-28 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-right font-mono text-sm text-ink outline-none focus:border-brand"
          />
          <span class="text-xs text-ink-faint">ms</span>
        </div>

        <span />
        <div class="flex items-center gap-3">
          <span class="w-32 text-xs text-ink-muted">Error banner</span>
          <input
            v-model.number="snap.general.noticeBadMs"
            type="number"
            min="1000"
            max="30000"
            step="500"
            class="w-28 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-right font-mono text-sm text-ink outline-none focus:border-brand"
          />
          <span class="text-xs text-ink-faint">ms</span>
        </div>
      </div>

      <hr class="border-line" />

      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-0.5 text-right font-medium text-ink-muted">Biebie Access:</span>
        <div>
          <p class="text-sm text-ink">
            {{ accessInstalled ? 'Installed & ready' : 'Not installed' }}
          </p>
          <p class="mt-0.5 text-xs text-ink-faint">
            {{
              accessInstalled
                ? 'Clusters that require a customer VPN can ask Biebie Access to connect automatically.'
                : 'Clusters that do not require customer VPN access continue to work normally.'
            }}
          </p>
        </div>

        <span class="pt-1 text-right font-medium text-ink-muted">Version:</span>
        <div class="flex flex-wrap items-center gap-3">
          <span class="font-mono text-sm text-ink">Biebie Kube {{ version || '—' }}</span>
          <button
            type="button"
            class="rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-xs text-ink-muted hover:text-ink disabled:opacity-50"
            :disabled="checking"
            @click="checkForUpdate"
          >
            {{ checking ? 'Checking…' : 'Check for updates…' }}
          </button>
        </div>
      </div>
    </section>

    <!-- TABLE TAB: Centered macOS / TablePlus two-column form -->
    <section v-else-if="settings.activeTab === 'table'" class="mx-auto max-w-2xl space-y-6">
      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-0.5 text-right font-medium text-ink-muted">Resource Grid:</span>
        <div class="space-y-2.5">
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input
              v-model="snap.table.striping"
              type="checkbox"
              class="size-4 rounded accent-brand"
              @change="settings.apply()"
            />
            <span>Alternating row striping</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input
              v-model="snap.table.showNamespaceWhenAll"
              type="checkbox"
              class="size-4 rounded accent-brand"
            />
            <span>Show Namespace column when viewing all namespaces</span>
          </label>
        </div>
      </div>

      <hr class="border-line" />

      <div class="grid grid-cols-[9.5rem_1fr] items-center gap-x-5 gap-y-3.5 text-sm">
        <span class="text-right font-medium text-ink-muted">Row density:</span>
        <div>
          <select
            v-model.number="snap.table.density"
            class="w-full max-w-xs rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
            @change="settings.apply()"
          >
            <option :value="1">Compact</option>
            <option :value="1.25">Default</option>
            <option :value="1.5">Comfortable</option>
          </select>
        </div>

        <span class="text-right font-medium text-ink-muted">Age format:</span>
        <div>
          <select
            v-model="snap.table.ageFormat"
            class="w-full max-w-xs rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
          >
            <option value="relative">Relative (5m, 2h, 4d)</option>
            <option value="iso">ISO 8601 timestamp</option>
          </select>
        </div>
      </div>
    </section>

    <!-- EDITOR TAB: Centered macOS / TablePlus two-column form -->
    <section v-else-if="settings.activeTab === 'editor'" class="mx-auto max-w-2xl space-y-6">
      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-1.5 text-right font-medium text-ink-muted">YAML &amp; Code:</span>
        <div class="space-y-3">
          <div class="flex items-center gap-3">
            <span class="w-28 text-xs text-ink-muted">Tab size</span>
            <select
              v-model.number="snap.editor.tabSize"
              class="w-44 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
            >
              <option :value="2">2 spaces</option>
              <option :value="4">4 spaces</option>
            </select>
          </div>

          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.wordWrap" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Wrap long lines in YAML editors</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.lineNumbers" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Show line numbers</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.minimap" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Show editor minimap</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.diffSideBySide" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Render diffs side-by-side by default</span>
          </label>
        </div>
      </div>

      <hr class="border-line" />

      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-1.5 text-right font-medium text-ink-muted">Pod Logs:</span>
        <div class="space-y-3">
          <div class="flex items-center gap-3">
            <span class="w-28 text-xs text-ink-muted">Default tail</span>
            <select
              v-model.number="snap.editor.logTailLines"
              class="w-44 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
            >
              <option :value="100">Last 100 lines</option>
              <option :value="500">Last 500 lines</option>
              <option :value="2000">Last 2000 lines</option>
            </select>
          </div>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.logWrap" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Wrap log lines by default</span>
          </label>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.logTimestamps" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Show RFC 3339 timestamps by default</span>
          </label>
        </div>
      </div>

      <hr class="border-line" />

      <div class="grid grid-cols-[9.5rem_1fr] items-start gap-x-5 gap-y-3.5 text-sm">
        <span class="pt-1.5 text-right font-medium text-ink-muted">Pod Terminal:</span>
        <div class="space-y-3">
          <div class="flex items-center gap-3">
            <span class="w-28 text-xs text-ink-muted">Font size</span>
            <input
              v-model.number="snap.editor.terminalFontSize"
              type="number"
              min="10"
              max="24"
              class="w-24 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-right font-mono text-sm text-ink outline-none focus:border-brand"
            />
            <span class="text-xs text-ink-faint">px</span>
          </div>
          <label class="flex cursor-pointer items-center gap-2.5 text-ink">
            <input v-model="snap.editor.cursorBlink" type="checkbox" class="size-4 rounded accent-brand" />
            <span>Blink terminal cursor</span>
          </label>
        </div>
      </div>
    </section>

    <!-- FONTS & THEMES TAB -->
    <SettingsFontsThemes v-else-if="settings.activeTab === 'fonts'" class="mx-auto h-full max-w-4xl" />

    <!-- LOCATIONS TAB -->
    <div v-else-if="settings.activeTab === 'locations'" class="mx-auto max-w-2xl">
      <SettingsLocations />
    </div>

    <!-- AUTHORING TAB -->
    <div v-else-if="settings.activeTab === 'authoring'" class="mx-auto max-w-2xl [&>section]:mt-0">
      <ResourceAuthoring />
    </div>

    <!-- KEYMAP TAB: TablePlus-style centered group cards with pill shortcut badges -->
    <section v-else-if="settings.activeTab === 'keymap'" class="mx-auto max-w-2xl space-y-5">
      <div class="flex items-center justify-center">
        <input
          v-model="keymapSearch"
          class="w-full max-w-md rounded-xl border border-line bg-surface-2 px-3.5 py-2 text-center text-xs text-ink outline-none focus:border-brand"
          placeholder="Search keyboard shortcuts…"
        />
      </div>

      <div v-for="group in filteredKeymap" :key="group.title" class="space-y-2">
        <h3 class="px-1 text-xs font-medium text-ink-muted">{{ group.title }}</h3>
        <div class="rounded-xl border border-line bg-surface-2/70 px-6 py-4">
          <dl class="space-y-2.5">
            <div
              v-for="item in group.items"
              :key="item.label"
              class="grid grid-cols-[1fr_11rem] items-center gap-5 text-sm"
            >
              <dt class="text-right text-xs text-ink">{{ item.label }}</dt>
              <dd class="flex items-center">
                <span
                  class="inline-flex w-full items-center justify-center rounded-lg border border-line-strong bg-surface-3 px-3 py-1 font-mono text-xs text-ink shadow-2xs"
                >
                  {{ item.keys }}
                </span>
              </dd>
            </div>
          </dl>
        </div>
      </div>
    </section>
  </div>
</template>
