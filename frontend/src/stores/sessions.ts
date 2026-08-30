import { acceptHMRUpdate, defineStore } from 'pinia'
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

  /**
   * stopMany closes a set of forwards and reads the list back once.
   *
   * Clearing a cluster's eight tunnels through stop() would re-read the list
   * eight times for one visible change. A refusal is counted and returned
   * rather than thrown, because a batch where seven closed and one did not is
   * neither a success nor a failure, and the caller has to be able to say so.
   */
  async function stopMany(ids: string[]): Promise<number> {
    const results = await Promise.allSettled(ids.map((id) => api.stopPortForward(id)))
    await load()
    return results.filter((result) => result.status === 'rejected').length
  }

  function subscribe() {
    on(events.portForwards, (sessions) => {
      forwards.value = sessions ?? []
    })
  }

  return { forwards, load, start, stop, stopMany, subscribe }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(usePortForwardStore, import.meta.hot))
}
