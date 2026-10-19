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
	if err := os.WriteFile(c.BookmarkPath, []byte(`{"roots":{"bookmark_bar":{"children":[{"type":"folder","name":"Music","children":[{"type":"url","name":"Artist - Song","url":"https://youtu.be/abc"}]}]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(c); err != nil {
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
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Button:
			if v.Text == "Load bookmarks" {
				load = v
			}
		case *widget.List:
			tracks = v
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
	if tracks.Length() != 1 {
		t.Fatalf("expected one loaded track, got %d", tracks.Length())
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
