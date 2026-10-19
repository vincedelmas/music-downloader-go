package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type browserEntry struct {
	name      string
	path      string
	directory bool
}

func readBrowserEntries(dir string, hidden bool) ([]browserEntry, error) {
	nodes, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	entries := make([]browserEntry, 0, len(nodes))
	for _, n := range nodes {
		if !hidden && strings.HasPrefix(n.Name(), ".") {
			continue
		}

		path := filepath.Join(dir, n.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}

		entries = append(entries, browserEntry{n.Name(), path, info.IsDir()})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].directory != entries[j].directory {
			return entries[i].directory
		}

		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	return entries, nil
}

// A separate OS window makes the browser genuinely resizable by dragging its edges.
func browsePath(a fyne.App, initial string, foldersOnly bool, chosen func(string)) fyne.Window {
	title := "Choose bookmarks file"
	if foldersOnly {
		title = "Choose music folder"
	}

	w := a.NewWindow(title)
	w.Resize(fyne.NewSize(880, 600))
	path := widget.NewEntry()
	path.SetPlaceHolder("Folder path")
	description := widget.NewLabel("")
	description.Truncation = fyne.TextTruncateEllipsis
	var entries []browserEntry
	current := ""
	selected := ""
	showHidden := widget.NewCheck("Hidden files", nil)
	showHidden.SetChecked(!foldersOnly)
	choose := widget.NewButtonWithIcon("Choose file", theme.ConfirmIcon(), func() {
		if selected != "" {
			chosen(selected)
			w.Close()
		}
	})
	choose.Importance = widget.HighImportance
	if foldersOnly {
		choose.SetText("Choose this folder")
	} else {
		choose.Disable()
	}

	list := widget.NewList(func() int {
		return len(entries)
	}, func() fyne.CanvasObject {
		return container.NewBorder(nil, nil, widget.NewIcon(theme.DocumentIcon()), nil, widget.NewLabel(""))
	}, func(i widget.ListItemID, o fyne.CanvasObject) {
		row := o.(*fyne.Container)
		row.Objects[0].(*widget.Label).SetText(entries[i].name)
		icon := theme.DocumentIcon()
		if entries[i].directory {
			icon = theme.FolderIcon()
		}

		row.Objects[1].(*widget.Icon).SetResource(icon)
	})
	var navigate func(string)
	navigate = func(dir string) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		next, err := readBrowserEntries(abs, showHidden.Checked)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		list.UnselectAll()
		entries = next
		current = abs
		path.SetText(abs)
		selected = ""
		description.SetText(fmt.Sprintf("%d items", len(entries)))
		if foldersOnly {
			selected = current
			choose.Enable()
		} else {
			choose.Disable()
		}

		list.Refresh()
	}
	list.OnSelected = func(i widget.ListItemID) {
		if i < 0 || i >= len(entries) {
			return
		}

		entry := entries[i]
		if entry.directory {
			navigate(entry.path)
			return
		}

		if foldersOnly {
			return
		}

		selected = entry.path
		description.SetText(entry.name)
		choose.Enable()
	}
	path.OnSubmitted = navigate
	up := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		navigate(filepath.Dir(current))
	})
	goTo := widget.NewButton("Go", func() {
		navigate(path.Text)
	})
	showHidden.OnChanged = func(bool) {
		navigate(current)
	}
	cancel := widget.NewButton("Cancel", w.Close)
	footer := container.NewBorder(nil, nil, description, container.NewHBox(cancel, choose))
	top := container.NewVBox(container.NewBorder(nil, nil, up, goTo, path), showHidden)
	w.SetContent(container.NewPadded(container.NewBorder(top, footer, nil, nil, list)))
	start := initial
	if info, err := os.Stat(start); err == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}

	for {
		if info, err := os.Stat(start); err == nil && info.IsDir() {
			break
		}

		parent := filepath.Dir(start)
		if parent == start || start == "" || start == "." {
			start, _ = os.UserHomeDir()
			break
		}

		start = parent
	}

	navigate(start)
	w.Show()
	return w
}
