<script setup lang="ts">
/**
 * Authoring a Kubernetes object that does not exist yet.
 *
 * The screen is arranged around the two things a person has to be sure of
 * before anything is written: where this is going, and what exactly is going
 * there. The target sits above the editor and never scrolls away; the manifest
 * that will be sent is shown as itself, not as a summary of itself.
 *
 * Nothing here decides whether creation is allowed, whether a manifest is
 * valid, or what a problem means. All of that is answered by Go and rendered.
 * The one judgement this component makes is a presentational one: production
 * asks for the object's name to be typed, the same as a delete does.
 */
import { computed, ref, watch } from 'vue'

import CodeEditor from '@/components/yaml/CodeEditor.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ContextTrail from '@/components/common/ContextTrail.vue'
import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import { singularTitle } from '@/composables/kind'
import { AuthoringMode, EnvironmentKind } from '@/types'
import type { Cluster, CreateAvailability, ManifestPreview } from '@/types'

const props = defineProps<{
  clusterId: string
  /** kind is the navigation kind of the list this was opened from. */
  kind: string
  kindTitle: string
  namespace: string
  cluster?: Cluster
}>()
const emit = defineEmits<{ close: []; created: [] }>()

const ui = useUIStore()

const availability = ref<CreateAvailability | null>(null)
const mode = ref<AuthoringMode>(AuthoringMode.AuthoringTypeScript)
const sessionId = ref('')
const source = ref('')
const preview = ref<ManifestPreview | null>(null)
const error = ref('')
const busy = ref(false)
const confirming = ref(false)
const showGenerated = ref(false)

const typescriptReady = computed(
  () => !!availability.value?.runtime.typescript && !!availability.value?.runtime.prepared,
)

const namespaced = computed(() => !!availability.value?.namespaced)

/**
 * The manifest shown in the preview pane is the manifest that will be sent.
 *
 * For YAML they are the same text by definition. For TypeScript they are not:
 * the TypeScript is a program, and what gets created is what it printed. A
 * screen that reviewed the program and created the output would be asking
 * somebody to approve something they were not shown.
 */
const generated = computed(() => preview.value?.yaml ?? '')

const resources = computed(() => preview.value?.resources ?? [])
const problems = computed(() => preview.value?.problems ?? [])

// Production asks for the object's name, exactly as a delete does. A manifest
// of several is confirmed by its first object: one name typed once for the
// batch, because the batch is one intention.
const confirmName = computed(() => {
  if (props.cluster?.environmentKind !== EnvironmentKind.EnvironmentProduction) return undefined
  return resources.value[0]?.name || undefined
})

async function load() {
  error.value = ''
  busy.value = true
  preview.value = null
  try {
    availability.value = await api.createAvailability(props.clusterId, props.namespace, props.kind)
    if (!availability.value.allowed) return

    if (mode.value === AuthoringMode.AuthoringTypeScript && !typescriptReady.value) {
      mode.value = AuthoringMode.AuthoringYAML
    }
    await open(mode.value)
  } catch (err) {
    error.value = message(err)
  } finally {
    busy.value = false
  }
}

async function open(next: AuthoringMode) {
  error.value = ''
  preview.value = null
  if (sessionId.value) void api.discardAuthoringSession(sessionId.value)
  try {
    const session = await api.newAuthoringSession(
      props.clusterId,
      props.namespace,
      props.kind,
      next,
    )
    sessionId.value = session.id
    source.value = session.source
    mode.value = next
  } catch (err) {
    error.value = message(err)
  }
}

function choose(next: AuthoringMode) {
  if (next === mode.value) return
  if (next === AuthoringMode.AuthoringTypeScript && !typescriptReady.value) return
  void open(next)
}

/**
 * One button for both surfaces, because the step is the same one: turn what is
 * in the editor into the manifest that would be sent, and say what is wrong
 * with it. TypeScript reaches that manifest by running; YAML is already there.
 */
async function check() {
  busy.value = true
  error.value = ''
  showGenerated.value = false
  try {
    preview.value =
      mode.value === AuthoringMode.AuthoringTypeScript
        ? await api.synthesize(props.clusterId, props.namespace, sessionId.value, source.value)
        : await api.validateManifest(props.clusterId, props.namespace, source.value)
  } catch (err) {
    error.value = message(err)
  } finally {
    busy.value = false
  }
}

async function create() {
  confirming.value = false
  busy.value = true
  error.value = ''
  try {
    const outcome = await api.createResources(props.clusterId, props.namespace, generated.value)
    ui.say(outcome.message, outcome.failed?.length ? 'bad' : 'ok')
    if (outcome.created?.length) emit('created')
    if (!outcome.failed?.length) close()
    else await check()
  } catch (err) {
    error.value = message(err)
  } finally {
    busy.value = false
  }
}

function close() {
  if (sessionId.value) void api.discardAuthoringSession(sessionId.value)
  emit('close')
}

watch(() => [props.clusterId, props.namespace, props.kind], load, { immediate: true })
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6" @click.self="close">
    <div class="flex h-full max-h-[90vh] w-full max-w-5xl flex-col rounded-2xl border border-line bg-surface-2 shadow-2xl">
      <header class="shrink-0 border-b border-line px-5 py-3">
        <div class="flex items-center gap-3">
          <h2 class="text-sm font-semibold text-ink">
            Create {{ availability?.targetKind || singularTitle(kindTitle) }}
          </h2>
          <button class="ml-auto text-xs text-ink-faint hover:text-ink" @click="close">Close</button>
        </div>

        <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
          <ContextTrail v-if="cluster" :cluster="cluster" compact />
          <!--
            The namespace line is shown only for a kind that has one. Printing
            "namespace —" beside a Namespace is the screen inventing a field
            the object does not have, which is how the starter came to invent
            a value for it.
          -->
          <span v-if="namespaced" class="font-mono text-[11px] text-ink-faint">
            namespace {{ namespace || '—' }}
          </span>
          <span v-else class="text-[11px] text-ink-faint">cluster-scoped</span>
        </div>
      </header>

      <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-5 py-2 text-xs text-bad">
        {{ error }}
      </p>

      <!--
        The GitOps answer comes before the editor rather than beside the Create
        button, because it decides whether there is an editor at all. A person
        who has typed a Deployment and is then told it belongs in Git has been
        allowed to waste their time.
      -->
      <div
        v-if="availability && !availability.allowed"
        class="min-h-0 flex-1 overflow-y-auto px-5 py-6"
      >
        <p class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
          {{ availability.needsNamespace ? 'Namespace' : 'GitOps' }}
        </p>
        <p class="mt-1 text-sm text-ink">
          {{
            availability.needsNamespace
              ? 'Not chosen'
              : availability.managed
                ? 'Managed'
                : 'Ownership unknown'
          }}
        </p>
        <p class="mt-2 max-w-xl text-sm leading-relaxed text-ink-muted">{{ availability.reason }}</p>
        <p v-if="availability.managed" class="mt-3 max-w-xl text-sm leading-relaxed text-ink-muted">
          Direct creation is not offered here. A resource this Application would not know about is
          one the next reconcile may delete, and the change would be recorded nowhere.
        </p>
        <div v-if="availability.app" class="mt-4 inline-block rounded-xl border border-line bg-surface-3 px-3 py-2">
          <p class="text-[10px] uppercase tracking-widest text-ink-faint">Application</p>
          <p class="mt-0.5 font-mono text-xs text-ink">
            {{ availability.app.namespace }}/{{ availability.app.name }}
          </p>
        </div>
      </div>

      <template v-else-if="availability">
        <div class="shrink-0 border-b border-line bg-surface-3/40 px-5 py-2">
          <p class="text-xs text-ink-muted">
            <span class="font-semibold text-ink">Unmanaged.</span>
            {{ availability.reason }}
          </p>
        </div>

        <div class="flex shrink-0 items-center gap-2 border-b border-line px-5 py-2">
          <div class="inline-flex rounded-xl border border-line bg-surface-3 p-1">
            <button
              class="rounded-lg px-3 py-1 text-xs"
              :class="[
                mode === AuthoringMode.AuthoringTypeScript ? 'bg-brand/20 text-ink' : 'text-ink-muted hover:text-ink',
                typescriptReady ? '' : 'cursor-not-allowed opacity-50',
              ]"
              @click="choose(AuthoringMode.AuthoringTypeScript)"
            >
              TypeScript{{ typescriptReady ? '' : ' 🔒' }}
            </button>
            <button
              class="rounded-lg px-3 py-1 text-xs"
              :class="mode === AuthoringMode.AuthoringYAML ? 'bg-brand/20 text-ink' : 'text-ink-muted hover:text-ink'"
              @click="choose(AuthoringMode.AuthoringYAML)"
            >
              YAML
            </button>
          </div>

          <button
            class="ml-auto rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-40"
            :disabled="busy"
            @click="check"
          >
            {{
              busy
                ? 'Working…'
                : mode === AuthoringMode.AuthoringTypeScript
                  ? 'Synthesise'
                  : 'Validate'
            }}
          </button>
          <button
            class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 disabled:opacity-40"
            :disabled="busy || !preview?.ready"
            @click="confirming = true"
          >
            Create
          </button>
        </div>

        <!--
          TypeScript is never hidden when its runtime is missing. A button that
          vanished would be a feature somebody has to be told exists; a locked
          one that says why is a feature they can go and enable.
        -->
        <p
          v-if="!typescriptReady"
          class="shrink-0 border-b border-line bg-warn/10 px-5 py-2 text-[11px] text-warn"
        >
          TypeScript authoring is unavailable. {{ availability.runtime.reason }}
          <RouterLink to="/settings" class="ml-1 underline">Open Settings</RouterLink>
        </p>

        <div class="flex min-h-0 flex-1">
          <div class="min-w-0 flex-1 border-r border-line">
            <CodeEditor
              v-model="source"
              :language="mode === AuthoringMode.AuthoringTypeScript ? 'typescript' : 'yaml'"
            />
          </div>

          <aside class="flex w-80 shrink-0 flex-col overflow-y-auto px-4 py-3">
            <p v-if="!preview" class="text-xs text-ink-muted">
              {{
                mode === AuthoringMode.AuthoringTypeScript
                  ? 'Synthesise to see the manifest this TypeScript produces. The manifest is what gets created, not the TypeScript.'
                  : 'Validate to check this manifest against the cluster before anything is sent.'
              }}
            </p>

            <template v-else>
              <p class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
                Generated manifest
              </p>
              <p class="mt-1 text-xs" :class="preview.ready ? 'text-ok' : 'text-warn'">
                {{ resources.length }} resource{{ resources.length === 1 ? '' : 's' }}
                {{ preview.ready ? '· ready' : '· not ready' }}
              </p>

              <ul class="mt-2 space-y-1">
                <li
                  v-for="(resource, index) in resources"
                  :key="index"
                  class="rounded-lg border border-line bg-surface-3 px-2.5 py-1.5"
                >
                  <p class="font-mono text-[11px] text-ink">
                    {{ resource.kind || '?' }}/{{ resource.name || '?' }}
                  </p>
                  <p class="font-mono text-[10px] text-ink-faint">
                    {{ resource.namespaced ? resource.namespace || '—' : 'cluster-scoped' }}
                  </p>
                  <p v-if="resource.exists" class="mt-0.5 text-[10px] text-warn">already exists</p>
                </li>
              </ul>

              <ul v-if="problems.length" class="mt-3 space-y-1">
                <li
                  v-for="(problem, index) in problems"
                  :key="index"
                  class="rounded-lg border border-warn/40 bg-warn/10 px-2.5 py-1.5 text-[11px] text-warn"
                >
                  <span v-if="problem.resource >= 0" class="font-mono text-ink-faint">
                    #{{ problem.resource + 1 }}
                  </span>
                  {{ problem.message }}
                </li>
              </ul>

              <button
                v-if="generated"
                class="mt-3 self-start text-[11px] text-ink-faint hover:text-ink"
                @click="showGenerated = !showGenerated"
              >
                {{ showGenerated ? 'Hide' : 'View' }} generated YAML
              </button>
              <pre
                v-if="showGenerated"
                class="mt-2 max-h-64 overflow-auto rounded-lg border border-line bg-surface-1 p-2 font-mono text-[10px] text-ink-muted"
              >{{ generated }}</pre>

              <!--
                cdk8s output is kept because a synth that failed said why, and
                the sentence above it is one line chosen out of a page.
              -->
              <details v-if="preview.output" class="mt-3">
                <summary class="cursor-pointer text-[11px] text-ink-faint hover:text-ink">
                  cdk8s output
                </summary>
                <pre
                  class="mt-1 max-h-48 overflow-auto rounded-lg border border-line bg-surface-1 p-2 font-mono text-[10px] text-ink-faint"
                >{{ preview.output }}</pre>
              </details>
            </template>
          </aside>
        </div>
      </template>

      <p v-else class="px-5 py-6 text-sm text-ink-muted">Checking this namespace…</p>
    </div>

    <ConfirmDialog
      :open="confirming"
      :title="
        resources.length === 1
          ? `Create ${resources[0]?.kind} ${resources[0]?.name}?`
          : `Create ${resources.length} resources?`
      "
      detail="This writes straight to the live cluster. Biebie Kube does not record it in Git, and Kubernetes has no transaction across objects — if one fails after others were created, what was created stays."
      :cluster="cluster"
      confirm-label="Create"
      :require-typing="confirmName"
      @cancel="confirming = false"
      @confirm="create"
    />
  </div>
</template>
