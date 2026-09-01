/**
 * The shapes the Go layer sends the UI.
 *
 * Nothing is declared here. Wails v3 generates TypeScript from the Go types,
 * so these are re-exports: one source of truth, and a rename in Go becomes a
 * compile error here rather than a runtime surprise. This file exists so
 * components import from one stable path instead of reaching into generated
 * directories.
 *
 * There is deliberately no type for a bearer token, a kubeconfig body or a
 * password, because no such value may cross into the frontend at all.
 */

export {
  ArgoActivityKind,
  Category,
  ClusterState,
  ComparisonBlocker,
  ComparisonState,
  DifferenceClass,
  DifferenceGroup,
  DifferenceKind,
  FailureKind,
  GitFault,
  GitRenderer,
  GitStep,
  GitStepResult,
  Health,
  Kind,
  Layer,
  ManifestCertainty,
  OwnershipConfidence,
  PortForwardState,
  ProbeResult,
  ResourceAction,
} from '@bindings/biebie-kube/internal/domain/models'

export type {
  AccessRequirement,
  ActionRequest,
  ActionResult,
  ApplyResult,
  ArgoActionResult,
  ArgoActivity,
  ArgoApp,
  ArgoAppRef,
  ArgoCard,
  ArgoChip,
  ArgoDashboard,
  ArgoEndpoint,
  ArgoSummary,
  Cluster,
  ClusterInput,
  ClusterMetrics,
  ClusterOverview,
  Column,
  Condition,
  ContainerInfo,
  ContainerPort,
  Counter,
  CustomerGroup,
  DataEntry,
  Diagnosis,
  EventRow,
  GitAccess,
  GitCheck,
  GitIdentity,
  GitSource,
  KindInfo,
  KindPresence,
  ListQuery,
  LogChunk,
  LogOptions,
  SourceState,
  ManifestLocation,
  ManifestSearch,
  StateComparison,
  StateDifference,
  PodDetail,
  PortForwardRequest,
  PortForwardSession,
  Probe,
  InspectProperty,
  RelatedGroup,
  ResourceOwnership,
  ResourcePage,
  ResourceRef,
  ResourceInspect,
  ResourceRow,
  SearchHit,
  Session,
  SSHConfigFile,
  TerminalChunk,
  TerminalRequest,
  TerminalSession,
} from '@bindings/biebie-kube/internal/domain/models'

export type { ResourceChange } from '@bindings/biebie-kube/internal/cluster/models'

export type { RowsChanged } from '@bindings/biebie-kube/internal/resources/models'

export type {
  ContextEntry as KubeconfigContext,
  File as KubeconfigFile,
  ImportOptions as KubeconfigImport,
} from '@bindings/biebie-kube/internal/kubeconfig/models'

export type { AccessState, ClusterView, HandoffResult, YAMLDiff } from '@bindings/biebie-kube/models'

// AccessState is the name of two different things: the enum of connection
// states in the protocol, and the installed-plus-status pair this application
// shows on a cluster card. The protocol one is re-exported under a clearer
// name rather than shadowed.
export { AccessState as AccessConnectionState } from '@bindings/github.com/ncmink/biebie-protocol/context/models'
export { Environment as EnvironmentKind } from '@bindings/github.com/ncmink/biebie-protocol/context/models'

export type {
  AccessProfile,
  AccessSessionChanged,
  AccessStatus,
  BiebieContext,
} from '@bindings/github.com/ncmink/biebie-protocol/context/models'
