<script setup lang="ts">
/**
 * What this machine can author Kubernetes resources with.
 *
 * The panel shows a path and a version rather than a tick, because the
 * interesting failure is not "Node is missing" — it is "Node is missing here".
 * A desktop application on macOS is started by launchd with a PATH that has
 * never seen /opt/homebrew/bin, so the copy a person uses every day in their
 * terminal can be invisible to this process. Showing which executable answered
 * is what lets somebody recognise that at a glance instead of arguing with a
 * tick they know is wrong.
 *
 * Nothing here decides readiness. Every judgement — whether TypeScript is
 * available, why it is not, what to install — is made in Go and rendered.
 */
import { computed, onMounted, ref } from 'vue'

import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import type { AuthoringRuntime, ToolStatus } from '@/types'

const ui = useUIStore()

const runtime = ref<AuthoringRuntime | null>(null)
const checking = ref(false)
const preparing = ref(false)
const error = ref('')

const tools = computed<{ label: string; status: ToolStatus }[]>(() => {
  const found = runtime.value
  if (!found) return []
  return [
    { label: 'Node.js', status: found.node },
    { label: 'npm', status: found.npm },
    { label: 'cdk8s', status: found.cdk8s },
  ]
})

const ready = computed(() => !!runtime.value?.typescript && !!runtime.value?.prepared)

async function check() {
  checking.value = true
  error.value = ''
  try {
    runtime.value = await api.authoringRuntime()
  } catch (err) {
    error.value = message(err)
  } finally {
    checking.value = false
  }
}

/**
 * Installing the cdk8s dependencies is a button rather than something that
 * happens when an editor opens.
 *
 * It is the only npm install in the application, it runs against a package.json
 * Biebie Kube wrote, and it reaches the network. All three are reasons for it
 * to be something a person chose rather than something they triggered by
 * pressing Preview.
 */
async function prepare() {
  preparing.value = true
  error.value = ''
  try {
    runtime.value = await api.prepareAuthoringRuntime()
    ui.say('The cdk8s dependencies are installed. TypeScript authoring is ready.')
  } catch (err) {
    error.value = message(err)
    await check()
  } finally {
    preparing.value = false
  }
}

onMounted(check)
</script>

<template>
  <section class="mt-8">
    <div class="flex flex-wrap items-center gap-2">
      <h2 class="text-sm font-semibold text-ink">Resource Authoring</h2>
      <button
        class="ml-auto rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-50"
        :disabled="checking"
        @click="check"
      >
        {{ checking ? 'Checking…' : 'Recheck' }}
      </button>
    </div>
    <p class="mt-1 text-xs text-ink-muted">
      Biebie Kube uses the Node.js, npm and cdk8s already installed on this machine. It neither
      bundles nor downloads them.
    </p>

    <p v-if="error" class="mt-3 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-xs text-bad">
      {{ error }}
    </p>

    <div v-if="runtime" class="mt-3 rounded-xl border border-line bg-surface-2">
      <div
        v-for="tool in tools"
        :key="tool.label"
        class="flex items-start gap-3 border-b border-line px-4 py-2.5 last:border-b-0"
      >
        <span class="w-20 shrink-0 text-xs text-ink-muted">{{ tool.label }}</span>
        <div class="min-w-0 flex-1">
          <p class="text-xs" :class="tool.status.available ? 'text-ok' : 'text-warn'">
            {{ tool.status.available ? '✓ Found' : '✕ Not found' }}
            <span v-if="tool.status.version" class="ml-1.5 font-mono text-ink-muted">
              {{ tool.status.version }}
            </span>
          </p>
          <p v-if="tool.status.path" class="truncate font-mono text-[11px] text-ink-faint">
            {{ tool.status.path }}
          </p>
          <p v-if="tool.status.reason" class="mt-0.5 text-[11px] text-ink-muted">
            {{ tool.status.reason }}
          </p>
        </div>
      </div>
    </div>

    <div v-if="runtime" class="mt-3 rounded-xl border border-line bg-surface-2 px-4 py-3">
      <div class="flex flex-wrap items-center gap-2">
        <p class="text-xs text-ink-muted">TypeScript authoring</p>
        <p class="text-xs font-semibold" :class="ready ? 'text-ok' : 'text-warn'">
          {{ ready ? 'Ready' : 'Unavailable' }}
        </p>
        <button
          v-if="runtime.typescript && !runtime.prepared"
          class="ml-auto rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 disabled:opacity-40"
          :disabled="preparing"
          @click="prepare"
        >
          {{ preparing ? 'Installing…' : 'Install cdk8s dependencies' }}
        </button>
      </div>

      <p v-if="runtime.reason" class="mt-1.5 text-xs text-ink-muted">{{ runtime.reason }}</p>
      <p v-if="runtime.typescript && !runtime.prepared" class="mt-1.5 text-[11px] text-ink-faint">
        This installs a fixed set of packages into a workspace Biebie Kube manages. It runs once,
        it reaches the npm registry, and nothing typed into an editor can add to the list.
      </p>

      <p class="mt-2 border-t border-line pt-2 text-[11px] text-ink-faint">
        Synthesising cdk8s runs the TypeScript in the editor through Node, with your privileges.
        Treat it the way you would treat a script you were about to run yourself.
      </p>
    </div>

    <p class="mt-2 text-xs text-ink-muted">
      YAML authoring needs none of this and is always available.
    </p>
  </section>
</template>
