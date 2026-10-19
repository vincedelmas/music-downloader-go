package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const appID = "io.github.vincedelmas.music-downloader"

var version = "dev"

// Settings live outside the installation directory.
type Config struct {
	BookmarkPath     string `json:"bookmark_path"`
	BookmarkPosition int    `json:"bookmark_position"`
	MusicDirectory   string `json:"music_directory"`
	NamePattern      string `json:"name_pattern"`
	FetchMetadata    bool   `json:"fetch_metadata"`
	MaxConcurrent    int    `json:"max_concurrent"`
	AudioQuality     string `json:"audio_quality"`
}

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "music-downloader"), nil
}
func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "MusicDownloader"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "MusicDownloader"), nil
	default:
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "music-downloader"), nil
	}
}
func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	var bookmark string
	switch runtime.GOOS {
	case "windows":
		bookmark = filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Bookmarks")
	case "darwin":
		bookmark = filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Bookmarks")
	default:
		bookmark = filepath.Join(home, ".config", "google-chrome", "Default", "Bookmarks")
	}
	return Config{BookmarkPath: bookmark, MusicDirectory: filepath.Join(home, "Music", "Downloaded Music"), NamePattern: "{artist} - {title}", FetchMetadata: true, MaxConcurrent: 3, AudioQuality: "192k"}
}
func (c Config) Validate() error {
	if c.BookmarkPath == "" || c.MusicDirectory == "" {
		return fmt.Errorf("choose a bookmarks file and a download folder")
	}
	if !filepath.IsAbs(c.BookmarkPath) || !filepath.IsAbs(c.MusicDirectory) {
		return fmt.Errorf("bookmarks and music paths must be absolute")
	}
	if c.BookmarkPosition < 0 {
		return fmt.Errorf("choose a bookmark folder")
	}
	if _, err := compileNamePattern(c.NamePattern); err != nil {
		return err
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 10 {
		return fmt.Errorf("simultaneous downloads must be between 1 and 10")
	}
	switch c.AudioQuality {
	case "128k", "192k", "256k", "320k":
	default:
		return fmt.Errorf("choose a supported audio quality")
	}
	return nil
}
func loadConfig() (Config, error) {
	c := defaultConfig()
	dir, err := configDir()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("could not read settings: %w", err)
	}
	// Migrate positional settings from earlier desktop versions; Spotify fields are ignored.
	var old struct {
		Pattern   *string `json:"name_pattern"`
		Separator string  `json:"music_separator"`
		Title     int     `json:"title_position"`
		Artist    int     `json:"artist_position"`
	}
	if json.Unmarshal(b, &old) == nil && old.Pattern == nil && old.Separator != "" && old.Title != old.Artist && old.Title >= 0 && old.Artist >= 0 && old.Title < 20 && old.Artist < 20 {
		count := max(old.Title, old.Artist) + 1
		parts := make([]string, count)
		for i := range parts {
			parts[i] = "{skip}"
		}
		parts[old.Title] = "{title}"
		parts[old.Artist] = "{artist}"
		c.NamePattern = strings.Join(parts, old.Separator)
	}
	return c, nil
}
func saveConfig(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "config.json"), b, 0600)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
