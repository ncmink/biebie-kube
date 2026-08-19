<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { api, message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import type { Appearance } from '@/stores/ui'
import type { KubeconfigFile } from '@/types'

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()

const configs = ref<KubeconfigFile[]>([])
const version = ref('')
const statePath = ref('')
const accessInstalled = ref(false)
const error = ref('')

const appearances: { id: Appearance; label: string }[] = [
  { id: 'dark', label: 'Dark' },
  { id: 'light', label: 'Light' },
  { id: 'system', label: 'System' },
]

async function load() {
  error.value = ''
  try {
    ;[configs.value, version.value, statePath.value, accessInstalled.value] = await Promise.all([
      api.listKubeconfigs(),
      api.version(),
      api.statePath(),
      api.accessInstalled(),
    ])
  } catch (err) {
    error.value = message(err)
  }
}

async function importDefault() {
  try {
    await api.importDefaultKubeconfig()
    await load()
    ui.say('Imported ~/.kube/config. Your original file was not modified.')
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

const copyIn = ref(false)

async function importChosen() {
  try {
    const path = await api.chooseKubeconfig()
    if (!path) return
    await api.importKubeconfig(path, '', copyIn.value)
    await load()
    ui.say(
      copyIn.value
        ? 'Imported a copy. The original file is untouched.'
        : 'Imported. Biebie Kube reads this file where it lives.',
    )
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function forget(ref_: string) {
  try {
    await api.forgetKubeconfig(ref_)
    await load()
    await clusters.load()
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

onMounted(load)
</script>

<template>
  <div class="h-full overflow-y-auto px-6 py-6">
    <div class="mx-auto max-w-3xl">
      <div class="flex items-center gap-3">
        <button class="text-xs text-ink-faint hover:text-ink" @click="router.back()">← Back</button>
        <h1 class="text-sm font-semibold uppercase tracking-widest text-ink-faint">Settings</h1>
      </div>

      <p v-if="error" class="mt-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
        {{ error }}
      </p>

      <section class="mt-6">
        <h2 class="text-sm font-semibold text-ink">Appearance</h2>
        <div class="mt-2 inline-flex rounded-xl border border-line bg-surface-2 p-1">
          <button
            v-for="option in appearances"
            :key="option.id"
            class="rounded-lg px-3 py-1 text-xs"
            :class="ui.appearance === option.id ? 'bg-brand/20 text-ink' : 'text-ink-muted hover:text-ink'"
            @click="ui.setAppearance(option.id)"
          >
            {{ option.label }}
          </button>
        </div>
      </section>

      <section class="mt-8">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-sm font-semibold text-ink">Kubeconfigs</h2>
          <div class="ml-auto flex items-center gap-2">
            <label class="flex items-center gap-1.5 text-xs text-ink-muted">
              <input v-model="copyIn" type="checkbox" class="accent-brand" /> Keep a copy
            </label>
            <button
              class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
              @click="importChosen"
            >
              Import file…
            </button>
            <button
              class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
              @click="importDefault"
            >
              Import ~/.kube/config
            </button>
          </div>
        </div>
        <p class="mt-1 text-xs text-ink-muted">
          Biebie Kube reads and indexes these files. It never rewrites the original unless you ask
          it to.
        </p>

        <ul class="mt-3 space-y-2">
          <li
            v-for="file in configs"
            :key="file.ref"
            class="rounded-xl border border-line bg-surface-2 px-4 py-3"
          >
            <div class="flex items-start gap-3">
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm text-ink">
                  {{ file.name }}
                  <span v-if="file.managed" class="text-[10px] uppercase tracking-widest text-ink-faint">
                    copied
                  </span>
                </p>
                <p class="truncate font-mono text-xs text-ink-faint">{{ file.path }}</p>
                <p v-if="file.error" class="mt-1 text-xs text-bad">{{ file.error }}</p>
                <p v-else class="mt-1 text-xs text-ink-muted">
                  {{ file.contexts?.length ?? 0 }} context{{
                    file.contexts?.length === 1 ? '' : 's'
                  }}
                </p>
              </div>
              <button
                class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
                @click="forget(file.ref)"
              >
                Forget
              </button>
            </div>
          </li>
        </ul>

        <p v-if="!configs.length" class="mt-3 text-sm text-ink-muted">
          No kubeconfig has been imported yet.
        </p>
      </section>

      <section class="mt-8">
        <h2 class="text-sm font-semibold text-ink">Biebie Access</h2>
        <p class="mt-1 text-sm text-ink-muted">
          {{
            accessInstalled
              ? 'Installed. Clusters that need a customer VPN can ask it to connect.'
              : 'Not installed. Clusters that do not require access still work normally.'
          }}
        </p>
      </section>

      <section class="mt-8 border-t border-line pt-4 text-xs text-ink-faint">
        <p>Biebie Kube {{ version }}</p>
        <p class="mt-1 font-mono">{{ statePath }}</p>
      </section>
    </div>
  </div>
</template>
