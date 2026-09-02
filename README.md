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

## Ownership decides what belongs to what

Opening a deployment shows its revisions and the pods it is running; opening a
pod names the deployment above it. The chain is walked with `ownerReferences`
rather than with the selector on the manifest:

```text
Deployment ──owns──▶ ReplicaSet ──owns──▶ Pod
     ▲                                     │
     └────────── Controlled By ────────────┘
```

Labels are the obvious way to answer "which pods does this deployment run?" and
the wrong one. Two deployments in a namespace can carry the same `app` label, so
a label search answers with the other one's pods as well — and a wrong answer
that looks right is what gets acted on. A UID cannot be shared.

But a UID cannot be asked for either: `metadata.ownerReferences` is not an
indexed field, so no field selector can narrow a list to one owner's children.
Reading the whole namespace to find three pods is the cost of insisting on
ownership, and it is not one worth paying. So both are used, the way a
controller's own `ClaimPods` does it:

```text
selector ──▶ API server narrows the read ──▶ ownerRef UID decides the answer
             (cheap, approximate)              (exact, client-side)
```

The two stages are kept apart because they are doing different jobs. The
selector is an optimisation, so a kind whose selector cannot be read — a cron
job has none — widens to the namespace rather than guessing; narrowing wrongly
costs a larger read and never a wrong answer. The limit on that read stays the
cold budget rather than the group's, because a selector that turned out not to
narrow would otherwise return the first two hundred pods in the namespace and
the ownership test would reject them to nothing.

The exceptions are the two relationships Kubernetes really does express without
ownership. A service routes to whatever carries its labels, whoever owns it, so
there the selector is the answer and an empty one is refused rather than widened
— `{}` means "match everything", and a headless service asked what it routes to
would be handed the whole namespace. A node's pods are asked for with a field
selector, because that is the one question that spans every namespace.

The list is fetched when the drawer opens rather than watched. It costs a list
where the inspector beside it costs a get, which is why it is a separate call:
the properties paint as soon as the object is read instead of waiting on a
namespace of pods.

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

## Operational state goes to the cluster, desired state goes to Git

The application has three responsibilities and they are not interchangeable:

```text
Observe    resources, metrics, events, logs, container diagnostics
Operate    restart, scale, cordon, suspend, trigger, exec, port forward
GitOps     the desired state those live objects were rendered from
```

Every mutation answers one question before it is routed anywhere: is this
changing what is running, or changing what was asked for? A restart is the
first — it is an incident verb, it belongs to the moment, and writing it into a
repository would be recording an event as an intention. A replica count is the
second: patching it live gets it overwritten by the next reconcile, and the
engineer who does it has fixed the symptom in the one place nobody will read.

```text
operational state ──▶ Kubernetes
desired state ─────▶ Git ──▶ Argo CD ──▶ Kubernetes
```

That is why there is no **New Resource** button posting arbitrary manifests at
the API. A deployment created that way exists until the first sync notices
nothing declares it. The gap between the two halves is not a missing feature,
it is the thing the product is for.

## What claims this object, and how sure is it

The bridge across that boundary is one question — which Argo CD Application
manages this live object? — and it has three possible answers of three
different strengths. The drawer says which one it got.

```text
tracking annotation ──▶ names the Application and repeats the object's identity
status.resources ─────▶ the Application's own account of what it manages
instance label ───────▶ a name, and nothing behind it
```

The label is the weak one and it is the one that looks strongest.
`app.kubernetes.io/instance` is Argo CD's default tracking method, and it is
also a label the Kubernetes recommended-labels convention tells every tool to
set — Helm writes it on everything it installs. A resource carrying it beside
an Application of the same name may have nothing whatever to do with Argo CD.
So the label raises a candidate and never a conclusion, and the panel says
"possibly managed" rather than lending it a confidence it has not earned. An
annotation is checked against the object it is on, because one copied by a
careless `kubectl apply -f` names an Application that never created it.

From the Application, the repository is read out of `spec.sources` before
`spec.source`, so a multi-source Application is not reported with the first of
several repositories as though it were the only one.

Knowing the repository, the revision and the directory is still not knowing
which file declares this object, and only one of the four cases below has a
file to find at all:

```text
plain directory   the tree is known; the file can be looked for
Helm / Kustomize  the manifests are rendered; no file equals this object
plugin            the same, and this application cannot even guess how
Helm chart        there is no Git tree at all
```

Each source carries the sentence explaining its own limit, so the reason is
written once in Go rather than reassembled in the frontend. The three that
render their objects get no **Locate manifest** button, because a button that
could only ever come back empty is worse than no button.

A repository URL may legitimately carry a token in its userinfo, and Argo CD
stores whatever was configured. Credentials are stripped where the source is
read rather than where a URL is displayed, because everything downstream of
that point crosses the binding, reaches a log line, or ends up quoted in an
error message. An `scp`-style remote keeps its account name — `git@github.com`
is an SSH user, not a secret, and removing it would change where the URL
points.

Reading ownership costs nothing on a cluster without Argo CD: the catalogue
already knows whether `applications.argoproj.io` is served, so the question is
answered before any request is made and the panel does not render at all.

Nothing here writes to Git yet. Establishing that the relationship is
trustworthy comes first, because create, edit and delete are all built on top
of it and each one would inherit a wrong answer.

### Lack of visibility is not proof of lack of ownership

Once that answer decides whether a write is offered, the interesting case stops
being "who owns this" and becomes "what if we could not find out". There are
three answers and the third is not a shade of the second:

```text
Managed     an applicable Argo CD claim was found
Unmanaged   the check completed, and found none
Unknown     the check did not complete
```

Every way of failing to look lands in the third: a 403 listing Applications, a
timeout, an API error, a listing that ran out of pages before it ran out of
Applications, a namespace that could be read beside one that could not. None of
them is folded into Unmanaged, because Unmanaged opens a gate. An account with
`create configmaps` and no `list applications.argoproj.io` is an ordinary
configuration, and reading its silence as an absence is how an object appears in
a namespace a repository owns and disappears at the next reconcile.

```text
Managed    ──▶ direct write blocked, the repository is named
Unmanaged  ──▶ direct write offered
Unknown    ──▶ direct write blocked, the failed probe is named
Loading    ──▶ direct write blocked
```

Loading is on that list deliberately. A screen that renders Unmanaged for the
second before the answer arrives is a screen somebody can press Create on, so
the state is declared in Go alongside the other three, the gate is closed unless
something explicitly opened it, and the panel says "Checking GitOps ownership…"
rather than guessing.

**The navigator cannot change the answer.** This is the safety bug the section
exists to rule out:

```text
Argo CD installed in    argocd
the Deployment lives in reporting
the navigator shows     reporting
```

Scope ownership to what the UI is displaying and the owning Application is
invisible, the Deployment reads as unmanaged, and a direct write is offered on
the strength of a dropdown. So the Application search is cluster-wide, and the
only namespace it will ever accept is the target's own — as a fallback for when
the cluster-wide read is refused, which is reported as the partial search it is.
Nothing the navigator holds is passed in, and there is no parameter to pass it
through.

The search does not assume `argocd` either. Applications are found through the
API by resource, wherever the installation put them, which is also what makes
"applications in any namespace" work without configuration. An
ApplicationSet-generated Application is a concrete Application and is treated as
one; the generator is reported beside it, because editing a generated
Application is overwritten on the next reconcile and that is worth knowing.

**Partial results are not absences.** A listing is only allowed to conclude
"nothing claims this" when it can say it saw everything. Pagination is followed
to the end and reports itself truncated if it hits the page budget first; a
namespace-scoped fallback says which namespace it asked about. Positive evidence
is the asymmetric case and survives an incomplete search: an Application that
lists this object lists it whatever else could not be read.

**A refusal is answered quickly and remembered.** A denied permission is not
going to start working in the next few seconds, so ownership results are cached
per cluster — briefly for an answer, longer for a refusal — and a non-admin
account is never left waiting on a check that was never going to pass. Nothing
retries a 403.

**A failed check takes away the write and nothing else.** Ownership is safety
enrichment, not a connection. Properties, events, logs, pod details and YAML all
stay readable under Unknown; the YAML editor still opens, read-only. Only the
mutations whose safety depends on the answer are withheld, and the panel says
which probe failed — resource, verb, scope, result — rather than leaving somebody
to read an error string.

Operational verbs are deliberately outside all of this. Logs, exec, port forward,
restart and scale are not routed through the ownership gate, and a
GitOps-managed object does not become unusable in an incident. The boundary is
the one drawn further up this file: desired-state editing goes to Git, the
moment goes to the cluster. Where that gets uncomfortable — a live `scale` on a
managed Deployment is an operational verb writing a field a repository declares,
and Argo CD will take it back — the honest answer is that it is a fix for the
next ten minutes, and it is treated as one rather than blocked.

---

## The repository is read with the engineer's own git

Finding the file means leaving the machine, and that raises the question of
whose credentials go with it. The answer is: nobody's new ones. Biebie Kube
shells out to the `git` already installed and lets it authenticate the way it
always does — credential helper, ssh-agent, keychain. It stores no token, asks
for no password, and adds no secret to the ones an engineer is already
responsible for.

The cluster's credentials are pointedly not reused. A `ServiceAccount` that can
read Argo CD objects is not a Git identity, and a repository somebody cannot
reach from their own terminal is one this application says it cannot reach
either. That is a smaller product and a much smaller thing to get wrong.

### The allowlist is the part to read carefully

Three strings arrive from the cluster: the repository URL, the revision and the
path. Whoever can edit an Argo CD Application chooses all three, and Git treats
several of them as instructions rather than as data:

```text
ext::sh -c id         a transport that runs an arbitrary command
file:///etc           a transport that reads this machine's disk
--upload-pack=…       a leading dash, so git reads it as an option
main:../../etc/passwd revision syntax addressing whatever it likes
```

So `Remote`, `Revision` and `Path` in `internal/git` are types, not validated
strings. They can only be built by a constructor that runs the allowlist, which
means a value that has not been checked cannot reach a command line — the check
is not something a later caller can forget. Only `https`, `http` and `ssh`
remotes are run; everything else is refused by name so the panel can say which
transport it declined. Nothing is ever passed through a shell.

Two more doors are closed on the way out. Every invocation sets
`GIT_TERMINAL_PROMPT=0` and an ssh `BatchMode`, because a background process
that stops to ask for a passphrase is an application that has hung. And git's
own errors quote the URL it was handed, so stderr is stripped of credentials
before it becomes an error — the same reflex as `describe()` removing home
directories, one layer further out.

Repositories are mirrored bare under the user's cache directory, with
`--filter=blob:none` where the server offers it. Bare, so no working copy
exists that somebody could edit by accident; the cache directory, because a
mirror is large, rebuilds itself, and deleting it costs only the speed of the
next read. Reading is bounded by a file count and a file size, and a search
that stopped early says so rather than reporting that nothing was found.

### Every way this fails leaves the panel standing

Locating a manifest is the one thing here that reaches the network, and it is
behind a button for that reason. When it does not work, what ownership already
established is untouched:

```text
no git installed      "Git is not installed on this machine."
transport refused     names the transport and why it will not be run
credentials rejected  says so, with the token removed from git's own words
host unreachable      says which host
nothing found         stays at "source tree known", says where it looked
several files found   lists all of them and picks none
```

The last one is not a failure. A base beside its overlay is a real state of a
repository, and choosing between them is a judgement this application does not
have the information to make. It shows the candidates and stops.

What is found is reported against the commit it was read at, not the branch
name that was asked for. `main` is a different tree tomorrow, and an answer
nobody can check afterwards is not much of an answer.

---

## Original vs Edited

Two comparisons live in this application and they answer opposite questions:

```text
Source vs Live      what a repository declares, against the cluster
Original vs Edited  the cluster as it was when this editor opened,
                    against what has been typed since
```

The first says whether the cluster has drifted. The second says what one person
is about to do, and it is the one the YAML editor shows, beside the button that
does it. Editing a live object without it is typing into a wall of text and
pressing Apply.

```text
Edit  Changes 3        2 changes, +2 −2        [ Revert all ]  [ Review & apply ]

ORIGINAL                          EDITED
  replicas: 3                       replicas: 5
    periodSeconds: 10                 periodSeconds: 15
```

It is Monaco's own diff editor rather than a custom component, side by side, and
the right-hand side is the same document the Edit tab holds — switching tabs
rebuilds the editor and never the text, so nothing is lost mid-edit.

**Original is a snapshot and stays one.** It is captured once, when the editor
opens, and no watch event, controller write or external apply replaces it.
That is the whole meaning of the comparison: "what have I changed since I opened
this?" cannot be answered by a value that moves. A live change is a real fact
and a different one, and it surfaces before the write as the conflict it is.

**Revert all** restores that snapshot and touches nothing else. It calls no
binding, so there is nothing in it that could reach a cluster; what it undoes is
typing, which is all that has happened.

Two questions are answered about the difference rather than one, because they
come apart:

```text
Dirty       the text differs — what the "modified" marker means
Equivalent  both sides parse to the same object
```

A manifest whose keys were reordered is both at once, and saying so is more
useful than either half alone: the editor is honestly modified, and applying it
would change nothing. Equivalence is judged on the parsed objects rather than on
re-encoded text, so it does not quietly become a question about this
application's YAML writer. Both are computed in Go, which is where they can be
tested, and the editor renders the answer.

### The version the editor opened at is the one that guards the write

The concurrency protection had a hole worth describing, because the shape of it
is easy to reproduce. The editor stripped `resourceVersion` out of the manifest
it showed — correctly; it is server-managed noise, and a token somebody can
delete while tidying up is not a token. But the apply then read the object again
and stamped the *current* version onto the write. Every write succeeded by
definition, and the interface implied something was being checked.

So the version is captured with the snapshot, carried on the session rather than
in the text, and sent back with the write:

```text
open   ──▶ snapshot + resourceVersion 88421
edit
apply  ──▶ live is 88999
       ──▶ nothing is written
```

Before the confirmation appears, the live version is read and compared. If it
moved, the write is not attempted and the choice is offered rather than made:

```text
This object changed in the cluster after the editor was opened.
[ Refresh original ]  [ Cancel ]
```

Refreshing starts a new session against the object as it now stands, and says
plainly that it does not merge the edits in. Nothing is rebased automatically —
guessing how somebody's replica count should combine with someone else's is not
a decision to make on their behalf. Apply itself refuses a write that carries no
version at all, rather than falling back to the live one.

The other guards are unchanged and sit in the same order: ownership first — a
managed or unverifiable object is not edited through a cluster — then target
identity, since a manifest renamed in the editor would create a second object
rather than update the one on screen, then concurrency. All three are in Go, so
a screen that is wrong about any of them cannot get past them. Production still
asks for the object's name to be typed.

None of this applies to **Create Resource**. An object that does not exist has
no original, no version to guard and nothing to diff, so that flow stays what it
was: author, preview the manifest that will be sent, create.

---

## Source vs Live

Lens shows what is running. Once the manifest is located, Biebie Kube can also
show what Git says should be running, and — more usefully — which of the
differences between them anybody needs to do something about:

```text
Source vs Live
⚠ 1 meaningful difference

Container image
super-auto-develop

SOURCE  super-auto-develop
LIVE    708607833758.dkr.ecr.…/super-auto-develop:6520

Show 2 system-managed differences
```

Source, not desired. What this reads is a file in a repository, and between
that file and the running object there may be a Helm rendering, a Kustomize
build, a pipeline that rewrites an image reference, or an Argo CD parameter
override. The rendered desired state Argo CD actually applied is something this
application does not have, and naming the feature after it would be claiming a
certainty that has to be earned.

Locating and comparing happen in one call, because they must come from the same
commit: two calls would be two reads of a branch that moves, and the panel could
show a file from one commit beside a difference computed against another with
nothing on screen saying so.

### A comparison nobody reads is worse than no comparison

The first version of this compared honestly and was unusable. A perfectly
ordinary Deployment produced ten differences, of which one mattered:

```text
metadata.annotations                          {"deployment.kubernetes.io/revision":"22"}
metadata.labels.argocd.argoproj.io/instance   ak-super-auto
spec.progressDeadlineSeconds                  600
spec.replicas                                 1
spec.revisionHistoryLimit                     10
spec.strategy                                 …
spec.template.spec.containers[…].image        super-auto-develop → …:6520
spec.template.spec.schedulerName              default-scheduler
spec.template.spec.securityContext            {}
spec.template.spec.volumes[…].hostPath.type   ""
```

Nine of those rows are Kubernetes describing itself, and the tenth is an image
nobody can account for sitting in the middle of them looking identical. A reader
learns within a day that this list is noise, and after that the one row that
matters is invisible for exactly as long as it takes for it to cause an
incident.

Two separate things fix it, and keeping them separate is the point.

**Normalisation** erases differences that were never differences. A Deployment
whose source omits `replicas` and whose live object says `replicas: 1` is not
two states, one of which is excusable — it is one state written two ways,
because Kubernetes defaults the field to 1. Emitting that as a difference and
then hiding it behind a disclosure would still be telling the reader something
untrue, just more quietly. Seven of the ten rows above disappear here.

**Classification** explains what genuinely remains.
`deployment.kubernetes.io/revision` really is in the cluster and really is not
in Git, and pretending otherwise would be lying about the object. It is kept,
labelled with who wrote it, and put behind a disclosure. Two of the ten rows end
up here.

One row is left in front of the reader.

### Which defaults, and why so few

A default is removed only when the source does not set the field *and* the live
value is exactly the documented default. A field set to anything else still
shows, which is what keeps `replicas: 2` in Git against `replicas: 1` running
visible while `replicas` omitted against `replicas: 1` disappears.

The table is kind-aware and deliberately short. A generic database of every
default in the Kubernetes API would be a large thing that is wrong in places
nobody notices, and being wrong here means hiding drift silently. Kinds are
added when somebody has read the API reference for them; Deployment is the only
one so far.

```text
Deployment    replicas 1 · progressDeadlineSeconds 600 · revisionHistoryLimit 10
              strategy RollingUpdate 25% / 25%
Pod spec      restartPolicy Always · dnsPolicy ClusterFirst
              schedulerName default-scheduler · terminationGracePeriodSeconds 30
              securityContext {} · serviceAccountName default
Container     terminationMessagePath · terminationMessagePolicy File
              resources {} · imagePullPolicy derived from the image tag
Volume        hostPath.type ""
```

Two of those are worth a note. The pull policy is the only rule that depends on
another field: an untagged image or one tagged `latest` is pulled every time and
anything else only when missing, so it is computed rather than looked up.
`hostPath.type` is the only place where an omitted field and an empty string are
treated as the same thing — that is true for this field and false for a great
many others, so it is written down for this field rather than applied as a rule.

### What a difference does and does not claim

Each row says where a field is, not whose fault it is. A container image that
differs is labelled `Container image` and shows both values; it is not labelled
drift, because a pipeline may have rewritten the tag between the commit and the
apply, and this comparison cannot tell that from somebody editing the cluster.

Anything unrecognised stays in front of the reader. Showing one field too many
costs a moment; hiding one costs a drift nobody notices, and the failure is
silent.

### Normalising is where this feature could quietly lie

Comparing the two objects as they arrive produces a difference on every field
Kubernetes writes for its own bookkeeping, and a panel that always says "47
differences" is a panel nobody reads. Stripping everything that appears only in
the live object is the opposite mistake and a worse one: a field somebody
deleted from Git and never removed from the cluster looks exactly like a field
the API server defaulted, and hiding both hides real drift.

So a field is removed only when it is known to be written by Kubernetes rather
than by a person, and the list stays short because every entry on it is a place
drift could hide:

```text
status                        the cluster's report, never desired state
metadata.uid                  does not exist until the object does
metadata.resourceVersion      the concurrency counter, changes on every write
metadata.generation           a counter about spec rather than part of it
metadata.creationTimestamp    a fact about the cluster
metadata.managedFields        which controller owns which field
metadata.selfLink             a URL the API server used to serve
last-applied-configuration    a copy of a previous manifest
argocd.argoproj.io/tracking-id  written by Argo CD after applying
creationTimestamp: null       serialisation artefact in embedded metadata
```

Two are deliberately left in. `metadata.deletionTimestamp` will never be in a
manifest, which is an argument for ignoring it and a better argument for the
opposite: an object being deleted is not an object that matches its manifest,
and that is exactly what somebody opening this panel needs to know. Labels the
cluster carries and the manifest does not are kept for a different reason —
`app.kubernetes.io/instance` is Argo CD's label-tracking marker and also what
Helm writes on everything it installs, so discarding it would mean silently
throwing away a label a chart genuinely declared.

One correction is invisible and load-bearing. A number read from YAML arrives
as a float and the same number from the API server arrives as an integer,
because the dynamic client decodes whole numbers that way. Both sides go
through the same decoder before anything is compared; without it, every replica
count, port and timeout in every repository would report as drift, and the
feature would not be useless so much as confidently wrong.

### Reordering a container is not a change

A container moving to the front of a pod spec must not report every field of
both containers as changed. A reader who sees that once stops believing the
panel, so lists whose elements name themselves are matched by that name and
addressed by it:

```text
spec.template.spec.containers[name=api].image
```

The table is short and taken from the merge keys Kubernetes declares for those
fields — containers, init and ephemeral containers, `env`, `volumes` by name,
`volumeMounts` by mount path, ports by whichever of name, `containerPort` or
`port` identifies every element on both sides. Anything not on it is compared
by position, which is right for the lists that really are ordered, `command`
and `args` among them. This is not a strategic-merge implementation and is not
trying to be one.

### What a difference does and does not claim

Each row says where a field is, not whose fault it is. `Changed` usually means
what it looks like. `Only in the cluster` may be drift, or a default the API
server filled in, or a sidecar a mesh injected, or something a controller wrote
back — and this comparison reads two objects, so it cannot tell those apart and
does not pretend to.

### Secret values do not cross the boundary

A comparison is rendered whenever the panel is open, which is not the same as
somebody asking to see a secret. For a kind the catalogue marks sensitive, the
`data` and `stringData` values are withheld and the difference says only that
the key differs:

```text
data.DB_PASSWORD    Value differs
```

The key name is the useful half and is not itself secret — it is already a
count on the resource list and a name in the inspector. The redaction covers
the whole map as well as its entries, because a `data` block missing on one
side would otherwise be one difference carrying every value in it.

### Where this and Argo CD can disagree

Argo CD renders Helm and Kustomize sources, runs its own normalisation, and
honours `ignoreDifferences` rules. This reads one file and compares fields. The
two can therefore disagree, and when they do the panel shows both facts and
corrects neither:

```text
Argo CD         Synced
Source vs Live  1 meaningful difference
```

Assuming one of them must be wrong is how a panel starts lying. The known
sources of honest disagreement are rendered sources this slice does not render,
`ignoreDifferences` rules it does not read, defaults for kinds whose API
reference nobody has read into the table yet, and semantically equal values
spelled differently — `cpu: 0.1` and `cpu: 100m` are the same quantity and this
reports them as a difference.

Comparison is offered only for a manifest located exactly. Generated sources,
an ambiguous match, a repository that could not be read, a document that does
not parse and an object that no longer exists are five different situations
with five different things to do about them, so each is its own state rather
than one "diff failed". None of them is shown as an error: "unavailable" is
what a Helm Application will always be, and colouring that red would teach
people to ignore the panel.

Everything here is read-only. Nothing writes to the cluster and nothing writes
to Git.

---

## When the repository will not open

Of those five states, one has somewhere further to go. A Helm Application will
never have a file to compare and a document that does not parse names the file
to go and fix, but a repository that could not be read is a sentence from git
standing in for half a dozen different problems with half a dozen different
people to go and ask:

```text
fatal: Could not read from remote repository.
```

That sentence is true and useless. Git is not installed, or is installed
somewhere the PATH of a window started by launchd does not reach. The host does
not resolve, or resolves and does not answer, or answers slowly. The key is not
one the server knows, or is one it knows perfectly well belonging to the other
of somebody's two accounts. The account is right and has never been given this
repository. The repository was renamed last month. Each of those is a different
afternoon.

So each is asked separately, in the order they sit in, and the answers are shown
the way a failed cluster connection already is:

```text
Git                ✓  Found on the PATH this application can see.
Repository URL     ✓  Read as an ssh repository.
Git host           ✓  gitlab.example.com accepted a connection.      412ms
SSH agent          ?  No ssh-agent is visible to this application.
Repository         ✕  The project you were looking for could not be
                      found or you don't have permission to view it.
```

Nothing there clones, writes or changes a credential. A host is asked whether it
is listening; a server is asked for a list of refs rather than a repository,
because `ls-remote` is one round trip and proves the same thing a clone would
have proved expensively.

### The mark that is neither a tick nor a cross

`?` is a check that ran and settled nothing, and it exists for the ssh-agent.
A key with no passphrase authenticates without an agent and always has, so
absence is not a failure — but this is a window rather than a terminal, `ssh-add`
puts a key somewhere found through an environment variable, and a desktop
application does not always inherit the environment a shell has. That is worth
saying and not worth colouring red.

### Two questions that look like one

Authentication and authorisation are asked separately because servers answer
them separately. `Test SSH identity` runs `ssh -T` and reads the greeting:

```text
Authenticated as octocat. This says who you are, not what you may read.
```

The account is reported only when the reply matches a form written down here —
GitHub's, GitLab's, Bitbucket's. A server whose greeting is unrecognised gets
the honest answer, that authentication worked and the identity could not be
determined. Two accounts on one host is precisely the situation this exists for,
and naming the wrong one would be worse than naming none.

### The half the server withheld

For the most common failure there is genuinely more than one cause, and the
server has declined to say which:

```text
The server would not hand over this repository. It did not say whether the
repository is missing or your account may not read it.

• Your account has not been given access to this repository
• Authentication succeeded as a different account from the one with access
• The repository was moved, renamed or deleted
• The path in the Argo CD Application is wrong
```

GitLab and several others answer "no such project" and "not yours" with the same
words on purpose, so that a stranger cannot map a private namespace by reading
error messages. Picking one of them here would be inventing what the server
withheld, so both stay.

### Copying the question rather than the answer

```text
git ls-remote https://gitlab.example.com/acme/infra.git HEAD
```

Running that in a terminal answers something this application cannot answer
about itself: whether a shell can read the repository when this window cannot,
which separates a credential problem from an environment one. The URL is
scrubbed and there is nothing else in it to leak, because this application holds
no credential to put in a command in the first place.

### What it will not do

It will not turn off host key checking. `StrictHostKeyChecking=no` is the
convenient fix for a host key failure and it is the one thing a tool must never
do on somebody's behalf, so a rejected host key is diagnosed as its own state
and left alone.

It will not edit `~/.ssh/config`, choose a key, write a Host block, touch
`known_hosts` or create a config that is not there. A missing config is normal —
ssh works without one — and writing into somebody's `.ssh` directory because a
comparison failed would be deciding something about their authentication that is
not this application's to decide. Where an ssh config does exist there is a
button that shows it in the file manager, resolved for the platform rather than
spelled `~/.ssh/config`, and revealing rather than opening because that file has
no extension and asking a desktop to guess which application edits it goes wrong
often enough to be worse than useless.

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
