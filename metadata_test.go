package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Kept separate from the Unix updater test so the metadata tests also compile on Windows.
type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func resetMetadata(t *testing.T) {
	t.Helper()
	metadataCache.Lock()
	metadataCache.items = make(map[string]TrackMetadata)
	metadataCache.Unlock()
	musicBrainzLast = time.Time{}
	t.Cleanup(func() { musicBrainzLast = time.Time{} })
}
func TestMusicBrainzLookupAndCache(t *testing.T) {
	resetMetadata(t)
	d := newDownloader(testConfig(t), ToolSet{})
	calls := 0
	d.client = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "musicbrainz.org" {
			t.Errorf("wrong provider: %s", req.URL.Host)
		}
		if req.Header.Get("User-Agent") == "" || req.Header.Get("Authorization") != "" {
			t.Error("public lookup requires an identifying User-Agent, without authentication")
		}
		if !strings.Contains(req.URL.Query().Get("query"), `artist:"Artist"`) {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"recordings":[{"title":"Song","score":100,"artist-credit":[{"name":"Someone Else"}]},{"title":"Song","score":100,"first-release-date":"2001-03-12","artist-credit":[{"name":"Artist","artist":{"name":"Artist"}}],"releases":[{"id":"056e4f3e-d505-4dad-8ec1-d04f521cbb56","title":"Album","status":"Official","date":"2001"}]}]}`))}, nil
	})}
	m, err := d.metadata(context.Background(), Track{Artist: "Artist", Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Artist != "Artist" || m.Title != "Song" || m.Album != "Album" || m.Year != "2001" || !strings.HasPrefix(m.CoverURL, "https://coverartarchive.org/release/") {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	if _, err = d.metadata(context.Background(), Track{Artist: "Artist", Title: "Song"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cache missed: %d requests", calls)
	}
}
func TestMusicBrainzRejectsWeakMatch(t *testing.T) {
	resetMetadata(t)
	d := newDownloader(testConfig(t), ToolSet{})
	d.client = &http.Client{Transport: metadataTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"recordings":[{"title":"Wrong song","score":100,"artist-credit":[{"name":"Artist"}]},{"title":"Song","score":60,"artist-credit":[{"name":"Artist"}]}]}`))}, nil
	})}
	if _, err := d.metadata(context.Background(), Track{Artist: "Artist", Title: "Song"}); err == nil {
		t.Fatal("an approximate match must not overwrite the song's tags")
	}
}
func TestMetadataRateLimitCancellation(t *testing.T) {
	resetMetadata(t)
	if err := waitForMusicBrainz(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := waitForMusicBrainz(ctx); err != context.DeadlineExceeded {
		t.Fatalf("expected cancel during rate-limit wait, got %v", err)
	}
	// Cancellation must release the gate for the next worker.
	if len(musicBrainzGate) != 0 {
		t.Fatal("cancelled lookup held the rate-limit gate")
	}
}

func TestPreferOriginalAlbumOverCompilation(t *testing.T) {
	var releases []musicBrainzRelease
	data := `[{"title":"Various Artists compilation","status":"Official","date":"1999","release-group":{"primary-type":"Album","secondary-types":["Compilation"]}},{"title":"Artist album reissue","status":"Official","date":"2020","release-group":{"primary-type":"Album"}},{"title":"Artist album","status":"Official","date":"2001","release-group":{"primary-type":"Album"}}]`
	if err := json.Unmarshal([]byte(data), &releases); err != nil {
		t.Fatal(err)
	}
	if index := bestRelease(releases, "Artist"); index != 2 {
		t.Fatalf("chose %d instead of original album", index)
	}
}

func TestStudioAlbumRegressions(t *testing.T) {
	for _, tt := range []struct{ fixture, artist, title, album, year string }{
		{"metallica", "Metallica", "Whiskey in the Jar", "Garage Inc.", "1998"},
		{"jackson", "Michael Jackson", "Human Nature", "Thriller", "1982"},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tt.fixture + "-recordings.json")
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Recordings []musicBrainzRecording `json:"recordings"`
			}
			if err = json.Unmarshal(data, &response); err != nil {
				t.Fatal(err)
			}
			m, err := chooseRecording(response.Recordings, Track{Artist: tt.artist, Title: tt.title})
			if err != nil {
				t.Fatal(err)
			}
			if m.Album != tt.album || m.Year != tt.year {
				t.Fatalf("selected %+v, want %s (%s)", m, tt.album, tt.year)
			}
			if !strings.Contains(m.CoverURL, "/release/") || !strings.HasSuffix(m.CoverURL, "/front-1200") {
				t.Fatalf("expected audio-edition album artwork: %s", m.CoverURL)
			}
			t.Logf("%s — %s: %s (%s), %s", tt.artist, tt.title, m.Album, m.Year, m.CoverURL)
		})
	}
}

func TestCoverSkipsSleevesAndTriesAnotherEdition(t *testing.T) {
	portrait := image.NewRGBA(image.Rect(0, 0, 40, 60))
	square := image.NewRGBA(image.Rect(0, 0, 40, 40))
	encode := func(img image.Image) []byte { var b bytes.Buffer; png.Encode(&b, img); return b.Bytes() }
	portraitPNG, squarePNG := encode(portrait), encode(square)
	d := newDownloader(testConfig(t), ToolSet{})
	var requests []string
	d.client = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Path)
		data := portraitPNG
		if req.URL.Path == "/audio-edition" {
			data = squarePNG
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	path, err := d.metadataCover(context.Background(), TrackMetadata{CoverURL: "https://example.org/cassette", CoverAlternatives: []string{"https://example.org/audio-edition"}, CoverFallbackURL: "https://example.org/unused"}, t.TempDir())
	if err != nil || path == "" {
		t.Fatalf("%q %v", path, err)
	}
	if len(requests) != 2 || requests[1] != "/audio-edition" {
		t.Fatalf("wrong artwork attempts: %v", requests)
	}
	if path, err = d.metadataCover(context.Background(), TrackMetadata{CoverURL: "https://example.org/cassette"}, t.TempDir()); err == nil || path != "" {
		t.Fatal("portrait sleeve should not be accepted as album artwork")
	}
}

// Opt-in end-to-end verification uses known recording fixtures and fetches real cover files.
func TestLiveAlbumCovers(t *testing.T) {
	if os.Getenv("MUSIC_DOWNLOADER_LIVE_COVERS") == "" {
		t.Skip("live artwork verification is opt-in")
	}
	for _, tt := range []struct{ fixture, artist, title string }{{"metallica", "Metallica", "Whiskey in the Jar"}, {"jackson", "Michael Jackson", "Human Nature"}} {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tt.fixture + "-recordings.json")
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Recordings []musicBrainzRecording `json:"recordings"`
			}
			if err = json.Unmarshal(data, &response); err != nil {
				t.Fatal(err)
			}
			m, err := chooseRecording(response.Recordings, Track{Artist: tt.artist, Title: tt.title})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			defer cancel()
			d := newDownloader(testConfig(t), ToolSet{})
			dir := t.TempDir()
			path, err := d.metadataCover(ctx, m, dir)
			if err != nil || path == "" {
				t.Fatalf("cover download: %q %v", path, err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			size, _, err := image.DecodeConfig(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %s, %dx%d", tt.artist, m.Album, size.Width, size.Height)
			if b, err := os.ReadFile(path); err == nil {
				os.WriteFile(filepath.Join(os.TempDir(), "music-"+tt.fixture+"-verified-cover.jpg"), b, 0600)
			}
		})
	}
}
