package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"music-downloader-go/assets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func runDesktop() {
	application := app.NewWithID(appID)
	buildDesktop(application).ShowAndRun()
}

func buildDesktop(application fyne.App) fyne.Window {
	application.SetIcon(assets.Icon)
	application.Settings().SetTheme(newDesktopTheme())
	window := application.NewWindow("Music Downloader")
	window.Resize(fyne.NewSize(1040, 780))
	window.SetMaster()
	config, loadErr := loadConfig()
	status := widget.NewLabel("Load your bookmarks to get started.")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBar()
	// Downloads, selection, and progress.
	var tracks []Track
	var states []string
	var visible []int
	var details []string
	list := widget.NewList(func() int {
		return len(visible)
	}, func() fyne.CanvasObject {
		name := widget.NewLabel("")
		name.TextStyle.Bold = true
		name.Truncation = fyne.TextTruncateEllipsis
		state := widget.NewLabel("")
		state.Importance = widget.LowImportance
		return container.NewBorder(nil, nil, widget.NewIcon(theme.MediaMusicIcon()), state, name)
	}, func(i widget.ListItemID, o fyne.CanvasObject) {
		row := o.(*fyne.Container)
		index := visible[i]
		row.Objects[0].(*widget.Label).SetText(tracks[index].Name)
		label := row.Objects[2].(*widget.Label)
		label.SetText(states[index])
		label.Importance = widget.LowImportance
		switch trackStateGroup(states[index]) {
		case "Warnings":
			label.Importance = widget.WarningImportance
		case "Failed":
			label.Importance = widget.DangerImportance
		case "Completed":
			label.Importance = widget.SuccessImportance
		}

		row.Refresh()
	})
	detail := widget.NewLabel("Select a track for details.")
	detail.Wrapping = fyne.TextWrapWord
	selected := -1
	list.OnSelected = func(i widget.ListItemID) {
		selected = visible[i]
		detail.SetText(details[selected])
	}
	list.OnUnselected = func(widget.ListItemID) {
		selected = -1
		detail.SetText("Select a track for details.")
	}
	groups := []string{"Queued", "In progress", "Completed", "Warnings", "Failed", "Skipped", "Cancelled"}
	filter := widget.NewSelect(append([]string{"All"}, groups...), nil)
	filter.SetSelected("All")
	counts := widget.NewLabel("")
	counts.Wrapping = fyne.TextWrapWord
	refreshList := func() {
		previous := selected
		list.UnselectAll()
		visible = nil
		totals := make(map[string]int)
		for i, state := range states {
			group := trackStateGroup(state)
			totals[group]++
			if filter.Selected == "All" || filter.Selected == group {
				visible = append(visible, i)
			}
		}
		var summary []string
		for _, group := range groups {
			if totals[group] > 0 {
				summary = append(summary, fmt.Sprintf("%d %s", totals[group], strings.ToLower(group)))
			}
		}
		counts.SetText(strings.Join(summary, "  ·  "))
		list.Refresh()
		for i, index := range visible {
			if index == previous {
				list.Select(i)
				break
			}
		}
	}
	filter.OnChanged = func(string) { refreshList() }
	var cancel context.CancelFunc
	busy := false
	var controls []fyne.Disableable
	setBusy := func(v bool) {
		busy = v
		for _, control := range controls {
			if v {
				control.Disable()
			} else {
				control.Enable()
			}
		}
	}
	showErr := func(err error) {
		dialog.ShowError(err, window)
	}
	openPath := func(path string) {
		u := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
		if !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}

		if err := application.OpenURL(u); err != nil {
			showErr(err)
		}
	}
	stop := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		if cancel != nil {
			cancel()
			status.SetText("Cancelling…")
		}
	})
	stop.Disable()
	load := widget.NewButtonWithIcon("Load bookmarks", theme.ViewRefreshIcon(), func() {
		loaded, err := parseBookmarks(config)
		if err != nil {
			showErr(err)
			return
		}

		list.UnselectAll()
		visible = nil
		tracks = loaded
		states = make([]string, len(tracks))
		details = make([]string, len(tracks))
		files, duplicate := trackPaths(config.MusicDirectory, tracks)
		for i, t := range tracks {
			states[i] = "Queued"
			details[i] = t.URL
			if duplicate[i] {
				states[i], details[i] = "Skipped", "Duplicate link"
			} else if _, err := os.Stat(files[i]); err == nil {
				states[i], details[i] = "Already downloaded", files[i]
			} else if !os.IsNotExist(err) {
				states[i], details[i] = "Failed", err.Error()
			}
		}
		refreshList()
		progress.SetValue(0)
		status.SetText(fmt.Sprintf("%d YouTube tracks found. Existing files will be skipped.", len(tracks)))
	})
	runBatch := func(refreshOnly bool) {
		if len(tracks) == 0 {
			showErr(fmt.Errorf("load your bookmarks first"))
			return
		}

		if err := config.Validate(); err != nil {
			showErr(err)
			return
		}

		tools, err := resolveTools()
		if err != nil {
			showErr(err)
			return
		}

		ctx, cancelDownload := context.WithCancel(context.Background())
		cancel = cancelDownload
		setBusy(true)
		stop.Enable()
		progress.SetValue(0)
		if refreshOnly {
			status.SetText("Refreshing song details…")
		} else {
			status.SetText("Downloading…")
		}

		cfg := config
		batch := append([]Track(nil), tracks...)
		go func() {
			defer cancelDownload()
			done := 0
			var eventMu sync.Mutex
			emit := func(e TrackEvent) {
				eventMu.Lock()
				defer eventMu.Unlock()
				fyne.Do(func() {
					states[e.Index] = e.State
					details[e.Index] = e.Detail
					refreshList()
					switch e.State {
					case "Completed", "Updated", "Saved with warning", "Already downloaded", "Skipped", "Failed", "Cancelled":
						done++
						progress.SetValue(float64(done) / float64(len(batch)))
					}

					status.SetText(fmt.Sprintf("%d / %d finished", done, len(batch)))
				})
			}
			d := newDownloader(cfg, tools)
			var err error
			if refreshOnly {
				err = d.refreshDetails(ctx, batch, emit)
			} else {
				err = d.process(ctx, batch, emit)
			}

			fyne.Do(func() {
				setBusy(false)
				stop.Disable()
				cancel = nil
				if err != nil {
					status.SetText(err.Error())
				} else {
					if refreshOnly {
						status.SetText("Song details refreshed.")
					} else {
						status.SetText("Finished. Check the status counts and select a track for details.")
					}
				}
			})
		}()
	}
	start := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), func() {
		runBatch(false)
	})
	refreshDetails := widget.NewButtonWithIcon("Refresh song details", theme.ViewRefreshIcon(), func() {
		runBatch(true)
	})
	start.Importance = widget.HighImportance
	openMusic := widget.NewButtonWithIcon("Open music folder", theme.FolderOpenIcon(), func() {
		if err := os.MkdirAll(config.MusicDirectory, 0755); err != nil {
			showErr(err)
			return
		}

		openPath(config.MusicDirectory)
	})
	downloads := container.NewBorder(
		container.NewVBox(
			pageHeading("Your music", "Download from your bookmark collection."),
			container.NewHBox(load, start, refreshDetails, openMusic),
			progress,
			container.NewBorder(nil, nil, widget.NewLabel("Show"), nil, filter),
			counts,
		),
		container.NewVBox(widget.NewSeparator(), detail),
		nil,
		nil,
		list,
	)

	// Bookmark source and naming preferences.
	bookmark := widget.NewEntry()
	bookmark.SetText(config.BookmarkPath)
	music := widget.NewEntry()
	music.SetText(config.MusicDirectory)
	folder := widget.NewSelect(nil, nil)
	folderPositions := make(map[string]int)
	refreshFolderChoices := func() error {
		names, err := bookmarkFolders(bookmark.Text)
		if err != nil {
			return err
		}

		folder.ClearSelected()
		folder.Options = nil
		folderPositions = make(map[string]int)
		for _, n := range names {
			parts := strings.SplitN(n, ": ", 2)
			if len(parts) != 2 {
				continue
			}

			index, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}

			name := parts[1]
			base := name
			for count := 2; ; count++ {
				if _, exists := folderPositions[name]; !exists {
					break
				}

				name = fmt.Sprintf("%s (%d)", base, count)
			}

			folder.Options = append(folder.Options, name)
			folderPositions[name] = index
			if index == config.BookmarkPosition {
				folder.SetSelected(name)
			}
		}

		folder.Refresh()
		return nil
	}
	refreshFolders := func() {
		if err := refreshFolderChoices(); err != nil {
			showErr(err)
		}
	}
	scan := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), refreshFolders)
	bookmark.OnChanged = func(string) {
		folder.ClearSelected()
		folder.Options = nil
		folder.Refresh()
	}
	bookmark.OnSubmitted = func(string) {
		refreshFolders()
	}
	selectFile := widget.NewButtonWithIcon("Browse…", theme.FolderOpenIcon(), func() {
		browsePath(application, bookmark.Text, false, func(path string) {
			bookmark.SetText(path)
			refreshFolders()
		})
	})
	selectDir := widget.NewButtonWithIcon(
		"Browse…",
		theme.FolderOpenIcon(),
		func() {
			browsePath(application, music.Text, true, func(path string) {
				music.SetText(path)
			})
		},
	)

	pattern := widget.NewEntry()
	pattern.SetText(config.NamePattern)
	pattern.SetPlaceHolder("{artist} - {title}")
	example := widget.NewEntry()
	example.SetText("Daft Punk - One More Time")
	preview := widget.NewLabel("")
	preview.Wrapping = fyne.TextWrapWord
	updatePreview := func() {
		p, err := compileNamePattern(pattern.Text)
		if err != nil {
			preview.SetText(err.Error())
			return
		}

		artist, title, err := p.parse(example.Text)
		if err != nil {
			preview.SetText("This name doesn't match the pattern.")
			return
		}

		preview.SetText("Artist: " + artist + "   •   Title: " + title)
	}
	pattern.OnChanged = func(string) {
		updatePreview()
	}
	example.OnChanged = func(string) {
		updatePreview()
	}
	updatePreview()
	metadata := widget.NewCheck("Add song details and album artwork", nil)
	metadata.SetChecked(config.FetchMetadata)
	concurrent := widget.NewSelect([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, nil)
	concurrent.SetSelected(strconv.Itoa(config.MaxConcurrent))
	quality := widget.NewSelect([]string{"128k", "192k", "256k", "320k"}, nil)
	quality.SetSelected(config.AudioQuality)
	save := widget.NewButtonWithIcon("Save settings", theme.DocumentSaveIcon(), func() {
		next := config
		next.BookmarkPath = strings.TrimSpace(bookmark.Text)
		next.MusicDirectory = strings.TrimSpace(music.Text)
		next.NamePattern = pattern.Text
		next.FetchMetadata = metadata.Checked
		if folder.Selected == "" {
			showErr(fmt.Errorf("find and select your bookmark folder"))
			return
		}

		pos, ok := folderPositions[folder.Selected]
		if !ok {
			showErr(fmt.Errorf("choose a bookmark folder"))
			return
		}

		next.BookmarkPosition = pos
		next.MaxConcurrent, _ = strconv.Atoi(concurrent.Selected)
		next.AudioQuality = quality.Selected
		if err := saveConfig(next); err != nil {
			showErr(err)
			return
		}

		config = next
		list.UnselectAll()
		tracks = nil
		states = nil
		details = nil
		refreshList()
		status.SetText("Settings saved. Load your bookmarks when ready.")

	})
	save.Importance = widget.HighImportance
	sourceCard := settingsCard("Your bookmarks", "Choose the music folder from your browser.", widget.NewForm(
		widget.NewFormItem("Bookmarks file", container.NewBorder(nil, nil, nil, selectFile, bookmark)),
		widget.NewFormItem("Music bookmarks", container.NewBorder(nil, nil, nil, scan, folder)),
	))
	nameCard := settingsCard("Bookmark names", "Use {artist} and {title}, with your separator between them.", widget.NewForm(
		widget.NewFormItem("Name pattern", pattern), widget.NewFormItem("Try a bookmark name", example), widget.NewFormItem("Preview", preview),
	))
	downloadCard := settingsCard("Your library", "", widget.NewForm(
		widget.NewFormItem("Save music to", container.NewBorder(nil, nil, nil, selectDir, music)),
		widget.NewFormItem("MP3 quality", quality), widget.NewFormItem("Downloads at once", concurrent), widget.NewFormItem("Song details", metadata),
	))
	settings := container.NewBorder(
		pageHeading("Preferences", ""),
		container.NewHBox(save),
		nil,
		nil,
		container.NewVScroll(container.NewVBox(sourceCard, nameCard, downloadCard)),
	)

	// Tool updates and application updates.
	toolsStatus := widget.NewLabel("")
	toolsStatus.Wrapping = fyne.TextWrapWord
	refreshTools := func() {
		_, err := resolveTools()
		if err != nil {
			toolsStatus.SetText("Download tools need to be installed.")
			return
		}

		toolsStatus.SetText("Ready to download.")
	}
	refreshTools()
	updateToolsButton := widget.NewButtonWithIcon("Update download tools", theme.ViewRefreshIcon(), func() {
		setBusy(true)
		ctx, cancelUpdate := context.WithCancel(context.Background())
		cancel = cancelUpdate
		stop.Enable()
		toolsStatus.SetText("Preparing tool update…")
		go func() {
			defer cancelUpdate()
			err := updateTools(ctx, func(s string) {
				fyne.Do(func() {
					toolsStatus.SetText(toolUpdateMessage(s))
				})
			})
			fyne.Do(func() {
				setBusy(false)
				stop.Disable()
				cancel = nil
				if err != nil {
					toolsStatus.SetText("Couldn’t update tools.\n" + err.Error())
				} else {
					refreshTools()
				}
			})
		}()
	})
	appStatus := widget.NewLabel("Check for a newer version.")
	appStatus.Wrapping = fyne.TextWrapWord
	var available githubRelease
	downloadUpdate := widget.NewButtonWithIcon("Download app update", theme.DownloadIcon(), func() {
		setBusy(true)
		r := available
		appStatus.SetText("Downloading " + r.Tag + "…")
		ctx, cancelUpdate := context.WithCancel(context.Background())
		cancel = cancelUpdate
		stop.Enable()
		go func() {
			defer cancelUpdate()
			path, err := downloadAppUpdate(ctx, r)
			fyne.Do(func() {
				setBusy(false)
				stop.Disable()
				cancel = nil
				if err != nil {
					showErr(err)
					appStatus.SetText("App update download failed.")
					return
				}

				appStatus.SetText("Update downloaded. Close the app and open the installer to finish.")
				openPath(filepath.Dir(path))
			})
		}()
	})
	downloadUpdate.Hide()
	check := widget.NewButton("Check for app updates", func() {
		setBusy(true)
		appStatus.SetText("Checking for updates…")
		go func() {
			ctx, cancelCheck := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelCheck()
			r, err := latestRelease(ctx, releaseRepo)
			fyne.Do(func() {
				setBusy(false)
				if err != nil {
					showErr(err)
					appStatus.SetText("Could not check for updates.")
					return
				}

				if strings.TrimPrefix(r.Tag, "v") == strings.TrimPrefix(version, "v") {
					appStatus.SetText("You have the latest release: " + r.Tag)
					downloadUpdate.Hide()
				} else {
					available = r
					appStatus.SetText("Version " + r.Tag + " is available.")
					downloadUpdate.Show()
				}
			})
		}()
	})
	releases := widget.NewButton("Release notes", func() {
		u, _ := url.Parse("https://github.com/" + releaseRepo + "/releases")
		if err := application.OpenURL(u); err != nil {
			showErr(err)
		}
	})
	updatesContent := container.NewVBox(
		pageHeading("Updates", ""),
		settingsCard(
			"Application",
			"Version "+version,
			container.NewVBox(appStatus, container.NewHBox(check, downloadUpdate, releases)),
		),
		settingsCard("Download tools", "", container.NewVBox(toolsStatus, updateToolsButton)),
	)
	if runtimeInstallAvailable() {
		install := widget.NewButton("Add to application menu", func() {
			setBusy(true)
			go func() {
				err := installForUser()
				fyne.Do(func() {
					setBusy(false)
					if err != nil {
						showErr(err)
					} else {
						dialog.ShowInformation(
							"Installed",
							"You can now find Music Downloader in your application menu and pin it to your taskbar.",
							window,
						)
					}
				})
			}()
		})
		controls = append(controls, install)
		updatesContent.Add(widget.NewSeparator())
		updatesContent.Add(install)
	}

	controls = append(controls,
		load, start, refreshDetails,
		save, bookmark, music, selectFile, selectDir, folder, scan,
		pattern, example, metadata, concurrent, quality,
		updateToolsButton, check, downloadUpdate,
	)

	// Navigation and shutdown.
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Downloads", theme.DownloadIcon(), downloads),
		container.NewTabItemWithIcon("Preferences", theme.SettingsIcon(), settings),
		container.NewTabItemWithIcon("Updates", theme.InfoIcon(), container.NewVScroll(updatesContent)),
	)
	if dir, err := configDir(); err == nil {
		if _, err = os.Stat(filepath.Join(dir, "config.json")); os.IsNotExist(err) {
			tabs.SelectIndex(1)
		}
	}

	window.SetContent(container.NewPadded(container.NewBorder(nil, container.NewBorder(nil, nil, nil, stop, status), nil, nil, tabs)))
	window.SetCloseIntercept(func() {
		if busy {
			dialog.ShowConfirm("Work is still running", "Cancel the current operation and quit?", func(ok bool) {
				if ok {
					if cancel != nil {
						cancel()
					}

					window.SetCloseIntercept(nil)
					window.Close()
				}
			}, window)
		} else {
			window.SetCloseIntercept(nil)
			window.Close()
		}
	})
	if _, err := os.Stat(config.BookmarkPath); err == nil {
		_ = refreshFolderChoices()
	}

	if loadErr != nil {
		dialog.ShowError(loadErr, window)
	}

	return window
}

func trackStateGroup(state string) string {
	switch state {
	case "Downloading", "Tagging":
		return "In progress"
	case "Completed", "Updated", "Already downloaded":
		return "Completed"
	case "Saved with warning":
		return "Warnings"
	default:
		return state
	}
}
