package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestDesktopLoadsBookmarks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("APPDATA", root)
	c := testConfig(t)
	if err := os.WriteFile(
		c.BookmarkPath,
		[]byte(`{"roots":{"bookmark_bar":{"children":[{"type":"folder","name":"Music","children":[{"type":"url","name":"Artist - Song","url":"https://youtu.be/abc"},{"type":"url","name":"Other - Track","url":"https://youtu.be/def"}]}]}}}`),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	if err := saveConfig(c); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(c.MusicDirectory, "Artist - Song.mp3"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	w := buildDesktop(a)
	defer w.Close()
	w.Show()
	var tabs *container.AppTabs
	var findTabs func(fyne.CanvasObject)
	findTabs = func(o fyne.CanvasObject) {
		if v, ok := o.(*container.AppTabs); ok {
			tabs = v
		}

		if v, ok := o.(*fyne.Container); ok {
			for _, child := range v.Objects {
				findTabs(child)
			}
		}
	}
	findTabs(w.Content())
	if tabs == nil {
		t.Fatal("navigation missing")
	}

	if len(tabs.Items) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tabs.Items))
	}

	var load *widget.Button
	var tracks *widget.List
	var filter *widget.Select
	var labels []*widget.Label
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Button:
			if v.Text == "Load bookmarks" {
				load = v
			}
		case *widget.List:
			tracks = v
		case *widget.Select:
			if len(v.Options) > 0 && v.Options[0] == "All" {
				filter = v
			}
		case *widget.Label:
			labels = append(labels, v)
		case *fyne.Container:
			for _, child := range v.Objects {
				walk(child)
			}
		}
	}
	walk(tabs.Items[0].Content)
	if load == nil || tracks == nil {
		t.Fatal("download controls missing")
	}

	test.Tap(load)
	if tracks.Length() != 2 {
		t.Fatalf("expected two loaded tracks, got %d", tracks.Length())
	}
	if filter == nil {
		t.Fatal("status filter missing")
	}
	for _, choice := range []struct{ group, detail string }{
		{"Completed", filepath.Join(c.MusicDirectory, "Artist - Song.mp3")},
		{"Queued", "https://youtu.be/def"},
	} {
		filter.SetSelected(choice.group)
		if tracks.Length() != 1 {
			t.Fatalf("%s: expected one track, got %d", choice.group, tracks.Length())
		}
		tracks.Select(0)
		found := false
		for _, label := range labels {
			if label.Text == choice.detail {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: selected track details do not match %q", choice.group, choice.detail)
		}
	}
	filter.SetSelected("Warnings")
	if tracks.Length() != 0 {
		t.Fatal("existing files must not be queued or warnings")
	}
	filter.SetSelected("All")
	tracks.Select(1)
	test.Tap(load)
	if tracks.Length() != 2 {
		t.Fatal("reloading bookmarks lost tracks")
	}
	// Confirm a build without a bundled tools folder still opens settings instead of exiting.
	tabs.SelectIndex(1)
	if os.Getenv("MUSIC_DOWNLOADER_SCREENSHOT") != "" {
		for i, name := range []string{"downloads", "preferences", "updates"} {
			tabs.SelectIndex(i)
			w.Content().Refresh()
			img := w.Canvas().Capture()
			f, err := os.Create(filepath.Join(os.TempDir(), "music-downloader-"+name+".png"))
			if err == nil {
				png.Encode(f, img)
				f.Close()
			}
		}
	}
}
