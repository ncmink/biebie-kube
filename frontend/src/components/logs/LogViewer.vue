<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { api, events, message, on } from '@/api'
import { useUIStore } from '@/stores/ui'
import type { ContainerInfo, LogChunk } from '@/types'

const props = defineProps<{ clusterId: string; namespace: string; pod: string }>()

const ui = useUIStore()

/**
 * The viewer holds a bounded window of lines.
 *
 * A container logging continuously would otherwise grow this array until the
 * renderer runs out of memory — the Go side batches, and this side forgets.
 */
const maxLines = 5000

const containers = ref<ContainerInfo[]>([])
const container = ref('')
const lines = ref<string[]>([])
const follow = ref(true)
const timestamps = ref(false)
const previous = ref(false)
const tailLines = ref(500)
const paused = ref(false)
const search = ref('')
const streamId = ref('')
const error = ref('')

const output = ref<HTMLElement>()
let unsubscribe: (() => void) | undefined

/** Buffered while paused, so resuming shows what was missed. */
let held: string[] = []

const shown = computed(() => {
  const needle = search.value.trim().toLowerCase()
  if (!needle) return lines.value
  return lines.value.filter((line) => line.toLowerCase().includes(needle))
})

async function loadContainers() {
  try {
    containers.value = await api.containers(props.clusterId, props.namespace, props.pod)
    if (!container.value && containers.value.length) {
      const first = containers.value.find((c) => !c.init) ?? containers.value[0]
      container.value = first.name
    }
  } catch (err) {
    error.value = message(err)
  }
}

async function start() {
  await stop()
  if (!container.value) return

  lines.value = []
  held = []
  error.value = ''

  try {
    streamId.value = await api.startLogStream(props.clusterId, {
      namespace: props.namespace,
      pod: props.pod,
      container: container.value,
      follow: follow.value,
      timestamps: timestamps.value,
      tailLines: tailLines.value,
      previous: previous.value,
    })
  } catch (err) {
    error.value = message(err)
  }
}

async function stop() {
  if (!streamId.value) return
  await api.stopLogStream(streamId.value)
  streamId.value = ''
}

function append(chunk: LogChunk) {
  if (chunk.streamId !== streamId.value) return
  if (chunk.error) error.value = chunk.error

  const incoming = chunk.lines ?? []
  if (!incoming.length) return

  if (paused.value) {
    held = [...held, ...incoming].slice(-maxLines)
    return
  }

  lines.value = [...lines.value, ...incoming].slice(-maxLines)
  void scroll()
}

async function scroll() {
  if (!follow.value) return
  await nextTick()
  const element = output.value
  if (element) element.scrollTop = element.scrollHeight
}

function resume() {
  paused.value = false
  if (held.length) {
    lines.value = [...lines.value, ...held].slice(-maxLines)
    held = []
    void scroll()
  }
}

async function download() {
  try {
    const text = await api.downloadLogs(props.clusterId, {
      namespace: props.namespace,
      pod: props.pod,
      container: container.value,
      follow: false,
      timestamps: timestamps.value,
      tailLines: tailLines.value,
      previous: previous.value,
    })
    const blob = new Blob([text], { type: 'text/plain' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `${props.pod}-${container.value}.log`
    link.click()
    URL.revokeObjectURL(link.href)
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

onMounted(async () => {
  unsubscribe = on(events.logChunk, append)
  await loadContainers()
  await start()
})

onBeforeUnmount(async () => {
  unsubscribe?.()
  await stop()
})

watch([container, follow, timestamps, previous, tailLines], start)
watch(() => [props.pod, props.namespace], async () => {
  await loadContainers()
  await start()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-line px-6 py-2">
      <select
        v-model="container"
        class="rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
      >
        <option v-for="item in containers" :key="item.name" :value="item.name">
          {{ item.name }}{{ item.init ? ' (init)' : '' }}
        </option>
      </select>

      <label class="flex items-center gap-1.5 text-xs text-ink-muted">
        <input v-model="follow" type="checkbox" class="accent-brand" /> Follow
      </label>
      <label class="flex items-center gap-1.5 text-xs text-ink-muted">
        <input v-model="timestamps" type="checkbox" class="accent-brand" /> Timestamps
      </label>
      <label class="flex items-center gap-1.5 text-xs text-ink-muted" title="Read the container instance that died">
        <input v-model="previous" type="checkbox" class="accent-brand" /> Previous
      </label>

      <select
        v-model.number="tailLines"
        class="rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
      >
        <option :value="100">Last 100</option>
        <option :value="500">Last 500</option>
        <option :value="2000">Last 2000</option>
        <option :value="10000">Last 10000</option>
      </select>

      <input
        v-model="search"
        class="w-48 rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
        placeholder="Search in view"
        spellcheck="false"
      />

      <div class="ml-auto flex gap-2">
        <button
          class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="paused ? resume() : (paused = true)"
        >
          {{ paused ? `Resume${held.length ? ` (${held.length})` : ''}` : 'Pause' }}
        </button>
        <button
          class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="download"
        >
          Download
        </button>
      </div>
    </div>

    <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-6 py-2 text-xs text-bad">
      {{ error }}
    </p>

    <div ref="output" class="min-h-0 flex-1 overflow-auto bg-surface-1 px-6 py-3">
      <pre
        v-if="shown.length"
        class="font-mono text-xs leading-relaxed whitespace-pre-wrap text-ink-muted"
      >{{ shown.join('\n') }}</pre>
      <p v-else class="py-10 text-center text-sm text-ink-faint">
        {{ search ? 'Nothing in view matches your search.' : 'Waiting for output…' }}
      </p>
    </div>

    <p class="shrink-0 border-t border-line px-6 py-1.5 font-mono text-[10px] text-ink-faint">
      {{ lines.length }} lines held · older lines are dropped past {{ maxLines }}
    </p>
  </div>
</template>
