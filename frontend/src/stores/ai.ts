import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref } from 'vue'

import { Events } from '@wailsio/runtime'

import { api, events, type AIStreamPayload } from '@/api'
import type { AICliInfo, AISettings, AskAIRequest, ResourceRef } from '@/types'

export const useAIStore = defineStore('ai', () => {
  const clis = ref<AICliInfo[]>([])
  const settings = ref<AISettings>({})
  const loadingClis = ref(false)

  const sessionId = ref('')
  const streaming = ref(false)
  const answer = ref('')
  const streamError = ref('')

  let unsubChunk: (() => void) | null = null
  let unsubDone: (() => void) | null = null
  let unsubError: (() => void) | null = null

  function wireEvents() {
    if (unsubChunk) return
    const handle = (payload: AIStreamPayload) => payload
    unsubChunk = Events.On(events.aiChunk, (ev) => {
      const payload = handle(ev.data as AIStreamPayload)
      if (payload.sessionId !== sessionId.value) return
      answer.value += payload.text ?? ''
    })
    unsubDone = Events.On(events.aiDone, (ev) => {
      const payload = handle(ev.data as AIStreamPayload)
      if (payload.sessionId !== sessionId.value) return
      streaming.value = false
    })
    unsubError = Events.On(events.aiError, (ev) => {
      const payload = handle(ev.data as AIStreamPayload)
      if (payload.sessionId !== sessionId.value) return
      streaming.value = false
      streamError.value = payload.error ?? 'The AI CLI stopped with an error.'
    })
  }

  async function refreshCLIs() {
    loadingClis.value = true
    try {
      clis.value = await api.listCLIs()
      settings.value = await api.aiSettings()
    } finally {
      loadingClis.value = false
    }
  }

  async function saveSettings(next: AISettings) {
    await api.setAISettings(next)
    settings.value = next
    await refreshCLIs()
  }

  async function acknowledge(cliId: string) {
    await api.acknowledgeCLI(cliId)
    if (!settings.value.acknowledged?.includes(cliId)) {
      settings.value = {
        ...settings.value,
        acknowledged: [...(settings.value.acknowledged ?? []), cliId],
      }
    }
    await refreshCLIs()
  }

  function resetDrawer() {
    sessionId.value = ''
    streaming.value = false
    answer.value = ''
    streamError.value = ''
  }

  async function ask(clusterId: string, resource: ResourceRef, prompt: string, cliId: string) {
    wireEvents()
    resetDrawer()
    streaming.value = true
    const req: AskAIRequest = { clusterId, ref: resource, prompt, cliId }
    const session = await api.askAI(req)
    sessionId.value = session.sessionId
  }

  async function cancel() {
    if (sessionId.value) {
      await api.cancelAskAI(sessionId.value)
    }
    streaming.value = false
  }

  return {
    clis,
    settings,
    loadingClis,
    sessionId,
    streaming,
    answer,
    streamError,
    refreshCLIs,
    saveSettings,
    acknowledge,
    resetDrawer,
    ask,
    cancel,
  }
})

if (import.meta.hot) {
  acceptHMRUpdate(useAIStore, import.meta.hot)
}
