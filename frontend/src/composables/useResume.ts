import { onBeforeUnmount, onMounted } from 'vue'

/** Heartbeat interval for detecting wall-clock gaps after sleep. */
const heartbeatMs = 15_000

/** Gap longer than this is treated as waking from sleep. */
const sleepGapMs = 60_000

/**
 * Calls `onResume` when the window becomes visible again, the browser goes
 * online, or a heartbeat detects a large wall-clock jump (laptop sleep).
 */
export function useResume(onResume: () => void) {
  let lastBeat = Date.now()
  let interval: ReturnType<typeof setInterval> | undefined

  function beat() {
    const now = Date.now()
    if (now - lastBeat > sleepGapMs) {
      onResume()
    }
    lastBeat = now
  }

  function resume() {
    lastBeat = Date.now()
    onResume()
  }

  function onVisibility() {
    if (document.visibilityState === 'visible') resume()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('online', resume)
    interval = setInterval(beat, heartbeatMs)
  })

  onBeforeUnmount(() => {
    document.removeEventListener('visibilitychange', onVisibility)
    window.removeEventListener('online', resume)
    if (interval) clearInterval(interval)
  })
}
