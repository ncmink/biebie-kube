package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// buildApplicationMenu wires the macOS menu bar to Biebie Kube actions.
func buildApplicationMenu(appService *AppService) *application.Menu {
	menu := application.NewMenu()

	app := menu.AddSubmenu("Biebie Kube")
	app.AddRole(application.About)
	app.AddSeparator()
	app.Add("Check for Updates…").OnClick(func(_ *application.Context) {
		_ = appService.CheckForUpdate()
	})
	app.Add("Settings…").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(_ *application.Context) {
			openSettings()
		})
	app.AddSeparator()
	app.AddRole(application.ServicesMenu)
	app.AddSeparator()
	app.AddRole(application.Hide)
	app.AddRole(application.HideOthers)
	app.AddRole(application.ShowAll)
	app.AddSeparator()
	app.AddRole(application.Quit)

	menu.AddRole(application.FileMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.ViewMenu)
	menu.AddRole(application.WindowMenu)
	menu.AddRole(application.HelpMenu)

	return menu
}

func openSettings() {
	present()
	emit(EventOpenSettings, true)
}
