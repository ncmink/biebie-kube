<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind } from '@/types'
import type { AccessProfile, Cluster, KubeconfigFile } from '@/types'

/**
 * Adds a cluster, or edits one when a cluster is given.
 *
 * Both are the same form because they answer the same questions — which
 * kubeconfig context, whose cluster, which environment — and a second component
 * would only be the first one drifting out of step.
 */
const props = defineProps<{ open: boolean; cluster?: Cluster | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()

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

// The connections Biebie Access holds, so the profile can be picked rather than
// typed. An identifier typed by hand is the one field here whose mistakes are
// invisible: the cluster looks configured and only fails later, as a Connect
// button that never connects.
const accessProfiles = ref<AccessProfile[]>([])

const editing = computed(() => Boolean(props.cluster))
const selected = computed(() => files.value.find((file) => file.ref === kubeconfigRef.value))
const contexts = computed(() => selected.value?.contexts ?? [])

/**
 * Whether the profile has to be typed.
 *
 * Biebie Access being closed must not stop a cluster being configured, so the
 * text field stays as the fallback — and it is also what keeps an identifier
 * that was already saved editable when it no longer matches anything on this
 * machine.
 */
const mustTypeProfile = computed(
  () =>
    accessProfiles.value.length === 0 ||
    (accessProfileId.value !== '' &&
      !accessProfiles.value.some((profile) => profile.id === accessProfileId.value)),
)

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    error.value = ''
    fill()
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

/** fill starts the form from the cluster being edited, or empty for a new one. */
function fill() {
  const cluster = props.cluster
  kubeconfigRef.value = cluster?.kubeconfigRef ?? kubeconfigRef.value
  contextName.value = cluster?.contextName ?? ''
  name.value = cluster?.name ?? ''
  customerId.value = cluster?.customerId ?? ''
  customerName.value = cluster?.customerName ?? ''
  environmentId.value = cluster?.environmentId ?? ''
  environmentName.value = cluster?.environmentName ?? ''
  environmentKind.value = (cluster?.environmentKind as EnvironmentKind) ?? EnvironmentKind.EnvironmentUnknown
  requiresAccess.value = cluster?.access.required ?? false
  accessProfileId.value = cluster?.access.profileId ?? ''
}

async function refresh() {
  try {
    files.value = await api.listKubeconfigs()
    if (!kubeconfigRef.value && files.value.length) {
      kubeconfigRef.value = files.value[0].ref
    }
  } catch (err) {
    error.value = message(err)
  }

  // Separate from the kubeconfigs above, and deliberately silent: Biebie Access
  // being closed is the ordinary case, and it costs this form nothing. The
  // picker simply becomes a text field.
  try {
    accessProfiles.value = await api.accessProfiles()
  } catch {
    accessProfiles.value = []
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
  const input = {
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
  }
  try {
    const target = props.cluster
    if (target) {
      await api.updateCluster(target.id, input)
      ui.say(`Saved ${input.name}.`)
    } else {
      await api.createCluster(input)
      ui.say(`Added ${input.name}.`)
    }
    emit('saved')
    emit('close')
  } catch (err) {
    error.value = message(err)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-6 pt-16"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-2xl rounded-2xl border border-line bg-surface-2 p-6">
      <h2 class="text-base font-semibold text-ink">
        {{ editing ? 'Edit cluster' : 'Add a cluster' }}
      </h2>
      <p class="mt-1 text-sm text-ink-muted">
        Pick a context from a kubeconfig you already have, and say which customer it belongs to.
      </p>

      <p v-if="error" class="mt-4 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-sm">
        {{ error }}
      </p>

      <div class="mt-5 space-y-4">
        <div>
          <label class="text-xs text-ink-muted">Kubeconfig</label>
          <div class="relative mt-1">
            <select
              v-model="kubeconfigRef"
              class="h-10 w-full appearance-none rounded-lg border border-line bg-surface-1 px-3 pr-10 text-sm text-ink outline-none focus:border-brand"
            >
              <option v-for="file in files" :key="file.ref" :value="file.ref">
                {{ file.name }} — {{ file.path }}
              </option>
            </select>
            <span
              class="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-ink-faint"
              aria-hidden="true"
            >
              <svg class="size-5" viewBox="0 0 20 20" fill="currentColor">
                <path
                  fill-rule="evenodd"
                  d="M5.23 7.21a.75.75 0 011.06.02L10 11.17l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                  clip-rule="evenodd"
                />
              </svg>
            </span>
          </div>
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
          <div class="relative mt-1">
            <select
              v-model="contextName"
              class="h-10 w-full appearance-none rounded-lg border border-line bg-surface-1 px-3 pr-10 text-sm text-ink outline-none focus:border-brand"
            >
              <option value="">Select a context…</option>
              <option v-for="context in contexts" :key="context.name" :value="context.name">
                {{ context.name }} — {{ context.server }}
              </option>
            </select>
            <span
              class="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-ink-faint"
              aria-hidden="true"
            >
              <svg class="size-5" viewBox="0 0 20 20" fill="currentColor">
                <path
                  fill-rule="evenodd"
                  d="M5.23 7.21a.75.75 0 011.06.02L10 11.17l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                  clip-rule="evenodd"
                />
              </svg>
            </span>
          </div>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <label class="block">
            <span class="text-xs text-ink-muted">Cluster name</span>
            <input
              v-model="name"
              class="mt-1 h-10 w-full rounded-lg border border-line bg-surface-1 px-3 text-sm text-ink outline-none focus:border-brand"
              placeholder="RKE2 Production"
            />
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Environment</span>
            <div class="relative mt-1">
              <select
                v-model="environmentKind"
                class="h-10 w-full appearance-none rounded-lg border border-line bg-surface-1 px-3 pr-10 text-sm text-ink outline-none focus:border-brand"
              >
                <option :value="EnvironmentKind.EnvironmentUnknown">Unclassified</option>
                <option :value="EnvironmentKind.EnvironmentDevelopment">Development</option>
                <option :value="EnvironmentKind.EnvironmentStaging">Staging</option>
                <option :value="EnvironmentKind.EnvironmentProduction">Production</option>
              </select>
              <span
                class="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-ink-faint"
                aria-hidden="true"
              >
                <svg class="size-5" viewBox="0 0 20 20" fill="currentColor">
                  <path
                    fill-rule="evenodd"
                    d="M5.23 7.21a.75.75 0 011.06.02L10 11.17l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                    clip-rule="evenodd"
                  />
                </svg>
              </span>
            </div>
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Customer id</span>
            <input
              v-model="customerId"
              class="mt-1 h-10 w-full rounded-lg border border-line bg-surface-1 px-3 font-mono text-sm text-ink outline-none focus:border-brand"
              placeholder=""
            />
          </label>
          <label class="block">
            <span class="text-xs text-ink-muted">Customer name</span>
            <input
              v-model="customerName"
              class="mt-1 h-10 w-full rounded-lg border border-line bg-surface-1 px-3 text-sm text-ink outline-none focus:border-brand"
              placeholder=""
            />
          </label>
        </div>

        <p class="text-xs leading-relaxed text-ink-faint">
          The customer is what the cluster list is grouped by, and a customer can be kept off that
          list while you work somewhere else. To put a single cluster away, right-click it and
          archive it instead.
        </p>

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
          <template v-if="requiresAccess">
            <!--
              Picked from what Biebie Access actually holds when it is running,
              typed when it is not. The identifier has to match exactly, and
              nothing tells the engineer when it does not.
            -->
            <div v-if="!mustTypeProfile" class="relative mt-3">
              <select
                v-model="accessProfileId"
                class="h-10 w-full appearance-none rounded-lg border border-line bg-surface-1 px-3 pr-10 text-sm text-ink outline-none focus:border-brand"
              >
                <option value="">Select a connection…</option>
                <option v-for="profile in accessProfiles" :key="profile.id" :value="profile.id">
                  {{ profile.group ? `${profile.group} — ` : '' }}{{ profile.name }}
                  <template v-if="profile.provider"> ({{ profile.provider }})</template>
                </option>
              </select>
              <span
                class="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-ink-faint"
                aria-hidden="true"
              >
                <svg class="size-5" viewBox="0 0 20 20" fill="currentColor">
                  <path
                    fill-rule="evenodd"
                    d="M5.23 7.21a.75.75 0 011.06.02L10 11.17l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                    clip-rule="evenodd"
                  />
                </svg>
              </span>
            </div>

            <template v-else>
              <input
                v-model="accessProfileId"
                class="mt-3 h-10 w-full rounded-lg border border-line bg-surface-1 px-3 font-mono text-sm text-ink outline-none focus:border-brand"
                placeholder="Biebie Access connection name or id"
              />
              <p class="mt-2 text-xs text-ink-muted">
                {{
                  accessProfiles.length
                    ? 'This does not match the name or id of any connection in Biebie Access. Clear it to pick from the list.'
                    : 'Biebie Access is not running, so its connections cannot be listed. Its connection name works here, and is replaced with the stable id once Biebie Access confirms it.'
                }}
              </p>
            </template>
          </template>
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
          {{ editing ? 'Save changes' : 'Add cluster' }}
        </button>
      </div>
    </div>
  </div>
</template>
