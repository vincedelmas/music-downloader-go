package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneToolSets(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		directory := filepath.Join(root, fmt.Sprintf("set-%d", i))
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe", "deno"} {
			if err := os.WriteFile(filepath.Join(directory, executableName(name)), []byte("tool"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		manifest := filepath.Join(directory, "versions.json")
		if err := os.WriteFile(manifest, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		completed := time.Unix(int64(i), 0)
		if err := os.Chtimes(manifest, completed, completed); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"unrelated", "set-incomplete"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The active set is deliberately the oldest: its safety does not depend on age.
	if err := pruneToolSets(root, "set-0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"set-0", "set-3", "set-4", "unrelated", "set-incomplete"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("removed protected directory %s: %v", name, err)
		}
	}
	for _, name := range []string{"set-1", "set-2"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("old set %s remains: %v", name, err)
		}
	}
	if err := pruneToolSets(root, "set-0"); err != nil {
		t.Fatalf("repeated cleanup failed: %v", err)
	}
}
