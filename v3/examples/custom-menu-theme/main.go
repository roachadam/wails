package main

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	Variant = "WORKING-TREE"
	Commit  = "uncommitted"
)

//go:embed assets/index.html
var indexHTML string

func assetHandler() http.Handler {
	page := template.Must(template.New("index").Parse(indexHTML))
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(response, struct{ Variant, Commit string }{Variant, Commit}); err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
		}
	})
}

func customMenuTheme() *application.MenuBarTheme {
	return &application.MenuBarTheme{
		Default: &application.TextTheme{
			Background: application.NewRGBPtr(43, 23, 77),
			Text:       application.NewRGBPtr(247, 242, 255),
		},
		Hover: &application.TextTheme{
			Background: application.NewRGBPtr(83, 52, 131),
			Text:       application.NewRGBPtr(255, 255, 255),
		},
		Selected: &application.TextTheme{
			Background: application.NewRGBPtr(0, 109, 119),
			Text:       application.NewRGBPtr(255, 255, 255),
		},
	}
}

func main() {
	app := application.New(application.Options{
		Name:        "Custom Menu Theme POC",
		Description: "Owner-drawn Windows popup menu custom-theme demonstration",
		Assets: application.AssetOptions{
			Handler: assetHandler(),
		},
	})

	menu := app.NewMenu()
	themeMenu := menu.AddSubmenu("&Theme")
	themeMenu.Add("&Open").SetAccelerator("Ctrl+O")
	themeMenu.Add("&Save").SetAccelerator("Ctrl+S")
	themeMenu.Add("Disabled item").SetEnabled(false)
	themeMenu.AddSeparator()
	themeMenu.AddCheckbox("Checked item", true)
	themeMenu.AddRadio("Radio one", true)
	themeMenu.AddRadio("Radio two", false)
	nested := themeMenu.AddSubmenu("&Nested submenu")
	nested.Add("First nested item")
	nested.Add("Second nested item").SetEnabled(false)
	themeMenu.AddSeparator()
	themeMenu.Add("E&xit").OnClick(func(*application.Context) {
		app.Quit()
	})

	helpMenu := menu.AddSubmenu("&Help")
	helpMenu.Add("About this POC")

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Custom Popup Menu Theme — Before/After POC",
		Width:            880,
		Height:           620,
		BackgroundColour: application.NewRGB(24, 16, 48),
		Windows: application.WindowsWindow{
			Menu:  menu,
			Theme: application.Dark,
			CustomTheme: application.ThemeSettings{
				DarkModeMenuBar: customMenuTheme(),
			},
		},
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
