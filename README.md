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
        └────────── biebie-protocol ────────┘
                 Biebie Context Protocol
```

They share the Biebie Context Protocol, and nothing else. No shared
database, no shared business logic. Either application runs with the other
uninstalled.

The contract is its own module, `github.com/ncmink/biebie-protocol`, checked out beside this
repository as `biebie-protocol/` and resolved through a `replace` directive in
`go.mod`. Both applications point at that one copy, so changing the wire format
breaks the build in both at once instead of leaving a mismatch to be found at
runtime.

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
password, token or certificate — both live in the shared `biebie-protocol`
module. A deep link carries one thing:

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
├── service_resource.go    tables, YAML, actions, pods, events, overview, search
├── service_stream.go      logs, terminals, port forwards
├── service_argocd.go      Argo CD dashboard, sync, refresh, the UI button
├── service_access.go      Biebie Access integration and handoffs
│
├── internal/
│   ├── domain/            models shared across services
│   ├── store/             atomic JSON persistence
│   ├── kubeconfig/        read, parse, index — never rewrite
│   ├── kube/              client factory, discovery, informers
│   ├── cluster/           repository, lifecycle manager, diagnosis
│   ├── access/            IPC client, event server, deep links
│   ├── resources/         list, render, search, overview
│   ├── argocd/            Argo CD dashboard, sync and refresh
│   ├── logs/              bounded, batched streaming
│   ├── terminal/          remotecommand + safe shell detection
│   ├── portforward/       loopback-only sessions
│   └── manifest/          read, diff, apply
│
├── cmd/mock-access/       stands in for Biebie Access in development
└── cmd/access-smoke/      asks a real Biebie Access everything this app asks
```

The protocol is not in this tree. It is a sibling checkout, shared with Biebie
Access:

```text
projects/biebie/
├── biebie-protocol/       github.com/ncmink/biebie-protocol — the wire contract
├── biebie-kube/           this repository
└── biebie-access/         the other half
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

## One customer at a time

The cluster list has always been read customer-first, so hiding is a property of
the customer rather than a second grouping bolted on beside it. A whole customer
can be put away, and the list says how many are out of sight:

```text
Acme Co      hidden     3 clusters
SMOI                    2 clusters
Ungrouped               1 cluster      ← auto-imported contexts start here
```

Hiding is presentation only. Nothing is deleted, an open session and its tab
survive it, a hidden cluster still connects, and a handoff from Biebie Access
still resolves to it — matching keys off identifiers, never off what the list
happens to be showing. `internal/cluster/group_test.go` holds that line.

**Ungrouped** has no hide control, because it is not a customer anyone is done
with: it is where auto-import puts every context it finds, so hiding it would
mean the next imported cluster arrives invisible behind the very section that
would have offered to reveal it. A flag stored there by an older build is
ignored for the same reason.

A group is keyed by customer identifier, falling back to the customer name when
none was given, for the same reason cluster identity is a UUID: a name is a
label people rewrite, and a customer the engineer hid must stay hidden across
the rename. State on disk describes only the groups that differ from the
default, and a customer whose last cluster is deleted or reassigned forgets its
flag, so an identifier typed again months later cannot start out invisible.

```json
"customers": [{ "key": "acme", "hidden": true }]
```

### The archive is the one section that is always there

Hiding a whole customer is the wrong tool for one noisy cluster, so the list has
a second kind of section: **Archived**. It is listed even when it is empty, it
sits last so it never pushes the customers you are working with down the page,
and it is hidden until you ask for it — the point is that there is always
somewhere to drop a cluster you are not using.

Archiving is a property of the cluster, not a change of customer. The customer
fields keep saying what they said, so a cluster taken back out returns to its own
customer's section, and a customer you had hidden is still hidden when it gets
there. Because hidden is the archive's default, it is *revealing* it that gets
recorded, and that choice survives the archive emptying:

```json
"customers": [{ "key": "biebie/archived", "hidden": false }]
```

Right-click any card for the rest: connect, disconnect, edit, archive, hide the
customer, or remove the cluster from Biebie Kube. Editing goes through the same
form as adding — and deliberately preserves what the form does not show, so
correcting a cluster's name cannot lose the labels an import wrote or quietly
take it out of the archive.

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

## Actions are verbs, not edits

Right-clicking a row offers what its kind can be asked to do: a deployment
scales and restarts, a node cordons, a cron job suspends and triggers. Each one
is the patch `kubectl` would send for the same verb — `spec.replicas` through
the scale subresource, `kubectl.kubernetes.io/restartedAt` on the pod template,
`spec.unschedulable` on the node — so a cluster never reaches a state only this
application knows how to read, and a restart from this window is the same thing
as a restart from a terminal.

The list lives on the kind in `internal/domain/catalogue.go` and travels to the
UI with the rest of `KindInfo`, so the menu the engineer right-clicks and the
service carrying the request out cannot come to disagree about what a kind
offers. A daemon set is not offered a scale because it has no scale
subresource; a custom resource is offered nothing at all, because what an
operator's resource means is not something this application can know.

Both halves of every toggle sit on the kind — cordon and uncordon, suspend and
resume — and which half applies is decided from the row already on screen. That
is what keeps a right-click from costing a round trip to find out whether a node
is cordoned.

Nothing refreshes afterwards. The watch behind the table sees the change the way
it sees one made from anywhere else.

---

## Argo CD is read from the cluster, not from Argo CD

A cluster whose definitions include `applications.argoproj.io` gains an **Argo
CD** section in the navigator, with a dashboard beside the cluster overview:
counts across every Application, a card per resource kind, the Applications
that need attention, and a timeline built from Kubernetes events.

Argo CD's own REST API is deliberately not spoken. It would need a second set
of credentials, a login this application has no business holding, and a
reachable server — to read state that is already on the objects. Everything on
the page comes through client-go, the same as every other view here.

The two actions go the same way. A sync records an `operation` on the
Application and the `argocd-application-controller` performs it; a refresh sets
`argocd.argoproj.io/refresh` and Argo CD clears the annotation when it is done.
Both therefore work against a cluster whose Argo CD server this machine cannot
reach at all. A batch is never abandoned at the first failure: syncing forty
Applications where two are refused reports the thirty-eight and names the two.

Health outranks sync everywhere. An Application that is both `Degraded` and
`OutOfSync` is listed as degraded, because fixing the sync would not fix it,
and `Suspended` is somebody's deliberate pause rather than a fault to chase.

**Open Argo CD UI** finds the server by its `app.kubernetes.io/name=argocd-server`
label, so it works whichever namespace the chart was installed into, and it
reuses a running port forward rather than opening a second tunnel to the same
service — the one the port-forward panel lists is the one the browser is
pointed at. Pruning against a cluster marked production asks for the cluster's
name to be typed, because a sync with prune deletes live resources.

---

## Guardrails

Production is marked with a word, not only a colour, and a hazard band that
survives a washed-out external monitor. Changing anything in a production
cluster — a delete, an applied manifest, a scale, a restart — asks for the
object's name to be typed, and the dialog shows customer, environment and
cluster, because the expensive mistake is not deleting the wrong object, it is
deleting the right object in the wrong customer's cluster. The rule is the same
for every one of them on purpose: a guard an engineer has to reason about is
one they will eventually reason their way around.

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

To check against the real Biebie Access instead, with its own endpoint serving:

```bash
go -C ../biebie-access run ./cmd/endpoint-smoke   # prints a profile id
go run ./cmd/access-smoke <connection id or name>
```

That speaks the real protocol on the real endpoint, so what it proves locally
is what happens in production.

A cluster may name its connection instead of carrying the id, because the id is
a UUID and the name is what Biebie Access actually shows. Biebie Access resolves
either, and `access.connect` replies with the id it resolved to; the cluster
record is rewritten to that id the first time it comes back. Only the id is
stable, and every state change Biebie Access announces uses it — a cluster left
holding the name would not recognise its own customer network coming up, and
would never retry on its own.
