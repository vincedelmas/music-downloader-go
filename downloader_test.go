package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c := defaultConfig()
	c.BookmarkPath = filepath.Join(t.TempDir(), "Bookmarks")
	c.MusicDirectory = t.TempDir()
	return c
}

func TestParseBookmarks(t *testing.T) {
	c := testConfig(t)
	c.BookmarkPosition = 1
	data := `{"roots":{"bookmark_bar":{"children":[{"type":"url","name":"Other","url":"https://example.com"},{"type":"folder","name":"Music","children":[{"type":"url","name":"Artist - Track","url":"https://www.youtube.com/watch?v=1"},{"type":"folder","name":"Nested","children":[{"type":"url","name":"Another - Song","url":"https://youtu.be/2"}]},{"type":"url","name":"Ignore","url":"https://youtube.com.example.org/3"}]}]}}}`
	if err := os.WriteFile(c.BookmarkPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	tracks, err := parseBookmarks(c)
	if err != nil {
		t.Fatal(err)
	}

	if len(tracks) != 2 || tracks[0].Artist != "Artist" || tracks[0].Title != "Track" || tracks[1].Title != "Song" {
		t.Fatalf("unexpected tracks: %+v", tracks)
	}

	folders, err := bookmarkFolders(c.BookmarkPath)
	if err != nil || len(folders) != 1 || folders[0] != "1: Music" {
		t.Fatalf("folders=%v, err=%v", folders, err)
	}

	c.BookmarkPosition = 0
	if _, err = parseBookmarks(c); err == nil {
		t.Fatal("a URL is not a bookmark folder")
	}

	c.BookmarkPosition = -1
	if _, err = parseBookmarks(c); err == nil {
		t.Fatal("negative folder must fail")
	}
}

func TestSafeName(t *testing.T) {
	for _, name := range []string{"../../track", `Artist: what?/song*`, "..", "CON", "LPT1.txt", strings.Repeat("🎵", 200)} {
		safe := safeName(name)
		if safe == "" || safe == "." || safe == ".." || strings.ContainsAny(safe, `<>:"/\|?*`) || len(safe) > 180 || safe == "CON" || safe == "LPT1.txt" {
			t.Fatalf("unsafe result %q for %q", safe, name)
		}
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("APPDATA", root)
	t.Setenv("HOME", root)
	c := testConfig(t)
	if err := saveConfig(c); err != nil {
		t.Fatal(err)
	}

	c.AudioQuality = "320k"
	if err := saveConfig(c); err != nil {
		t.Fatal("overwriting saved config:", err)
	}

	got, err := loadConfig()
	if err != nil || got != c {
		t.Fatalf("config=%+v, err=%v", got, err)
	}

	dir, _ := configDir()
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !json.Valid(b) {
		t.Fatal("invalid config JSON")
	}

	c.MaxConcurrent = 0
	if err = saveConfig(c); err == nil {
		t.Fatal("zero concurrency should fail")
	}
}

func TestCancelledQueue(t *testing.T) {
	c := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan TrackEvent, 2)
	err := newDownloader(c, ToolSet{}).process(
		ctx,
		[]Track{{Name: "One", URL: "https://youtu.be/a"}, {Name: "Two", URL: "https://youtu.be/b"}},
		func(e TrackEvent) {
			events <- e
		},
	)
	if err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case e := <-events:
			if e.State != "Cancelled" {
				t.Fatal(e)
			}
		case <-time.After(time.Second):
			t.Fatal("missing cancellation event")
		}
	}
}

func TestExistingFilesAreSkipped(t *testing.T) {
	c := testConfig(t)
	if err := os.WriteFile(filepath.Join(c.MusicDirectory, "One.mp3"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}

	var event TrackEvent
	err := newDownloader(c, ToolSet{}).process(
		context.Background(),
		[]Track{{Name: "One", URL: "https://youtu.be/a"}},
		func(e TrackEvent) {
			event = e
		},
	)
	if err != nil || event.State != "Skipped" {
		t.Fatalf("%+v, %v", event, err)
	}
}

func TestFilenameCollisions(t *testing.T) {
	tracks := []Track{
		{Name: "Song", URL: "1"},
		{Name: "song", URL: "2"},
		{Name: "Song (2)", URL: "3"},
		{Name: "Other", URL: "1"},
	}
	files, skip := trackPaths(t.TempDir(), tracks)
	seen := make(map[string]bool)
	for _, p := range files {
		key := strings.ToLower(p)
		if seen[key] {
			t.Fatalf("colliding filename: %s", p)
		}

		seen[key] = true
	}

	if !skip[3] || skip[0] || skip[1] {
		t.Fatalf("incorrect duplicate detection: %v", skip)
	}
}
