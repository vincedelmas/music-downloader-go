// Package assets embeds the application icon for the desktop UI and installer.
package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var iconPNG []byte

// Icon is the application icon shared by the desktop UI and installer.
var Icon = fyne.NewStaticResource("icon.png", iconPNG)
