/**
 * The single place the UI talks to Go.
 *
 * Wails v3 generates one module per service with fully typed methods, so this
 * facade adds no types of its own. What it does add is a stable, short shape
 * for components to import, a couple of ergonomic defaults, and the names of
 * the events the backend publishes.
 */
import { Browser, CancellablePromise, Clipboard, Events } from '@wailsio/runtime'

import {
  AccessService,
  AppService,
  ArgoCDService,
  AuthoringService,
  ClusterService,
  GitOpsService,
  LogService,
  PortForwardService,
  ResourceService,
  TerminalService,
} from '@bindings/biebie-kube'
import type { ArgoAppRef } from '@bindings/biebie-kube/internal/domain/models'

/**
 * list turns Go's nil slices into empty arrays.
 *
 * A Go function returning `[]T` sends JSON null when it has nothing, which the
 * generated types report honestly as `T[] | null`. Every screen wants "no rows"
 * rather than "no answer", so the distinction is collapsed once here instead of
 * being guarded at a dozen call sites.
 */
function list<A extends unknown[], T>(
  call: (...args: A) => CancellablePromise<T[] | null>,
): (...args: A) => CancellablePromise<T[]> {
  return (...args: A) => call(...args).then((value) => value ?? [])
}

export const api = {
  ready: AppService.Ready,
  version: AppService.Version,
  statePath: AppService.StatePath,
  checkForUpdate: AppService.CheckForUpdate,

  listClusters: list(ClusterService.ListClusters),
  getCluster: ClusterService.GetCluster,
  createCluster: ClusterService.CreateCluster,
  updateCluster: ClusterService.UpdateCluster,
  deleteCluster: ClusterService.DeleteCluster,

  listCustomerGroups: list(ClusterService.ListCustomerGroups),
  setCustomerGroupHidden: list(ClusterService.SetCustomerGroupHidden),
  setClusterArchived: ClusterService.SetClusterArchived,

  connectCluster: ClusterService.ConnectCluster,
  disconnectCluster: ClusterService.DisconnectCluster,
  listSessions: list(ClusterService.ListSessions),
  getSession: ClusterService.GetSession,

  listNamespaces: list(ClusterService.ListNamespaces),
  setNamespace: ClusterService.SetNamespace,

  listKubeconfigs: list(ClusterService.ListKubeconfigs),
  importKubeconfig: (path: string, name: string, copy: boolean) =>
    ClusterService.ImportKubeconfig({ path, name, copy }),
  importDefaultKubeconfig: ClusterService.ImportDefaultKubeconfig,
  chooseKubeconfig: ClusterService.ChooseKubeconfig,
  forgetKubeconfig: ClusterService.ForgetKubeconfig,
  resourceCatalogue: list(ClusterService.ResourceCatalogue),

  listResources: ResourceService.ListResources,
  countResources: list(ResourceService.CountResources),
  inspectResource: ResourceService.InspectResource,
  relatedResources: list(ResourceService.ListRelatedResources),
  getYAML: ResourceService.GetResourceYAML,

  // Editing a live object. openEditor is what makes "Original" a snapshot
  // rather than a moving target, and the version it returns is what applyYAML
  // writes against — so the two are always used as a pair.
  openEditor: ResourceService.OpenResourceEditor,
  compareEdit: ResourceService.CompareResourceEdit,
  editorFreshness: ResourceService.CheckResourceFreshness,
  applyYAML: ResourceService.ApplyResourceYAML,
  deleteResource: ResourceService.DeleteResource,
  performAction: ResourceService.PerformResourceAction,
  podDetail: ResourceService.GetPodDetail,
  containers: list(ResourceService.ListContainers),
  events: list(ResourceService.ListEvents),
  overview: ResourceService.GetClusterOverview,
  search: list(ResourceService.SearchResources),

  // Resource authoring. Every entry is a domain operation: there is
  // deliberately nothing here that takes an executable and arguments.
  authoringRuntime: AuthoringService.AuthoringRuntime,
  prepareAuthoringRuntime: AuthoringService.PrepareAuthoringRuntime,
  createAvailability: AuthoringService.CreateAvailability,
  newAuthoringSession: AuthoringService.NewAuthoringSession,
  discardAuthoringSession: AuthoringService.DiscardAuthoringSession,
  synthesize: AuthoringService.Synthesize,
  validateManifest: AuthoringService.Validate,
  createResources: AuthoringService.CreateResources,

  argoInstalled: ArgoCDService.ArgoCDInstalled,
  argoDashboard: ArgoCDService.GetArgoDashboard,
  argoApplications: list(ArgoCDService.ListArgoApplications),
  gitOwnership: ArgoCDService.ResolveGitOwnership,
  sourceState: GitOpsService.CompareWithSource,
  diagnoseGitAccess: GitOpsService.DiagnoseGitAccess,
  testGitIdentity: GitOpsService.TestGitIdentity,
  revealSSHConfig: GitOpsService.RevealSSHConfig,
  syncArgoApps: (clusterId: string, apps: ArgoAppRef[], prune: boolean) =>
    ArgoCDService.SyncArgoApplications(clusterId, { apps, prune }),
  refreshArgoApps: (clusterId: string, apps: ArgoAppRef[], hard: boolean) =>
    ArgoCDService.RefreshArgoApplications(clusterId, { apps, hard }),
  openArgoUI: ArgoCDService.OpenArgoUI,

  startLogStream: LogService.StartLogStream,
  stopLogStream: LogService.StopLogStream,
  saveLogs: LogService.SaveLogs,

  // The terminal and port-forward requests are spread into arguments because
  // every caller has the parts to hand and none of them has a request object.
  openTerminal: (clusterId: string, namespace: string, pod: string, container: string) =>
    TerminalService.OpenTerminal(clusterId, { namespace, pod, container, command: [] }),
  sendTerminalInput: TerminalService.SendTerminalInput,
  resizeTerminal: TerminalService.ResizeTerminal,
  closeTerminal: TerminalService.CloseTerminal,
  listTerminals: list(TerminalService.ListTerminals),

  startPortForward: (
    clusterId: string,
    namespace: string,
    resourceType: string,
    resourceName: string,
    remotePort: number,
    localPort: number,
  ) =>
    PortForwardService.StartPortForward(clusterId, {
      namespace,
      resourceType,
      resourceName,
      remotePort,
      localPort,
    }),
  stopPortForward: PortForwardService.StopPortForward,
  listPortForwards: list(PortForwardService.ListPortForwards),

  accessStatus: AccessService.GetAccessStatus,
  connectWithAccess: AccessService.ConnectWithAccess,
  accessInstalled: AccessService.AccessInstalled,
  accessProfiles: list(AccessService.ListAccessProfiles),
  consumeHandoff: AccessService.ConsumeHandoff,
}

/**
 * Events the Go layer publishes.
 *
 * The names are declared `as const` so they narrow to literals, which is what
 * lets the generated payload types flow into `on` without a type argument.
 */
export const events = {
  session: 'cluster:session',
  resources: 'cluster:resources',
  rows: 'cluster:rows',
  logChunk: 'logs:chunk',
  terminalChunk: 'terminal:chunk',
  portForwards: 'portforward:changed',
  accessChanged: 'access:changed',
  openCluster: 'app:openCluster',
  handoffFailed: 'app:handoffFailed',
} as const

/** on subscribes to a Go event and returns the unsubscribe function. */
export function on<E extends Events.WailsEventName>(
  event: E,
  handler: (payload: Events.WailsEventData<E>) => void,
): () => void {
  return Events.On(event, (wailsEvent) => handler(wailsEvent.data))
}

/** openInBrowser hands a URL to the user's browser rather than the webview. */
export function openInBrowser(url: string): void {
  void Browser.OpenURL(url)
}

/**
 * copyToClipboard puts text on the system clipboard.
 *
 * It goes through Wails rather than `navigator.clipboard` because the browser
 * API is only available in a secure context, and the webview serves this
 * application from a custom scheme that does not qualify. The call silently
 * does nothing there — which is worse than failing, because the button
 * animates and the clipboard is unchanged.
 *
 * Wails asks the platform directly, so it works wherever the app does. The
 * browser API is kept as a fallback for `wails dev` in a real browser tab,
 * where the runtime bridge is not attached.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await Clipboard.SetText(text)
    return true
  } catch {
    // Ignored: the fallback below is the answer, and reporting both failures
    // would tell the user about a bridge they do not know they are using.
  }
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

/**
 * message turns anything thrown by a binding into a sentence.
 *
 * Wails rejects with an Error whose message is the Go error string, but a
 * cancelled call and a network fault arrive differently, so everything is
 * funnelled through one place rather than each caller guessing.
 */
export function message(error: unknown): string {
  if (typeof error === 'string') return error
  if (error instanceof Error) return error.message
  return 'Something went wrong.'
}
