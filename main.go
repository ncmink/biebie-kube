package main

import (
	"context"
	"embed"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	bctx "github.com/ncmink/biebie-protocol/context"
	"github.com/ncmink/biebie-protocol/deeplink"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/logs"
	"biebie-kube/internal/portforward"
	"biebie-kube/internal/resources"
	"biebie-kube/internal/shellenv"
	"biebie-kube/internal/terminal"
)

//go:embed all:frontend/dist
var assets embed.FS

// mainWindow names the single window, so code that needs to raise it can find
// it by name instead of guessing which window has focus.
const mainWindow = "main"

func init() {
	// Registering an event's payload type makes the binding generator emit a
	// typed listener for it, so a component cannot subscribe to one event and
	// read another's fields. Every event the application publishes is declared
	// here, including the ones the internal packages emit, because this is the
	// layer that knows the frontend exists.
	application.RegisterEvent[bctx.AccessSessionChanged](EventAccessChanged)
	application.RegisterEvent[HandoffResult](EventOpenCluster)
	application.RegisterEvent[string](EventHandoffFailed)

	application.RegisterEvent[domain.Session](cluster.EventSessionChanged)
	application.RegisterEvent[cluster.ResourceChange](cluster.EventResourcesChanged)
	application.RegisterEvent[resources.RowsChanged](resources.EventRows)
	application.RegisterEvent[domain.LogChunk](logs.EventChunk)
	application.RegisterEvent[domain.TerminalChunk](terminal.EventChunk)
	application.RegisterEvent[[]domain.PortForwardSession](portforward.EventChanged)
}

func main() {
	// The PATH is repaired before any kubeconfig is read. A cluster opened by
	// autoimport or by a deep link runs its exec plugin within moments of
	// startup, and a helper that was not found once is cached as a failure.
	// A failure here is not fatal: a kubeconfig with embedded credentials
	// needs no helper at all.
	if _, err := shellenv.Apply(context.Background()); err != nil {
		log.Printf("Biebie Kube could not read the login shell's PATH, so a credential helper may not be found: %v", err)
	}

	core, err := NewCore()
	if err != nil {
		log.Fatalf("Biebie Kube could not start: %v", err)
	}

	accessService := &AccessService{core: core}

	// A deep link that arrives before the window exists is held until the
	// frontend is listening. This happens whenever Biebie Access launches
	// Biebie Kube rather than switching to a running copy.
	links := newLinkQueue(func(link string) {
		accessService.OpenDeepLink(context.Background(), link)
	})
	appService := &AppService{core: core, links: links}

	app := application.New(application.Options{
		Name:        "Biebie Kube",
		Description: "Kubernetes workspace for the biebie.net family.",
		Services: []application.Service{
			application.NewService(appService),
			application.NewService(&ClusterService{core: core}),
			application.NewService(&ResourceService{core: core}),
			application.NewService(&LogService{core: core}),
			application.NewService(&TerminalService{core: core}),
			application.NewService(&PortForwardService{core: core}),
			application.NewService(&ArgoCDService{core: core}),
			application.NewService(accessService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// A deep link arriving while Biebie Kube is open must reach that
		// window. A second process would give the engineer two windows with
		// two sets of port forwards pointing at the same customer.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "net.biebie.kube",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				present()
				if link := deepLinkFrom(data.Args); link != "" {
					links.deliver(link)
				}
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	configureUpdater(app)
	appService.updater = app.Updater

	// macOS delivers a URL scheme launch as an application event rather than
	// an argument, so both paths feed the same queue.
	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(event *application.ApplicationEvent) {
		if link := event.Context().URL(); link != "" {
			links.deliver(link)
		}
	})

	core.Start(links.deliver)
	app.OnShutdown(core.Stop)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindow,
		Title:     "Biebie Kube",
		Width:     1440,
		Height:    900,
		MinWidth:  1100,
		MinHeight: 700,
		URL:       "/",
		// The window matches the dark surface the UI paints, so resizing does
		// not flash a light rectangle.
		BackgroundColour: application.NewRGB(28, 29, 31),
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 44,
		},
	})

	if link := deepLinkFrom(os.Args[1:]); link != "" {
		links.deliver(link)
	}

	if err := app.Run(); err != nil {
		log.Printf("Biebie Kube stopped: %v", err)
	}
}

// deepLinkFrom finds a biebie-kube:// argument.
//
// Desktop environments pass a deep link as a plain argument, sometimes among
// others, so the arguments are scanned rather than assuming position.
func deepLinkFrom(args []string) string {
	for _, arg := range args {
		if strings.EqualFold(deeplink.Scheme(arg), deeplink.SchemeKube) {
			return arg
		}
	}
	return ""
}
