# Biebie Kube

Kubernetes desktop workspace for DevOps engineers who operate clusters across
many customers. Part of the **biebie.net** product family, and a sibling to
[Biebie Access](../biebie-access).

Biebie Kube **is not a VPN client**. It never speaks Forcepoint, Ivanti or
FortiClient. When a cluster sits behind a customer network, it asks Biebie
Access whether that network is reachable and, if not, asks it to connect.

```text
Vue 3 UI
   ↓
Wails services / events      (no REST API in between)
   ↓
Go application layer
   ↓
Kubernetes services
   ↓
client-go
   ↓
Kubernetes API
```

## Stack

Wails v3 · Go 1.25 · client-go · Vue 3 · TypeScript · Vite · Pinia ·
Tailwind CSS v4 · xterm.js · Monaco

---

## The two applications stay separate

```text
Biebie Access                        Biebie Kube
customer identity                    cluster metadata
credentials                          Kubernetes sessions
VPN sessions                         resources, logs, exec
network connectivity                 port forwards, YAML
        │                                   │
        └────────── protocol/ ──────────────┘
                 Biebie Context Protocol
```

They share the Biebie Context Protocol, and nothing else. No shared
database, no shared business logic. Either application runs with the other
uninstalled. In this repo the contract lives in `protocol/`
(`biebie-kube/protocol/...`).

---

## Nothing secret crosses the boundary

This is the rule the integration is built around. A `BiebieContext` carries
*who and where*, never *how to authenticate*:

```json
{
  "contextId": "ctx_20260819T0930",
  "customerId": "smoi",
  "customerName": "SMOI",
  "environmentId": "prod",
  "environmentName": "Production",
  "accessProfileId": "smoi-vpn",
  "clusterId": "rke2-prod",
  "clusterName": "RKE2 Production"
}
```

`protocol/context` rejects a context whose values look like credentials,
and `protocol/deeplink` refuses to build or parse a URL carrying a
password, token or certificate. A deep link carries one thing:

```text
biebie-kube://open?handoff=hnd_01JABC123
```

The handoff identifier is not a secret by itself. It is random, expires in
roughly a minute, works once, and is checked against the requesting
application and the current OS user before any context is returned.

Transport is a Unix domain socket on macOS and Linux and a named pipe on
Windows, in a directory the operating system restricts to the owner. Nothing
binds a network interface.

---

## The first end-to-end scenario

```text
Biebie Access                            Biebie Kube
────────────────────────────────────────────────────────────────
SMOI, credential "Ming"
Forcepoint connected
172.16.20.65:6443 reachable
[ Open in Biebie Kube ]
   │
   ├── creates a short-lived handoff
   └── launches biebie-kube://open?handoff=…
                                         consumes the handoff
                                         resolves the cluster
                                         connects client-go
                                         shows SMOI / Production / RKE2
```

The engineer does not choose the same cluster twice, and no password or
Kubernetes token travelled through the URL.

A link can arrive before the window exists, which is what happens when Biebie
Access launches Biebie Kube rather than switching to a running copy. The link is
held until the frontend calls `AppService.Ready`, so a handoff is never consumed
into a window that was not yet listening — a handoff works once, and losing it
would mean asking the engineer to start over.

### The reverse direction

A cluster that cannot be reached says which layer failed, and offers the fix:

```text
Access      ✓  Biebie Access connected
Network     ✓  172.16.20.65 reachable
TCP         ✕  172.16.20.65:6443 timed out
Kubernetes  –  not tested
```

If access is the missing layer, `Connect with Biebie Access` opens
`biebie-access://connect?profile=…`. When the VPN comes up, Biebie Access emits
an `AccessSessionChanged` event and Biebie Kube retries the cluster on its own.
Neither application restarts.

---

## Connection is a state machine, not a boolean

```go
disconnected → waiting_access → connecting → connected
                                     ↓
                    unauthorized / unreachable / failed
```

Each state carries a diagnosis, so the UI can say *which* layer broke rather
than "connection error".

---

## Layout

```text
biebie-kube/
├── main.go                application options, window, single instance, deep links
├── core.go                wiring shared by every service, and nothing else
├── service_app.go         version, state path, the deep-link queue
├── service_cluster.go     clusters, kubeconfigs, namespaces, catalogue
├── service_resource.go    tables, YAML, pods, events, overview, search
├── service_stream.go      logs, terminals, port forwards
├── service_access.go      Biebie Access integration and handoffs
│
├── protocol/              Biebie Context Protocol — no kube business logic
│   ├── context/           who/where record
│   ├── handoff/           short-lived, single-use handoff store
│   ├── ipc/               Unix socket / Windows named pipe
│   ├── deeplink/          biebie-kube:// and biebie-access:// URLs
│   └── version/           protocol version negotiation
│
├── internal/
│   ├── domain/            models shared across services
│   ├── store/             atomic JSON persistence
│   ├── kubeconfig/        read, parse, index — never rewrite
│   ├── kube/              client factory, discovery, informers
│   ├── cluster/           repository, lifecycle manager, diagnosis
│   ├── access/            IPC client, event server, deep links
│   ├── resources/         list, render, search, overview
│   ├── logs/              bounded, batched streaming
│   ├── terminal/          remotecommand + safe shell detection
│   ├── portforward/       loopback-only sessions
│   └── manifest/          read, diff, apply
│
└── cmd/mock-access/       stands in for Biebie Access in development
```

Each `service_*.go` file is a Wails service registered in `main.go`. They hold
no logic: a bound method resolves its arguments, calls one internal service, and
passes the error through `describe` so no home directory reaches the UI.

---

## Types come from Go, once

Wails v3 generates TypeScript from the Go types, including string enums for
`Kind`, `ClusterState`, `Health` and the rest. `frontend/src/types` re-exports
that output rather than restating it, so a renamed field is a compile error in
the frontend instead of an empty column at runtime. `frontend/bindings` is
generated on every build and is not committed.

A Go function returning `[]T` sends JSON `null` when it has nothing, which the
generated types report honestly. `frontend/src/api` collapses that to an empty
array once, at the seam, so no screen has to tell "no rows" from "no answer".

---

## Kubeconfigs are read, never rewritten

Importing indexes a file where it lives. `kubectl config set-context` changes
show up without a re-import, because contexts are read live rather than cached.
A copy is made only when explicitly asked for, and then with owner-only
permissions, since a kubeconfig usually embeds a client certificate.

---

## Watching, not polling

```text
Kubernetes API → informer → Go cache → debounced Wails event → Pinia → Vue
```

A rollout in a large cluster produces a handful of refreshes rather than one
per pod event. Log streams are batched and bounded on both sides, so a chatty
container cannot grow the renderer's memory without limit.

A watch outlives the request that started it. Wails cancels a bound method's
context when it returns, so anything longer-lived — informers, log streams, exec
sessions, port forwards — is detached from it deliberately and ends only when
its own session does.

---

## Guardrails

Production is marked with a word, not only a colour, and a hazard band that
survives a washed-out external monitor. Deleting or applying against a
production cluster asks for the object's name to be typed, and the dialog shows
customer, environment and cluster — because the expensive mistake is not
deleting the wrong object, it is deleting the right object in the wrong
customer's cluster.

Applying YAML always diffs against a freshly read object first. Secrets are
listed but never decoded without a deliberate action.

---

## Development

```bash
wails3 dev                     # run with hot reload
go test ./...                  # backend
cd frontend && npx vue-tsc --noEmit
wails3 package                 # build and bundle
```

Generate bindings through the task rather than by hand, so local type checks see
what the build sees:

```bash
wails3 task common:generate:bindings
```

Platform manifests are generated from `build/config.yml`, including the
`biebie-kube://` registration the handoff depends on. Change that file and run
`wails3 task common:update:build-assets`; editing a plist or the NSIS script
directly means the next regeneration silently drops the change.

To exercise the handoff without Biebie Access installed:

```bash
go run ./cmd/mock-access -open
```

That speaks the real protocol on the real endpoint, so what it proves locally
is what happens in production.
