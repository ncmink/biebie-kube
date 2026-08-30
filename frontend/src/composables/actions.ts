import { ResourceAction } from '@/types'
import type { KindInfo, ResourceRow } from '@/types'
import type { ContextMenuItem } from './menu'

/** Everything the UI needs to offer one action and to explain it first. */
export interface ActionDescriptor {
  action: ResourceAction
  label: string

  /** verb names the action in the confirmation's heading and button. */
  verb: string

  /** detail is the consequence, in the sentence the confirmation shows. */
  detail: string

  /** replicas asks for a count before the action can run. */
  replicas?: boolean
}

const descriptors: Record<ResourceAction, Omit<ActionDescriptor, 'action'>> = {
  [ResourceAction.$zero]: { label: '', verb: '', detail: '' },
  [ResourceAction.ActionScale]: {
    label: 'Scale…',
    verb: 'Scale',
    detail: 'Sets the desired replica count. Pods are added or removed to match it.',
    replicas: true,
  },
  [ResourceAction.ActionRestart]: {
    label: 'Restart rollout',
    verb: 'Restart',
    detail:
      'Replaces every pod under the workload’s own update strategy. The manifest is unchanged.',
  },
  [ResourceAction.ActionCordon]: {
    label: 'Cordon',
    verb: 'Cordon',
    detail: 'Stops the scheduler placing new pods here. Pods already running stay where they are.',
  },
  [ResourceAction.ActionUncordon]: {
    label: 'Uncordon',
    verb: 'Uncordon',
    detail: 'Lets the scheduler place pods on this node again.',
  },
  [ResourceAction.ActionSuspend]: {
    label: 'Suspend',
    verb: 'Suspend',
    detail: 'Stops the schedule. Runs already in flight finish on their own.',
  },
  [ResourceAction.ActionResume]: {
    label: 'Resume',
    verb: 'Resume',
    detail: 'Puts the schedule back. Missed runs are not made up.',
  },
  [ResourceAction.ActionTrigger]: {
    label: 'Trigger now',
    verb: 'Trigger',
    detail: 'Creates a job from this schedule’s template and runs it once, outside the schedule.',
  },
}

/**
 * actionsFor is what this object, right now, can be asked to do.
 *
 * A kind lists both halves of every toggle, because which half applies is a
 * property of the object rather than of the type. Deciding between them here,
 * from the row the table already holds, is what keeps a right-click from
 * costing a round trip to the cluster to find out whether a node is cordoned.
 */
export function actionsFor(info: KindInfo | undefined, row: ResourceRow): ActionDescriptor[] {
  return (info?.actions ?? [])
    .filter((action) => applies(action, row))
    .map((action) => ({ action, ...descriptors[action] }))
}

/** menuItems renders descriptors as the rows of a right-click menu. */
export function menuItems(actions: ActionDescriptor[]): ContextMenuItem[] {
  return actions.map((entry) => ({ id: entry.action, label: entry.label }))
}

function applies(action: ResourceAction, row: ResourceRow): boolean {
  switch (action) {
    case ResourceAction.ActionCordon:
      return !cordoned(row)
    case ResourceAction.ActionUncordon:
      return cordoned(row)
    case ResourceAction.ActionSuspend:
      return !suspended(row)
    case ResourceAction.ActionResume:
      return suspended(row)
    default:
      return true
  }
}

const cordoned = (row: ResourceRow) => (row.fields?.status ?? '').includes('cordoned')
const suspended = (row: ResourceRow) => (row.fields?.suspend ?? '') === 'Yes'

/**
 * currentReplicas is what the scale dialog opens on.
 *
 * Every scalable kind reports its desired count in the row already on screen —
 * as a bare number, or as the second half of a ready pair — so the dialog can
 * be pre-filled without reading the object again. A count that cannot be found
 * is left at zero rather than guessed at, and the user types what they meant.
 */
export function currentReplicas(row: ResourceRow): number {
  const desired = count(row.fields?.desired)
  if (desired !== undefined) return desired

  return count((row.fields?.ready ?? '').split('/')[1]) ?? 0
}

function count(value: string | undefined): number | undefined {
  const trimmed = (value ?? '').trim()
  if (!trimmed) return undefined

  const parsed = Number(trimmed)
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : undefined
}
