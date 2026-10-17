<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { api, message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import type { KubeconfigFile } from '@/types'

const clusters = useClusterStore()
const ui = useUIStore()

const configs = ref<KubeconfigFile[]>([])
const statePath = ref('')
const error = ref('')
const copyIn = ref(false)

async function load() {
  error.value = ''
  try {
    ;[configs.value, statePath.value] = await Promise.all([api.listKubeconfigs(), api.statePath()])
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
  <div class="space-y-4">
    <p v-if="error" class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">{{ error }}</p>

    <div>
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="text-sm font-semibold text-ink">Kubeconfigs</h3>
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
        Biebie Kube reads and indexes these files. It never rewrites the original unless you ask it to.
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
                <span v-if="file.managed" class="text-[10px] uppercase tracking-widest text-ink-faint"> copied </span>
              </p>
              <p class="truncate font-mono text-xs text-ink-faint">{{ file.path }}</p>
              <p v-if="file.error" class="mt-1 text-xs text-bad">{{ file.error }}</p>
              <p v-else class="mt-1 text-xs text-ink-muted">
                {{ file.contexts?.length ?? 0 }} context{{ file.contexts?.length === 1 ? '' : 's' }}
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
      <p v-if="!configs.length" class="mt-3 text-sm text-ink-muted">No kubeconfig has been imported yet.</p>
    </div>

    <div class="border-t border-line pt-4">
      <h3 class="text-sm font-semibold text-ink">Application data</h3>
      <p class="mt-1 font-mono text-xs text-ink-faint">{{ statePath }}</p>
    </div>
  </div>
</template>
