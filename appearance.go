package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type desktopTheme struct{ fyne.Theme }

func newDesktopTheme() fyne.Theme {
	return desktopTheme{theme.DefaultTheme()}
}

func (t desktopTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 83, G: 211, B: 179, A: 255}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 14, G: 23, B: 25, A: 255}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 9, G: 34, B: 29, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 232, G: 241, B: 238, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 15, G: 26, B: 29, A: 255}
	case theme.ColorNameButton, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 29, G: 45, B: 49, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 49, G: 77, B: 79, A: 255}
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return color.NRGBA{R: 44, G: 64, B: 67, A: 255}
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 126, G: 151, B: 152, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 24, G: 35, B: 39, A: 255}
	case theme.ColorNameSelection, theme.ColorNameFocus:
		return color.NRGBA{R: 42, G: 106, B: 93, A: 180}
	}

	return t.Theme.Color(n, theme.VariantDark)
}

func (t desktopTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case theme.SizeNamePadding:
		return 10
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return 8
	case theme.SizeNameCardRadius:
		return 12
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameHeadingText:
		return 21
	}

	return t.Theme.Size(n)
}

func pageHeading(title, subtitle string) fyne.CanvasObject {
	heading := canvas.NewText(title, color.NRGBA{R: 232, G: 241, B: 238, A: 255})
	heading.TextSize = 25
	heading.TextStyle.Bold = true
	sub := widget.NewLabel(subtitle)
	sub.Importance = widget.LowImportance
	sub.Wrapping = fyne.TextWrapWord
	if subtitle == "" {
		return container.NewVBox(heading)
	}

	return container.NewVBox(heading, sub)
}

func settingsCard(title, subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	return widget.NewCard(title, subtitle, container.NewPadded(content))
}

func toolUpdateMessage(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "yt-dlp"):
		return "Updating the downloader…"
	case strings.Contains(lower, "ffmpeg") || strings.Contains(lower, "ffprobe"):
		return "Updating audio tools…"
	case strings.Contains(lower, "deno"):
		return "Updating YouTube support…"
	default:
		return "Preparing download tools…"
	}
}
