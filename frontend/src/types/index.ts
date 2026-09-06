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
  AuthoringMode,
  Category,
  ClusterState,
  ComparisonBlocker,
  ComparisonState,
  DifferenceClass,
  DifferenceCause,
  DifferenceAttention,
  DifferenceGroup,
  DifferenceKind,
  EvidenceKind,
  ExplanationConfidence,
  FailureKind,
  GitFault,
  GitRenderer,
  GitStep,
  GitStepResult,
  Health,
  Kind,
  Layer,
  ManifestCertainty,
  OwnershipClaim,
  OwnershipConfidence,
  OwnershipProbeResult,
  OwnershipStatus,
  OwnershipUncertainty,
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
  AuthoringRuntime,
  Cluster,
  ClusterInput,
  ClusterMetrics,
  ClusterOverview,
  Column,
  Condition,
  ContainerInfo,
  ContainerPort,
  Counter,
  CreateAvailability,
  CreatedResource,
  CreateFailure,
  CreateOutcome,
  CustomerGroup,
  DataEntry,
  Diagnosis,
  DifferenceEvidence,
  DifferenceExplanation,
  EditComparison,
  EditFreshness,
  EditSession,
  EvidenceFact,
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
  ManifestPreview,
  ManifestProblem,
  ManifestResource,
  ManifestSearch,
  MutationGate,
  OwnershipProbe,
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
  ToolStatus,
} from '@bindings/biebie-kube/internal/domain/models'

export type { AuthoringSession } from '@bindings/biebie-kube/models'

export type { ResourceChange } from '@bindings/biebie-kube/internal/cluster/models'

export type { RowsChanged } from '@bindings/biebie-kube/internal/resources/models'

export type {
  ContextEntry as KubeconfigContext,
  File as KubeconfigFile,
  ImportOptions as KubeconfigImport,
} from '@bindings/biebie-kube/internal/kubeconfig/models'

export type { AccessState, ClusterView, HandoffResult } from '@bindings/biebie-kube/models'

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
  Forward,
} from '@bindings/github.com/ncmink/biebie-protocol/context/models'
