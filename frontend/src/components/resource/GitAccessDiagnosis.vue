<script setup lang="ts">
/**
 * Why a repository could not be read, one layer at a time.
 *
 * It follows ConnectionDiagnosis, which explains a failed cluster connection
 * the same way and for the same reason: one sentence about a failure leaves
 * the reader guessing which part of the path produced it, and the parts have
 * different people to go and ask.
 *
 * Everything on screen was decided in Go. This file chooses no wording, knows
 * no git semantics, and does not read an error string — a component that
 * decided for itself whether a server's refusal meant "missing" or "not yours"
 * would be answering a question the server declined to answer.
 */
import { computed, ref } from 'vue'

import { api, copyToClipboard, message } from '@/api'
import { GitFault, GitStep, GitStepResult } from '@/types'
import type { GitAccess, GitIdentity, ResourceRef } from '@/types'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{
  clusterId: string
  resource: ResourceRef
  access: GitAccess
}>()

const emit = defineEmits<{ retry: [] }>()

const ui = useUIStore()

const identity = ref<GitIdentity | null>(null)
const identityError = ref('')
const testing = ref(false)
const outputOpen = ref(false)
const copied = ref(false)
let copiedTimer = 0

const stepNames: Partial<Record<GitStep, string>> = {
  [GitStep.StepGit]: 'Git',
  [GitStep.StepRemote]: 'Repository URL',
  [GitStep.StepHost]: 'Git host',
  [GitStep.StepAgent]: 'SSH agent',
  [GitStep.StepRepository]: 'Repository',
}

// The marks match ConnectionDiagnosis, with one addition. `?` is a check that
// ran and settled nothing, which the cluster probes never need and this does:
// an absent ssh-agent is not a pass and is not a failure.
const marks: Partial<Record<GitStepResult, string>> = {
  [GitStepResult.StepPassed]: '✓',
  [GitStepResult.StepFailed]: '✕',
  [GitStepResult.StepUnknown]: '?',
  [GitStepResult.StepSkipped]: '·',
}

const tones: Partial<Record<GitStepResult, string>> = {
  [GitStepResult.StepPassed]: 'text-ok',
  [GitStepResult.StepFailed]: 'text-bad',
  [GitStepResult.StepUnknown]: 'text-warn',
  [GitStepResult.StepSkipped]: 'text-ink-faint',
}

const checks = computed(() => props.access.checks ?? [])
const causes = computed(() => props.access.causes ?? [])
const working = computed(() => props.access.fault === GitFault.FaultNone)

// Only for ssh, and only worth offering once the connection has got far enough
// that who is calling is the open question. There is nothing to ask an https
// host, and nothing to learn from asking a host that did not answer.
const askable = computed(
  () =>
    props.access.transport === 'ssh' &&
    props.access.fault !== GitFault.FaultUnreachable &&
    props.access.fault !== GitFault.FaultTimeout &&
    props.access.fault !== GitFault.FaultGitMissing &&
    props.access.fault !== GitFault.FaultBadRemote,
)

async function testIdentity() {
  testing.value = true
  identityError.value = ''
  try {
    identity.value = await api.testGitIdentity(props.clusterId, props.resource)
  } catch (err) {
    identity.value = null
    identityError.value = message(err)
  } finally {
    testing.value = false
  }
}

async function copyCommand() {
  if (!(await copyToClipboard(props.access.command))) {
    ui.say('Could not copy to the clipboard.', 'bad')
    return
  }
  copied.value = true
  window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => (copied.value = false), 1500)
}

async function revealConfig() {
  try {
    await api.revealSSHConfig(props.access.sshConfig.path ?? '')
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <div class="rounded-lg border border-line bg-surface-2 px-3 py-2.5">
    <p class="text-[10px] uppercase tracking-wider text-ink-faint">Git access</p>
    <p class="mt-1 text-xs font-medium" :class="working ? 'text-ok' : 'text-bad'">
      {{ access.summary }}
    </p>

    <dl class="mt-3 space-y-1.5">
      <div v-for="check in checks" :key="check.step" class="flex items-start gap-2.5 text-[11px]">
        <dt class="w-24 shrink-0 text-ink-faint">{{ stepNames[check.step] ?? check.step }}</dt>
        <dd class="flex min-w-0 flex-1 items-start gap-2">
          <span class="font-mono" :class="tones[check.result]" aria-hidden="true">
            {{ marks[check.result] }}
          </span>
          <span
            class="min-w-0 flex-1 leading-relaxed"
            :class="check.result === GitStepResult.StepSkipped ? 'text-ink-faint' : 'text-ink-muted'"
          >
            {{ check.result === GitStepResult.StepSkipped ? 'Not tested' : check.detail }}
          </span>
          <span v-if="check.elapsedMs" class="shrink-0 font-mono text-ink-faint">
            {{ check.elapsedMs }}ms
          </span>
        </dd>
      </div>
    </dl>

    <!--
      More than one cause on purpose. Several git servers answer "no such
      repository" and "not yours" with the same words so that a stranger
      cannot map a private namespace by reading errors, and picking one of
      them here would be inventing the half the server withheld.
    -->
    <template v-if="causes.length">
      <p class="mt-3 text-[10px] uppercase tracking-wider text-ink-faint">Possible causes</p>
      <ul class="mt-1 space-y-1">
        <li
          v-for="cause in causes"
          :key="cause"
          class="flex gap-2 text-[11px] leading-relaxed text-ink-muted"
        >
          <span class="text-ink-faint" aria-hidden="true">•</span>
          <span>{{ cause }}</span>
        </li>
      </ul>
    </template>

    <div
      v-if="identity || identityError"
      class="mt-3 rounded-md border border-line px-2.5 py-2 text-[11px] leading-relaxed"
    >
      <template v-if="identity">
        <p class="text-ink-muted">{{ identity.summary }}</p>
        <p v-if="identity.greeting" class="mt-1 break-all font-mono text-[10px] text-ink-faint">
          {{ identity.greeting }}
        </p>
      </template>
      <p v-else class="text-bad">{{ identityError }}</p>
    </div>

    <div class="mt-3 flex flex-wrap gap-2">
      <button
        class="rounded-lg border border-line px-2.5 py-1 text-[11px] text-ink-muted hover:text-ink"
        @click="emit('retry')"
      >
        Retry comparison
      </button>

      <button
        v-if="askable"
        class="rounded-lg border border-line px-2.5 py-1 text-[11px] text-ink-muted hover:text-ink disabled:cursor-default disabled:text-ink-faint"
        :disabled="testing"
        @click="testIdentity"
      >
        {{ testing ? 'Asking the host…' : 'Test SSH identity' }}
      </button>

      <button
        class="rounded-lg border border-line px-2.5 py-1 text-[11px] text-ink-muted hover:text-ink"
        @click="copyCommand"
      >
        {{ copied ? 'Copied' : 'Copy diagnostic command' }}
      </button>

      <button
        v-if="access.sshConfig.exists"
        class="rounded-lg border border-line px-2.5 py-1 text-[11px] text-ink-muted hover:text-ink"
        @click="revealConfig"
      >
        Show SSH config
      </button>
    </div>

    <p class="mt-2 text-[11px] leading-relaxed text-ink-faint">
      Running the copied command in your own terminal answers something Biebie Kube cannot answer
      about itself: whether your shell can read this repository when this window cannot.
    </p>

    <!--
      Absence is reported and nothing is done about it. ssh works perfectly
      well with no config, and creating a file in somebody's .ssh directory
      because a comparison failed would be deciding something about their
      authentication that is not this application's to decide.
    -->
    <p v-if="!access.sshConfig.exists" class="mt-1 text-[11px] leading-relaxed text-ink-faint">
      There is no SSH config at
      <span class="break-all font-mono">{{ access.sshConfig.path }}</span
      >. That is normal — ssh does not need one.
    </p>

    <template v-if="access.output">
      <button
        class="mt-2 text-[11px] text-ink-faint underline decoration-line underline-offset-2 hover:text-ink-muted"
        @click="outputOpen = !outputOpen"
      >
        {{ outputOpen ? 'Hide git output' : 'Show git output' }}
      </button>
      <pre
        v-if="outputOpen"
        class="mt-2 max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md bg-surface-1 p-2.5 font-mono text-[11px] leading-relaxed text-ink-muted"
        >{{ access.output }}</pre
      >
    </template>
  </div>
</template>
