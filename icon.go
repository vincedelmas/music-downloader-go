package main

import (
	"embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/icon.png
var iconFS embed.FS

var appIcon = func() fyne.Resource {
	b, _ := iconFS.ReadFile("assets/icon.png")
	return fyne.NewStaticResource("icon.png", b)
}()
