package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestBrowserNavigationAndSelection(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "Music")
	os.Mkdir(sub, 0700)
	file := filepath.Join(sub, "Bookmarks")
	os.WriteFile(file, []byte("{}"), 0600)
	os.WriteFile(filepath.Join(root, ".hidden"), nil, 0600)
	entries, err := readBrowserEntries(root, false)
	if err != nil || len(entries) != 1 || !entries[0].directory {
		t.Fatalf("entries=%+v, %v", entries, err)
	}

	entries, err = readBrowserEntries(root, true)
	if err != nil || len(entries) != 2 {
		t.Fatalf("hidden entries=%+v, %v", entries, err)
	}

	a := test.NewApp()
	a.Settings().SetTheme(newDesktopTheme())
	defer a.Quit()
	selected := ""
	w := browsePath(a, root, false, func(p string) {
		selected = p
	})
	if w.FixedSize() {
		t.Fatal("file browser must be resizable")
	}

	w.Resize(fyne.NewSize(1100, 760))
	if os.Getenv("MUSIC_DOWNLOADER_SCREENSHOT") != "" {
		f, err := os.Create(filepath.Join(os.TempDir(), "music-downloader-browser.png"))
		if err == nil {
			png.Encode(f, w.Canvas().Capture())
			f.Close()
		}
	}

	var list *widget.List
	var choose *widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.List:
			list = v
		case *widget.Button:
			if v.Text == "Choose file" {
				choose = v
			}
		case *fyne.Container:
			for _, c := range v.Objects {
				walk(c)
			}
		}
	}
	walk(w.Content())
	if list == nil || choose == nil {
		t.Fatal("file browser controls missing")
	}

	list.Select(0) // Directories sort first; clicking opens Music.
	if list.Length() != 1 {
		t.Fatalf("did not enter Music folder, %d items", list.Length())
	}

	list.Select(0)
	test.Tap(choose)
	if selected != file {
		t.Fatalf("selected %q, want %q", selected, file)
	}
	// Folder mode chooses the current directory without requiring a child folder.
	selected = ""
	folderWindow := browsePath(a, sub, true, func(p string) {
		selected = p
	})
	var selectFolder func(fyne.CanvasObject)
	selectFolder = func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Choose this folder" {
			test.Tap(b)
		}

		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				selectFolder(child)
			}
		}
	}
	selectFolder(folderWindow.Content())
	if selected != sub {
		t.Fatalf("selected folder %q", selected)
	}
}
