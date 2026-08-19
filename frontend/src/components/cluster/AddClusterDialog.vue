<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind } from '@/types'
import type { KubeconfigFile } from '@/types'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; added: [] }>()

const ui = useUIStore()

const files = ref<KubeconfigFile[]>([])
const kubeconfigRef = ref('')
const contextName = ref('')
const name = ref('')
const customerId = ref('')
const customerName = ref('')
const environmentId = ref('')
const environmentName = ref('')
const environmentKind = ref<EnvironmentKind>(EnvironmentKind.EnvironmentUnknown)
const requiresAccess = ref(false)
const accessProfileId = ref('')
const importPath = ref('')
const saving = ref(false)
const error = ref('')

const selected = computed(() => files.value.find((file) => file.ref === kubeconfigRef.value))
const contexts = computed(() => selected.value?.contexts ?? [])

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    error.value = ''
    await refresh()
  },
)

// Choosing a context fills in a name, because the context name is almost
// always what the engineer would have typed anyway.
watch(contextName, (value) => {
  if (value && !name.value) name.value = value
})

watch(environmentKind, (kind) => {
  if (!environmentName.value && kind) {
    environmentName.value = kind.charAt(0).toUpperCase() + kind.slice(1)
    environmentId.value = environmentId.value || kind
  }
})

async function refresh() {
  try {
    files.value = await api.listKubeconfigs()
    if (!kubeconfigRef.value && files.value.length) {
      kubeconfigRef.value = files.value[0].ref
    }
  } catch (err) {
    error.value = message(err)
  }
}

async function importFile() {
  if (!importPath.value.trim()) return
  try {
    const file = await api.importKubeconfig(importPath.value.trim(), '', false)
    importPath.value = ''
    await refresh()
    kubeconfigRef.value = file.ref
  } catch (err) {
    error.value = message(err)
  }
}

async function save() {
  saving.value = true
  error.value = ''
  try {
    await api.createCluster({
      name: name.value.trim(),
      customerId: customerId.value.trim(),
      customerName: customerName.value.trim(),
      environmentId: environmentId.value.trim(),
      environmentName: environmentName.value.trim(),
      environmentKind: environmentKind.value,
      kubeconfigRef: kubeconfigRef.value,
      contextName: contextName.value,
      requiresAccess: requiresAccess.value,
      accessProfileId: accessProfileId.value.trim(),
    })
    ui.say(`Added ${name.value.trim()}.`)
    emit('added')
    emit('close')
    reset()
  } catch (err) {
    error.value = message(err)
  } finally {
    saving.value = false
  }
}

function reset() {
  contextName.value = ''
  name.value = ''
  customerId.value = ''
  customerName.value = ''
  environmentId.value = ''
  environmentName.value = ''
  environmentKind.value = EnvironmentKind.EnvironmentUnknown
  requiresAccess.value = false
  accessProfileId.value = ''
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-6 pt-16"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-2xl rounded-2xl border border-line bg-surface-2 p-6">
      <h2 class="text-base font-semibold text-ink">Add a cluster</h2>
      <p class="mt-1 text-sm text-ink-muted">
        Pick a context from a kubeconfig you already have, and say which customer it belongs to.
      </p>

      <p v-if="error" class="mt-4 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-sm">
        {{ error }}
      </p>

      <div class="mt-5 space-y-4">
        <div>
          <label class="text-xs text-ink-muted">Kubeconfig</label>
          <select
            v-model="kubeconfigRef"
            class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
          >
            <option v-for="file in files" :key="file.ref" :value="file.ref">
              {{ file.name }} — {{ file.path }}
            </option>
          </select>
          <p v-if="selected?.error" class="mt-1 text-xs text-bad">{{ selected.error }}</p>

          <div class="mt-2 flex gap-2">
            <input
              v-model="importPath"
              class="flex-1 rounded-lg border border-line bg-surface-1 px-3 py-1.5 font-mono text-xs text-ink outline-none focus:border-brand"
              placeholder="/path/to/another/kubeconfig"
              spellcheck="false"
            />
            <button
              class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
              @click="importFile"
            >
              Import
            </button>
          </div>
        </div>

        <div>
          <label class="text-xs text-ink-muted">Context</label>
          <select
            v-model="contextName"
            class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
          >
            <option value="">Select a context…</option>
            <option v-for="context in contexts" :key="context.name" :value="context.name">
              {{ context.name }} — {{ context.server }}
            </option>
          </select>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <label class="block">
            <span class="text-xs text-ink-muted">Cluster name</span>
            <input
              v-model="name"
              class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
              placeholder="RKE2 Production"
            />
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Environment</span>
            <select
              v-model="environmentKind"
              class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            >
              <option :value="EnvironmentKind.EnvironmentUnknown">Unclassified</option>
              <option :value="EnvironmentKind.EnvironmentDevelopment">Development</option>
              <option :value="EnvironmentKind.EnvironmentStaging">Staging</option>
              <option :value="EnvironmentKind.EnvironmentProduction">Production</option>
            </select>
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Customer id</span>
            <input
              v-model="customerId"
              class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
              placeholder="smoi"
            />
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Customer name</span>
            <input
              v-model="customerName"
              class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
              placeholder="SMOI"
            />
          </label>
        </div>

        <div class="rounded-xl border border-line bg-surface-3 p-4">
          <label class="flex items-start gap-3">
            <input v-model="requiresAccess" type="checkbox" class="mt-1 accent-brand" />
            <span>
              <span class="text-sm text-ink">This cluster needs a customer network</span>
              <span class="mt-0.5 block text-xs leading-relaxed text-ink-muted">
                Biebie Kube will ask Biebie Access whether the profile is connected before it tries
                Kubernetes, and will retry by itself once it comes up. Leave this off for local
                clusters.
              </span>
            </span>
          </label>
          <input
            v-if="requiresAccess"
            v-model="accessProfileId"
            class="mt-3 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
            placeholder="Biebie Access profile id, for example smoi-vpn"
          />
        </div>
      </div>

      <div class="mt-6 flex justify-end gap-2">
        <button
          class="rounded-lg border border-line px-3 py-1.5 text-sm text-ink-muted hover:text-ink"
          @click="emit('close')"
        >
          Cancel
        </button>
        <button
          class="rounded-lg bg-brand px-3 py-1.5 text-sm font-semibold text-surface-1 disabled:opacity-40"
          :disabled="saving || !contextName || !name"
          @click="save"
        >
          Add cluster
        </button>
      </div>
    </div>
  </div>
</template>
