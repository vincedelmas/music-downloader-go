package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamePatterns(t *testing.T) {
	for _, tt := range []struct{ pattern, name, artist, title string }{
		{"{artist} - {title}", "Daft Punk - One More Time", "Daft Punk", "One More Time"},
		{"{artist} {-} {title}", "Daft Punk - One More Time", "Daft Punk", "One More Time"},
		{"{title} | {artist}", "One More Time | Daft Punk", "Daft Punk", "One More Time"},
		{"{artist} :: {title}", "Artist :: A title :: with punctuation", "Artist", "A title :: with punctuation"},
		{"{skip}_{title}_{artist}", "2001_One More Time_Daft Punk", "Daft Punk", "One More Time"},
	} {
		t.Run(tt.pattern, func(t *testing.T) {
			p, err := compileNamePattern(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			a, title, err := p.parse(tt.name)
			if err != nil || a != tt.artist || title != tt.title {
				t.Fatalf("%s / %s / %v", a, title, err)
			}
		})
	}

	for _, pattern := range []string{
		"{title}",
		"{artist}{title}",
		"{artist} - {title",
		"{{artist}} - {title}",
		"{artist} - {title} - {artist}",
	} {
		if _, err := compileNamePattern(pattern); err == nil {
			t.Errorf("accepted invalid pattern %s", pattern)
		}
	}

	p, _ := compileNamePattern("{artist} - {title}")
	if _, _, err := p.parse("No separator"); err == nil {
		t.Fatal("unmatched name accepted")
	}
}

func TestMigrateLegacySettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("APPDATA", root)
	dir, _ := configDir()
	os.MkdirAll(dir, 0700)
	os.WriteFile(
		filepath.Join(dir, "config.json"),
		[]byte(`{"music_separator":" | ","title_position":0,"artist_position":1,"spotify_client_secret":"old-secret"}`),
		0600,
	)
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}

	if c.NamePattern != "{title} | {artist}" || !c.FetchMetadata {
		t.Fatalf("migration: %+v", c)
	}

	if err = saveConfig(c); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if strings.Contains(string(data), "spotify") {
		t.Fatal("old credentials retained after saving")
	}
}
