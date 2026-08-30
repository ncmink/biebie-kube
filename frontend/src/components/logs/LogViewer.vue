<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  triggerRef,
  watch,
} from 'vue'

import LogButton from '@/components/logs/LogButton.vue'
import { api, events, message, on } from '@/api'
import { highlight, parseAnsi, stripAnsi } from '@/composables/ansi'
import type { Segment } from '@/composables/ansi'
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

/**
 * Row is one parsed line.
 *
 * The escape codes are read once, when the chunk arrives, rather than on every
 * render: a followed container delivers a chunk eight times a second, and the
 * same five thousand lines must not be re-parsed each time.
 */
interface Row {
  /** Monotonic within a stream. Doubles as Vue's key and as the line number. */
  id: number
  /** The line without escape codes, for searching and for copying. */
  plain: string
  /** The instant Kubernetes prefixed, when timestamps were asked for. */
  stamp: string
  segments: Segment[]
}

/** Kubernetes prefixes each line with an RFC 3339 instant and a space. */
const stamped = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z) /

const containers = ref<ContainerInfo[]>([])
const container = ref('')

// Rows are held in a shallow ref and mutated in place. Replacing the array on
// every chunk would cost more than drawing the lines does.
const rows = shallowRef<Row[]>([])
let counter = 0

const follow = ref(true)
const timestamps = ref(false)
const previous = ref(false)
const tailLines = ref(500)
const paused = ref(false)
const wrap = ref(false)
const numbers = ref(false)
const matchCase = ref(false)
const expanded = ref(false)
const search = ref('')
const streamId = ref('')
const error = ref('')
const saving = ref(false)
const copied = ref(false)

/**
 * Whether the view is parked at the end of the output.
 *
 * Following the stream and sticking to the bottom are separate things: an
 * engineer who scrolls up to read something wants the lines to keep arriving
 * without the view yanking itself back down.
 */
const atBottom = ref(true)

const output = ref<HTMLElement>()
let unsubscribe: (() => void) | undefined
let copiedTimer: number | undefined

/** Buffered while paused, so resuming shows what was missed. */
let held: Row[] = []

const needle = computed(() => search.value.trim())

const shown = computed(() => {
  if (!needle.value) return rows.value

  const term = matchCase.value ? needle.value : needle.value.toLowerCase()
  const matches = rows.value.filter((row) =>
    (matchCase.value ? row.plain : row.plain.toLowerCase()).includes(term),
  )

  // Matching lines keep their colours and gain a mark on the term itself, so
  // the search reads as an emphasis rather than as a different kind of output.
  return matches.map((row) => ({
    ...row,
    segments: highlight(row.segments, needle.value, matchCase.value),
  }))
})

/** build parses one raw line into the row the template draws. */
function build(line: string): Row {
  const hit = timestamps.value ? stamped.exec(line) : null
  const rest = hit ? line.slice(hit[0].length) : line

  counter += 1
  return {
    id: counter,
    plain: stripAnsi(rest),
    stamp: hit ? hit[1] : '',
    segments: parseAnsi(rest),
  }
}

/** text reassembles a row for the clipboard, timestamp included. */
function text(row: Row): string {
  return row.stamp ? `${row.stamp} ${row.plain}` : row.plain
}

/** clock shortens an RFC 3339 instant to a local wall clock with milliseconds. */
function clock(stamp: string): string {
  const at = new Date(stamp)
  if (Number.isNaN(at.getTime())) return stamp
  const time = at.toLocaleTimeString(undefined, { hour12: false })
  return `${time}.${String(at.getMilliseconds()).padStart(3, '0')}`
}

/** style turns a parsed segment into the handful of properties it asked for. */
function style(segment: Segment): Record<string, string> {
  const css: Record<string, string> = {}
  if (segment.fg) css.color = segment.fg
  if (segment.bg) css.backgroundColor = segment.bg
  if (segment.bold) css.fontWeight = '600'
  if (segment.dim) css.opacity = '0.65'
  if (segment.italic) css.fontStyle = 'italic'
  if (segment.underline) css.textDecoration = 'underline'
  return css
}

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

  rows.value = []
  counter = 0
  held = []
  error.value = ''
  atBottom.value = true

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

  const parsed = incoming.map(build)

  if (paused.value) {
    for (const row of parsed) held.push(row)
    if (held.length > maxLines) held.splice(0, held.length - maxLines)
    return
  }

  admit(parsed)
}

/** admit appends rows, drops the oldest past the bound, and redraws once. */
function admit(parsed: Row[]) {
  const list = rows.value
  for (const row of parsed) list.push(row)
  if (list.length > maxLines) list.splice(0, list.length - maxLines)

  triggerRef(rows)
  void scroll()
}

async function scroll() {
  if (!atBottom.value) return
  await nextTick()
  const element = output.value
  if (element) element.scrollTop = element.scrollHeight
}

/**
 * Leaving the bottom is what turns auto-scrolling off, and returning to it is
 * what turns it back on, so no separate control is needed for either.
 */
function watchScroll() {
  const element = output.value
  if (!element) return
  atBottom.value = element.scrollHeight - element.scrollTop - element.clientHeight < 24
}

function jump() {
  atBottom.value = true
  void scroll()
}

function resume() {
  paused.value = false
  if (held.length) {
    admit(held)
    held = []
  }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(shown.value.map(text).join('\n'))
    copied.value = true
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    ui.say('Could not copy to the clipboard.', 'bad')
  }
}

/**
 * Go writes the file behind a native save panel.
 *
 * The browser route — a blob URL on an anchor with a download attribute — is
 * ignored by the webview, so the button did nothing at all.
 */
async function save() {
  saving.value = true
  try {
    const path = await api.saveLogs(props.clusterId, {
      namespace: props.namespace,
      pod: props.pod,
      container: container.value,
      follow: false,
      timestamps: timestamps.value,
      tailLines: tailLines.value,
      previous: previous.value,
    })
    // No path means the panel was cancelled, which needs no notice.
    if (path) ui.say(`Saved logs to ${path}.`)
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    saving.value = false
  }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && expanded.value) expanded.value = false
}

onMounted(async () => {
  unsubscribe = on(events.logChunk, append)
  window.addEventListener('keydown', onKeydown)
  await loadContainers()
  await start()
})

onBeforeUnmount(async () => {
  unsubscribe?.()
  window.removeEventListener('keydown', onKeydown)
  window.clearTimeout(copiedTimer)
  await stop()
})

// Timestamps change how a line is split apart, not only what the stream sends,
// so they belong with the options that restart it.
watch([container, follow, timestamps, previous, tailLines], start)
watch(() => [props.pod, props.namespace], async () => {
  await loadContainers()
  await start()
})
</script>

<template>
  <div
    class="flex h-full min-h-0 flex-col"
    :class="expanded ? 'fixed inset-0 z-40 bg-surface-0' : ''"
  >
    <div class="flex shrink-0 flex-wrap items-center gap-1 border-b border-line px-4 py-2">
      <LogButton :label="follow ? 'Stop following' : 'Follow'" :active="follow" @click="follow = !follow">
        <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
          <path
            d="M4.5 3.5 8 7l3.5-3.5M4.5 8 8 11.5 11.5 8"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </LogButton>

      <LogButton
        :label="paused ? `Resume${held.length ? ` (${held.length} held)` : ''}` : 'Pause'"
        :active="paused"
        @click="paused ? resume() : (paused = true)"
      >
        <svg viewBox="0 0 16 16" class="size-3.5" aria-hidden="true">
          <path v-if="paused" d="M5 3.2 12.2 8 5 12.8Z" fill="currentColor" />
          <g v-else fill="currentColor">
            <rect x="4.6" y="3.4" width="2.2" height="9.2" rx="0.7" />
            <rect x="9.2" y="3.4" width="2.2" height="9.2" rx="0.7" />
          </g>
        </svg>
      </LogButton>

      <LogButton
        label="Read the container instance that died"
        :active="previous"
        @click="previous = !previous"
      >
        <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
          <path d="M7.4 4.3 3.2 8l4.2 3.7ZM13 4.3 8.8 8l4.2 3.7Z" fill="currentColor" />
        </svg>
      </LogButton>

      <span class="mx-1 h-4 w-px bg-line" />

      <select
        v-model="container"
        class="rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
      >
        <option v-for="item in containers" :key="item.name" :value="item.name">
          {{ item.name }}{{ item.init ? ' (init)' : '' }}
        </option>
      </select>

      <select
        v-model.number="tailLines"
        class="rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
      >
        <option :value="100">Last 100</option>
        <option :value="500">Last 500</option>
        <option :value="2000">Last 2000</option>
        <option :value="10000">Last 10000</option>
      </select>

      <span class="mx-1 h-4 w-px bg-line" />

      <input
        v-model="search"
        class="w-44 rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
        placeholder="containing"
        spellcheck="false"
      />
      <LogButton label="Match case" :active="matchCase" @click="matchCase = !matchCase">
        <span class="text-[10px] leading-none font-semibold">Aa</span>
      </LogButton>

      <div class="ml-auto flex items-center gap-1">
        <LogButton label="Wrap lines" :active="wrap" @click="wrap = !wrap">
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <path
              d="M2.5 4h11M2.5 12h4M2.5 8h8.4a2.3 2.3 0 0 1 0 4.6H6.5m2-1.8L6.4 12.6l2.1 1.8"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </LogButton>

        <LogButton label="Line numbers" :active="numbers" @click="numbers = !numbers">
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <path
              d="M6 4h8M6 8h8M6 12h8M2.5 4h1M2.5 8h1M2.5 12h1"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
          </svg>
        </LogButton>

        <LogButton label="Timestamps" :active="timestamps" @click="timestamps = !timestamps">
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="5.4" stroke="currentColor" stroke-width="1.2" />
            <path
              d="M8 4.8V8l2.2 1.6"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </LogButton>

        <span class="mx-1 h-4 w-px bg-line" />

        <LogButton :label="copied ? 'Copied' : 'Copy what is in view'" :active="copied" @click="copy">
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <rect
              x="5.6"
              y="5.6"
              width="7.9"
              height="7.9"
              rx="1.6"
              stroke="currentColor"
              stroke-width="1.2"
            />
            <path
              d="M10.4 5.6V4a1.6 1.6 0 0 0-1.6-1.6H4A1.6 1.6 0 0 0 2.4 4v4.8A1.6 1.6 0 0 0 4 10.4h1.6"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </LogButton>

        <LogButton :label="saving ? 'Saving…' : 'Save to a file'" :disabled="saving" @click="save">
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <path
              d="M8 2.6v7.3M5.1 7l2.9 2.9L10.9 7M3 13.4h10"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </LogButton>

        <LogButton
          :label="expanded ? 'Leave full view (Esc)' : 'Fill the window'"
          :active="expanded"
          @click="expanded = !expanded"
        >
          <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
            <path
              v-if="expanded"
              d="M6.2 2.6v3.6H2.6M13.4 6.2H9.8V2.6M9.8 13.4V9.8h3.6M2.6 9.8h3.6v3.6"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
            <path
              v-else
              d="M2.6 6.2V2.6h3.6M9.8 2.6h3.6v3.6M13.4 9.8v3.6H9.8M6.2 13.4H2.6V9.8"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </LogButton>
      </div>
    </div>

    <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-4 py-2 text-xs text-bad">
      {{ error }}
    </p>

    <div class="relative min-h-0 flex-1">
      <div
        ref="output"
        class="h-full overflow-auto bg-surface-1 py-2 font-mono text-xs leading-5"
        @scroll="watchScroll"
      >
        <div v-if="shown.length" :class="wrap ? 'w-full' : 'w-max min-w-full'">
          <div
            v-for="row in shown"
            :key="row.id"
            v-memo="[row.segments, wrap, numbers]"
            class="group flex min-h-5 hover:bg-surface-3"
          >
            <span
              v-if="numbers"
              class="sticky left-0 shrink-0 bg-surface-1 pr-3 pl-4 text-right tabular-nums text-ink-faint/60 select-none group-hover:bg-surface-3"
            >
              {{ row.id }}
            </span>
            <span v-else class="shrink-0 pl-4" />

            <span
              v-if="row.stamp"
              class="shrink-0 pr-3 tabular-nums text-ink-faint select-none"
              :title="row.stamp"
            >
              {{ clock(row.stamp) }}
            </span>

            <span
              class="pr-4 text-ink"
              :class="wrap ? 'min-w-0 flex-1 break-words whitespace-pre-wrap' : 'whitespace-pre'"
            >
              <span
                v-for="(part, at) in row.segments"
                :key="at"
                :style="style(part)"
                :class="part.match ? 'rounded-xs bg-warn text-surface-0' : ''"
                >{{ part.text }}</span
              >
            </span>
          </div>
        </div>

        <p v-else class="py-10 text-center text-sm text-ink-faint">
          {{ needle ? 'Nothing in view matches your search.' : 'Waiting for output…' }}
        </p>
      </div>

      <button
        v-if="!atBottom && shown.length"
        class="absolute bottom-4 left-1/2 -translate-x-1/2 rounded-full border border-line-strong bg-surface-3 px-3 py-1 text-xs text-ink-muted shadow-lg hover:text-ink"
        @click="jump"
      >
        Jump to latest ↓
      </button>
    </div>

    <p class="shrink-0 border-t border-line px-4 py-1.5 font-mono text-[10px] text-ink-faint">
      {{ needle ? `${shown.length} of ${rows.length} lines match` : `${rows.length} lines held` }} ·
      older lines are dropped past {{ maxLines }}
      <template v-if="paused && held.length"> · {{ held.length }} held while paused</template>
    </p>
  </div>
</template>
