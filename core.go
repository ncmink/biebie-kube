// Biebie Kube is the Kubernetes half of the biebie.net desktop family.
//
// This file wires the internal services together and holds nothing else. Each
// Wails service lives in its own service_*.go file beside it, and every rule
// lives in internal/... — the application layer only delegates. Kubernetes
// logic never reaches the frontend, and VPN logic never reaches this
// application at all.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/access"
	"biebie-kube/internal/argocd"
	"biebie-kube/internal/autoimport"
	"biebie-kube/internal/cluster"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/kubeconfig"
	"biebie-kube/internal/logs"
	"biebie-kube/internal/manifest"
	"biebie-kube/internal/portforward"
	"biebie-kube/internal/resources"
	"biebie-kube/internal/store"
	"biebie-kube/internal/terminal"
)

// appVersion is shown in Settings, sent as the Kubernetes user agent, and
// compared with GitHub Releases. Release builds stamp it with
// -X main.appVersion=…; a var is required so -X can replace it.
var appVersion = "0.2.6"

// Events published to the frontend by the application layer itself. The
// per-domain events are declared by the packages that emit them.
const (
	// EventAccessChanged reports a customer network state change.
	EventAccessChanged = "access:changed"

	// EventOpenCluster asks the UI to navigate, after a handoff resolved.
	EventOpenCluster = "app:openCluster"

	// EventHandoffFailed reports a handoff that could not be honoured.
	EventHandoffFailed = "app:handoffFailed"
)

// Core owns everything the services share.
//
// Wails v3 registers services rather than one binding object, and several of
// them need the same cluster manager and the same event channel. Rather than
// let each build its own, they are constructed once here and handed out.
type Core struct {
	store   *store.Store
	configs *kubeconfig.Service
	imports *autoimport.Service

	clusters *cluster.Manager
	access   *access.Client
	launcher *access.Launcher
	server   *access.Server

	resources *resources.Service
	manifests *manifest.Service
	logs      *logs.Service
	terminals *terminal.Service
	forwards  *portforward.Service
	argocd    *argocd.Service
}

// NewCore constructs the application's services.
func NewCore() (*Core, error) {
	statePath, err := store.DefaultPath()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(statePath)
	if err != nil {
		return nil, err
	}

	core := &Core{store: st}
	core.configs = kubeconfig.NewService(st, filepath.Join(filepath.Dir(statePath), "kubeconfigs"))

	accessClient, err := access.NewClient()
	if err != nil {
		return nil, err
	}
	core.access = accessClient
	core.launcher = access.NewLauncher()

	events := emitter{}

	clusters := cluster.NewRepository(st)
	core.imports = autoimport.NewService(core.configs, clusters, st)

	core.clusters = cluster.NewManager(
		clusters,
		core.configs,
		kube.NewFactory(appVersion),
		accessClient,
		events,
	)

	core.resources = resources.NewService(core.clusters, events)

	// The manager reports that a watch fired; the resource service is what
	// knows which table that changes and what to send for it.
	core.clusters.OnResources(core.resources.OnResourceChange)

	core.manifests = manifest.NewService(core.clusters, core.resources)
	core.logs = logs.NewService(core.clusters, events)
	core.terminals = terminal.NewService(core.clusters, events)
	core.forwards = portforward.NewService(core.clusters, events)

	// Opening the Argo CD UI is a port forward like any other, so the Argo CD
	// service borrows the one that already owns them rather than dialling a
	// tunnel the port-forward panel would know nothing about.
	core.argocd = argocd.NewService(core.clusters, core.forwards)

	return core, nil
}

// Start brings up the parts that need a running application.
//
// Biebie Kube listens so Biebie Access can report a profile coming up, and so
// a second launch can hand over its deep link instead of opening a duplicate
// window. Failing to bind is not fatal — the application simply loses those
// two conveniences.
func (c *Core) Start(onLink func(string)) {
	server, err := access.NewServer(access.ServerOptions{
		OnSessionChanged: c.onAccessSessionChanged,
		OnOpenLink:       onLink,
		OnError:          func(error) {},
	})
	if err == nil {
		if err := server.Start(); err == nil {
			c.server = server
		}
	}

	// A first run with no kubeconfig imported would show an empty window with
	// no obvious next step, so the conventional location is indexed silently.
	if len(c.store.Read().Kubeconfigs) == 0 {
		_, _ = c.configs.ImportDefault()
	}

	// Contexts become clusters before the window opens, so the first screen
	// already lists what the engineer can reach. This runs on every launch
	// rather than only the first, because a context added with kubectl since
	// last time is exactly the case worth catching — and it only ever considers
	// a context once, so nothing deleted comes back.
	c.imports.Sync()
}

// Stop releases every session before the process exits, so no port forward or
// exec stream is left holding a socket.
func (c *Core) Stop() {
	c.forwards.StopAll()
	c.terminals.CloseAll()
	c.logs.StopAll()
	c.clusters.CloseAll()
	if c.server != nil {
		_ = c.server.Close()
	}
}

// onAccessSessionChanged reacts to Biebie Access reporting a profile moved.
//
// This is what completes the second scenario: a cluster left waiting for a
// customer network retries by itself when that network comes up, with no
// restart of either application.
func (c *Core) onAccessSessionChanged(event bctx.AccessSessionChanged) {
	c.access.Forget(event.ProfileID)
	emit(EventAccessChanged, event)

	if event.State != bctx.AccessConnected {
		return
	}
	c.clusters.RetryWaiting(context.Background(), event.ProfileID)
}

// emitter adapts the application to the event interface the internal services
// depend on, so none of them import Wails.
type emitter struct{}

func (emitter) Emit(event string, data any) { emit(event, data) }

// emit publishes an event to the frontend.
//
// The application is fetched rather than captured because services are
// constructed before application.New runs, and a nil application before then
// is an ordinary state: an event emitted during construction has no window to
// reach yet.
func emit(event string, data any) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(event, data)
}

// describe strips internal detail from an error before it crosses to the UI.
func describe(err error) error {
	if err == nil {
		return nil
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		if trimmed := strings.ReplaceAll(err.Error(), home, "~"); trimmed != err.Error() {
			return fmt.Errorf("%s", trimmed)
		}
	}
	return err
}
