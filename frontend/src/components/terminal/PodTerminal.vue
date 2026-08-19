<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

import { api, events, message, on } from '@/api'
import type { ContainerInfo } from '@/types'

const props = defineProps<{ clusterId: string; namespace: string; pod: string }>()

const host = ref<HTMLElement>()
const containers = ref<ContainerInfo[]>([])
const container = ref('')
const status = ref('')
const error = ref('')
const sessionId = ref('')

let term: Terminal | undefined
let fit: FitAddon | undefined
let unsubscribe: (() => void) | undefined
let resizeObserver: ResizeObserver | undefined

/** The palette matches the application's, so the terminal is not a bright hole. */
const theme = {
  background: '#111114',
  foreground: '#f4f4f5',
  cursor: '#cbb6e8',
  selectionBackground: '#cbb6e855',
  black: '#1c1d1f',
  brightBlack: '#71717a',
}

function ensureTerminal() {
  if (term || !host.value) return

  term = new Terminal({
    fontFamily: "ui-monospace, 'SF Mono', 'JetBrains Mono', monospace",
    fontSize: 12,
    cursorBlink: true,
    convertEol: true,
    theme,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value)
  fit.fit()

  term.onData((data) => {
    if (sessionId.value) void api.sendTerminalInput(sessionId.value, data)
  })

  resizeObserver = new ResizeObserver(() => {
    fit?.fit()
    if (sessionId.value && term) {
      void api.resizeTerminal(sessionId.value, term.cols, term.rows)
    }
  })
  resizeObserver.observe(host.value)
}

async function loadContainers() {
  try {
    containers.value = (await api.containers(props.clusterId, props.namespace, props.pod)).filter(
      (item) => !item.init,
    )
    if (!container.value && containers.value.length) container.value = containers.value[0].name
  } catch (err) {
    error.value = message(err)
  }
}

async function open() {
  await close()
  if (!container.value) return

  ensureTerminal()
  error.value = ''
  status.value = 'Looking for a shell…'
  term?.clear()

  try {
    const session = await api.openTerminal(
      props.clusterId,
      props.namespace,
      props.pod,
      container.value,
    )
    sessionId.value = session.id
    status.value = `Connected with ${session.shell}`
    if (term) void api.resizeTerminal(session.id, term.cols, term.rows)
    term?.focus()
  } catch (err) {
    status.value = ''
    error.value = message(err)
  }
}

async function close() {
  if (!sessionId.value) return
  await api.closeTerminal(sessionId.value)
  sessionId.value = ''
  status.value = ''
}

onMounted(async () => {
  unsubscribe = on(events.terminalChunk, (chunk) => {
    if (chunk.sessionId !== sessionId.value) return
    if (chunk.data) term?.write(chunk.data)
    if (chunk.error) error.value = chunk.error
    if (chunk.done) {
      sessionId.value = ''
      status.value = 'Session ended'
      term?.writeln('\r\n\x1b[90m— session ended —\x1b[0m')
    }
  })

  await loadContainers()
  await open()
})

onBeforeUnmount(async () => {
  unsubscribe?.()
  resizeObserver?.disconnect()
  await close()
  term?.dispose()
})

watch(container, open)
watch(() => [props.pod, props.namespace], async () => {
  await loadContainers()
  await open()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b border-line px-6 py-2">
      <select
        v-model="container"
        class="rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
      >
        <option v-for="item in containers" :key="item.name" :value="item.name">{{ item.name }}</option>
      </select>
      <span class="font-mono text-xs text-ink-faint">{{ status }}</span>
      <div class="ml-auto flex gap-2">
        <button
          class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="open"
        >
          Restart
        </button>
        <button
          v-if="sessionId"
          class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="close"
        >
          Disconnect
        </button>
      </div>
    </div>

    <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-6 py-2 text-xs text-bad">
      {{ error }}
    </p>

    <div ref="host" class="min-h-0 flex-1 bg-surface-1 px-3 py-2" />
  </div>
</template>
