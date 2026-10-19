package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type BookmarkNode struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	URL      string         `json:"url"`
	Children []BookmarkNode `json:"children"`
}

type Bookmarks struct {
	Roots struct {
		BookmarkBar BookmarkNode `json:"bookmark_bar"`
	} `json:"roots"`
}

type Track struct {
	Name   string
	URL    string
	Title  string
	Artist string
}

type TrackMetadata struct {
	Title             string
	Artist            string
	Album             string
	Year              string
	CoverURL          string
	CoverFallbackURL  string
	CoverAlternatives []string
}

type TrackEvent struct {
	Index  int
	State  string
	Detail string
}

func readBookmarks(path string) (Bookmarks, error) {
	var b Bookmarks
	data, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}

	err = json.Unmarshal(data, &b)
	return b, err
}

func bookmarkFolders(path string) ([]string, error) {
	b, err := readBookmarks(path)
	if err != nil {
		return nil, err
	}

	var names []string
	for i, n := range b.Roots.BookmarkBar.Children {
		if n.Type == "folder" {
			names = append(names, fmt.Sprintf("%d: %s", i, n.Name))
		}
	}

	return names, nil
}

func parseBookmarks(c Config) ([]Track, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	b, err := readBookmarks(c.BookmarkPath)
	if err != nil {
		return nil, err
	}

	nodes := b.Roots.BookmarkBar.Children
	if c.BookmarkPosition >= len(nodes) || nodes[c.BookmarkPosition].Type != "folder" {
		return nil, fmt.Errorf("select a folder on your bookmarks bar")
	}

	pattern, err := compileNamePattern(c.NamePattern)
	if err != nil {
		return nil, err
	}

	var parseErr error
	var tracks []Track
	var visit func(BookmarkNode)
	visit = func(n BookmarkNode) {
		if n.Type == "folder" {
			for _, child := range n.Children {
				visit(child)
			}

			return
		}

		u, err := url.Parse(n.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return
		}

		host := strings.ToLower(u.Hostname())
		if host != "youtu.be" && host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
			return
		}

		artist, title, err := pattern.parse(n.Name)
		if err != nil {
			if parseErr == nil {
				parseErr = err
			}

			return
		}

		t := Track{Name: n.Name, URL: n.URL, Title: title, Artist: artist}
		tracks = append(tracks, t)
	}
	visit(nodes[c.BookmarkPosition])
	if parseErr != nil {
		return nil, parseErr
	}

	return tracks, nil
}

func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}

		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		s = "track"
	}

	for len(s) > 180 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}

	s = strings.TrimRight(s, " .")
	// Prefix Windows device names even when the app runs on another OS.
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	reservedName := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL"
	numberedPort := len(base) == 4 &&
		(strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9'

	if reservedName || numberedPort {
		s = "_" + s
	}

	return s
}

type MusicDownloader struct {
	config Config
	tools  ToolSet
	client *http.Client
}

func newDownloader(c Config, t ToolSet) *MusicDownloader {
	return &MusicDownloader{config: c, tools: t, client: &http.Client{Timeout: 30 * time.Second}}
}

func (d *MusicDownloader) tag(ctx context.Context, path string, m TrackMetadata) error {
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return err
	}

	preserveOriginal := false
	defer func() {
		if !preserveOriginal {
			os.RemoveAll(tmp)
		}
	}()
	args := []string{"-nostdin", "-i", path}
	cover, coverErr := d.metadataCover(ctx, m, tmp)
	if cover != "" {
		args = append(args,
			"-i", cover,
			"-map", "0:a",
			"-map", "1:0",
			"-c:a", "copy",
			"-c:v", "mjpeg",
			"-disposition:v:0", "attached_pic",
		)
	} else {
		args = append(args, "-map", "0:a", "-c:a", "copy")
	}

	out := filepath.Join(tmp, "tagged.mp3")
	args = append(args,
		"-metadata", "title="+m.Title,
		"-metadata", "artist="+m.Artist,
		"-metadata", "album="+m.Album,
		"-metadata", "date="+m.Year,
		"-id3v2_version", "3",
		"-y", out,
	)
	cmd := toolCommand(ctx, d.tools.FFmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("metadata: %w: %s", err, stderr.String())
	}
	// Keep a recoverable original until replacement has succeeded (also works on Windows).
	backup := filepath.Join(tmp, "original.mp3")
	if err = os.Rename(path, backup); err != nil {
		return err
	}

	if err = os.Rename(out, path); err != nil {
		restoreErr := os.Rename(backup, path)
		if restoreErr != nil {
			preserveOriginal = true
			return fmt.Errorf("replace failed: %v; original retained at %s", err, backup)
		}

		return err
	}

	return coverErr
}

func (d *MusicDownloader) process(ctx context.Context, tracks []Track, emit func(TrackEvent)) error {
	return d.processBatch(ctx, tracks, emit, false)
}

func (d *MusicDownloader) refreshDetails(ctx context.Context, tracks []Track, emit func(TrackEvent)) error {
	metadataCache.Lock()
	for _, t := range tracks {
		delete(metadataCache.items, normalizedName(t.Artist)+"/"+normalizedName(t.Title))
	}

	metadataCache.Unlock()
	return d.processBatch(ctx, tracks, emit, true)
}

func (d *MusicDownloader) processBatch(ctx context.Context, tracks []Track, emit func(TrackEvent), refreshOnly bool) error {
	if err := os.MkdirAll(d.config.MusicDirectory, 0755); err != nil {
		return err
	}
	// Duplicate URLs are skipped; identical bookmark names get deterministic suffixes.
	files, skip := trackPaths(d.config.MusicDirectory, tracks)
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for w := 0; w < d.config.MaxConcurrent; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					emit(TrackEvent{i, "Cancelled", ""})
					continue
				}

				if skip[i] {
					emit(TrackEvent{i, "Skipped", "Duplicate link"})
					continue
				}

				_, statErr := os.Stat(files[i])
				if statErr != nil && !os.IsNotExist(statErr) {
					emit(TrackEvent{i, "Failed", statErr.Error()})
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}

				exists := statErr == nil
				if refreshOnly && !exists {
					emit(TrackEvent{i, "Skipped", "Not downloaded yet"})
					continue
				}

				if !refreshOnly && exists {
					emit(TrackEvent{i, "Skipped", "File already exists"})
					continue
				}

				if !refreshOnly {
					emit(TrackEvent{i, "Downloading", "Extracting MP3 audio"})
					err := d.download(ctx, tracks[i], files[i])
					if err != nil {
						state := "Failed"
						if ctx.Err() != nil {
							state = "Cancelled"
						}

						emit(TrackEvent{i, state, err.Error()})
						mu.Lock()
						failed++
						mu.Unlock()
						continue
					}
				}

				warning := ""
				if d.config.FetchMetadata || refreshOnly {
					emit(TrackEvent{i, "Tagging", "Adding track details and artwork"})
					m, err := d.metadata(ctx, tracks[i])
					if err == nil {
						err = d.tag(ctx, files[i], m)
					}

					if err != nil {
						warning = err.Error()
					}
				}

				if warning != "" {
					if refreshOnly {
						emit(TrackEvent{i, "Failed", warning})
						mu.Lock()
						failed++
						mu.Unlock()
					} else {
						emit(TrackEvent{i, "Saved with warning", warning})
					}
				} else {
					state := "Completed"
					if refreshOnly {
						state = "Updated"
					}

					emit(TrackEvent{i, state, files[i]})
				}
			}
		}()
	}

	for i := range tracks {
		jobs <- i
	}

	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if failed > 0 {
		return fmt.Errorf("%d track(s) failed; see the track details", failed)
	}

	return nil
}

func (d *MusicDownloader) download(ctx context.Context, t Track, out string) error {
	tmp, err := os.MkdirTemp(d.config.MusicDirectory, ".download-*")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-overwrites",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", d.config.AudioQuality,
		"--ffmpeg-location", filepath.Dir(d.tools.FFmpeg),
		"--js-runtimes", "deno:" + d.tools.Deno,
		"--output", filepath.Join(tmp, "audio.%(ext)s"),
		"--quiet",
		"--no-warnings",
		"--", t.URL,
	}

	cmd := toolCommand(ctx, d.tools.YtDlp, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return os.Rename(filepath.Join(tmp, "audio.mp3"), out)
}

// Reserve the entire final filename, including generated suffixes, across platforms.
func trackPaths(dir string, tracks []Track) ([]string, []bool) {
	used := make(map[string]bool)
	seen := make(map[string]bool)
	files := make([]string, len(tracks))
	skip := make([]bool, len(tracks))
	for i, t := range tracks {
		skip[i] = seen[t.URL]
		seen[t.URL] = true
		base := safeName(t.Name)
		name := base
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s (%d)", base, n)
		}

		used[strings.ToLower(name)] = true
		files[i] = filepath.Join(dir, name+".mp3")
	}

	return files, skip
}

func (d *MusicDownloader) fetchCover(ctx context.Context, coverURL, dir string) (string, error) {
	if coverURL == "" {
		return "", nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", coverURL, nil)
	if err != nil {
		return "", err
	}

	res, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("artwork unavailable: %w", err)
	}

	defer res.Body.Close()
	// Missing artwork is normal for community-maintained releases; still write the song tags.
	if res.StatusCode == http.StatusNotFound {
		return "", nil
	}

	if res.StatusCode != 200 {
		return "", fmt.Errorf("artwork unavailable (%s)", res.Status)
	}

	path := filepath.Join(dir, "cover.jpg")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}

	written, err := io.Copy(f, io.LimitReader(res.Body, 15<<20+1))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}

	if closeErr != nil {
		return "", closeErr
	}

	if written > 15<<20 {
		return "", fmt.Errorf("album artwork is too large")
	}

	source, err := os.Open(path)
	if err != nil {
		return "", err
	}

	size, _, err := image.DecodeConfig(source)
	source.Close()
	if err != nil {
		return "", fmt.Errorf("could not read album artwork: %w", err)
	}

	ratio := float64(size.Width) / float64(max(1, size.Height))
	if ratio < 0.8 || ratio > 1.25 {
		return "", fmt.Errorf("artwork is a sleeve or booklet rather than an album cover")
	}

	return path, nil
}

func (d *MusicDownloader) metadataCover(ctx context.Context, m TrackMetadata, dir string) (string, error) {
	urls := append([]string{m.CoverURL}, m.CoverAlternatives...)
	urls = append(urls, m.CoverFallbackURL)
	seen := make(map[string]bool)
	var lastErr error
	for _, u := range urls {
		if u == "" || seen[u] {
			continue
		}

		seen[u] = true
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		path, err := d.fetchCover(ctx, u, dir)
		if path != "" {
			return path, nil
		}

		if err != nil {
			lastErr = err
		}
	}

	return "", lastErr
}
