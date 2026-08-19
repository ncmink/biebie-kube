import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api, events, on } from '@/api'
import type { PortForwardSession } from '@/types'

/** Port forwards, which outlive the page that started them. */
export const usePortForwardStore = defineStore('portForwards', () => {
  const forwards = ref<PortForwardSession[]>([])

  async function load() {
    forwards.value = await api.listPortForwards()
  }

  async function start(
    clusterId: string,
    namespace: string,
    resourceType: string,
    resourceName: string,
    remotePort: number,
    localPort: number,
  ) {
    const session = await api.startPortForward(
      clusterId,
      namespace,
      resourceType,
      resourceName,
      remotePort,
      localPort,
    )
    await load()
    return session
  }

  async function stop(id: string) {
    await api.stopPortForward(id)
    await load()
  }

  function subscribe() {
    on(events.portForwards, (sessions) => {
      forwards.value = sessions ?? []
    })
  }

  return { forwards, load, start, stop, subscribe }
})
