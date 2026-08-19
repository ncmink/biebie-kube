import { ClusterState, Health, PortForwardState } from '@/types'

/**
 * age renders a timestamp the way kubectl does: one unit, largest first.
 *
 * Null is accepted as well as undefined because an optional Go time arrives as
 * JSON null, and a pod that has not started yet is an ordinary thing to render.
 */
export function age(iso: string | null | undefined): string {
  if (!iso) return '—'
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '—'

  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 172_800) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86_400)}d`
}

/** duration renders elapsed time for a connected session. */
export function duration(iso: string | null | undefined): string {
  if (!iso) return '—'
  const started = new Date(iso).getTime()
  if (Number.isNaN(started)) return '—'

  const total = Math.max(0, Math.floor((Date.now() - started) / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${minutes}:${pad(seconds)}`
}

/** bytes renders a memory figure for a dashboard tile. */
export function bytes(value: number): string {
  if (!value) return '0'
  const units = ['B', 'Ki', 'Mi', 'Gi', 'Ti']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(size < 10 && unit > 0 ? 1 : 0)} ${units[unit]}`
}

/** millicores renders CPU the way Kubernetes reports it. */
export function millicores(value: number): string {
  return value >= 1000 ? `${(value / 1000).toFixed(1)} cores` : `${value}m`
}

/**
 * The presentation maps below are partial on purpose.
 *
 * Each enum carries a `$zero` member for Go's zero value, which is not a state
 * the UI has anything to say about, and a cluster may report a state added by
 * a newer backend than this window was built against. Both cases land on the
 * accessor's fallback rather than rendering blank.
 */
const healthColours: Partial<Record<Health, string>> = {
  [Health.HealthHealthy]: 'bg-ok',
  [Health.HealthWarning]: 'bg-warn',
  [Health.HealthCritical]: 'bg-bad',
  [Health.HealthProgress]: 'bg-info',
  [Health.HealthUnknown]: 'bg-ink-faint',
}

export function healthDot(health: Health): string {
  return healthColours[health] ?? 'bg-ink-faint'
}

const stateLabels: Partial<Record<ClusterState, string>> = {
  [ClusterState.ClusterDisconnected]: 'Disconnected',
  [ClusterState.ClusterWaitingAccess]: 'Waiting for access',
  [ClusterState.ClusterConnecting]: 'Connecting',
  [ClusterState.ClusterConnected]: 'Connected',
  [ClusterState.ClusterUnauthorized]: 'Not authorised',
  [ClusterState.ClusterUnreachable]: 'Unreachable',
  [ClusterState.ClusterFailed]: 'Failed',
}

export function stateLabel(state: ClusterState | undefined): string {
  return state ? (stateLabels[state] ?? state) : 'Disconnected'
}

const stateColours: Partial<Record<ClusterState, string>> = {
  [ClusterState.ClusterDisconnected]: 'bg-ink-faint',
  [ClusterState.ClusterWaitingAccess]: 'bg-warn',
  [ClusterState.ClusterConnecting]: 'bg-info',
  [ClusterState.ClusterConnected]: 'bg-ok',
  [ClusterState.ClusterUnauthorized]: 'bg-bad',
  [ClusterState.ClusterUnreachable]: 'bg-bad',
  [ClusterState.ClusterFailed]: 'bg-bad',
}

export function stateDot(state: ClusterState | undefined): string {
  return (state && stateColours[state]) ?? 'bg-ink-faint'
}

const forwardHealths: Partial<Record<PortForwardState, Health>> = {
  [PortForwardState.PortForwardStarting]: Health.HealthProgress,
  [PortForwardState.PortForwardRunning]: Health.HealthHealthy,
  [PortForwardState.PortForwardStopped]: Health.HealthUnknown,
  [PortForwardState.PortForwardFailed]: Health.HealthCritical,
}

/** forwardHealth lets a port forward reuse the same dot as everything else. */
export function forwardHealth(state: PortForwardState): Health {
  return forwardHealths[state] ?? Health.HealthUnknown
}
