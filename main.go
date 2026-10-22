package main

import (
	"embed"
	"log"
	"os"

	"github.com/openutm/utm-adapter/internal/adapter"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/tray.png
var trayIcon []byte

func main() {
	service, err := adapter.NewService(adapter.ConfigPath())
	if err != nil {
		log.Fatalf("Cannot load configuration: %v", err)
	}
	var window *application.WebviewWindow
	app := application.New(application.Options{Name: "UTM Adapter", Description: "Ground control to Flight Blender", SingleInstance: &application.SingleInstanceOptions{UniqueID: "org.openutm.adapter", OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
		if window != nil {
			window.Show()
			window.Focus()
		}
	}}, Services: []application.Service{application.NewService(service)}, Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)}, Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}})
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "UTM Adapter", Hidden: os.Getenv("UTM_ADAPTER_START_HIDDEN") == "1", Width: 920, Height: 780, MinWidth: 740, MinHeight: 620, URL: "/", BackgroundColour: application.NewRGB(247, 249, 248)})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { event.Cancel(); window.Hide() })
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTemplateIcon(trayIcon)
	tray.SetTooltip("UTM Adapter · Ground control to Flight Blender")
	menu := app.NewMenu()
	menu.Add("Open UTM Adapter").OnClick(func(_ *application.Context) { window.Show(); window.Focus() })
	menu.AddSeparator()
	menu.Add("Resume connections").OnClick(func(_ *application.Context) {
		go func() {
			if e := service.Start(); e != nil {
				log.Print(e)
			}
		}()
	})
	menu.Add("Pause connections").OnClick(func(_ *application.Context) { go service.Stop() })
	menu.AddSeparator()
	menu.Add("Quit UTM Adapter").OnClick(func(_ *application.Context) { app.Quit() })
	tray.SetMenu(menu)
	app.OnShutdown(service.Stop)
	if err = service.Start(); err != nil {
		log.Printf("Connection startup: %v", err)
	}
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
}
