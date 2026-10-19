//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFFmpegTaggingPreservesAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("FFprobe not installed")
	}

	c := testConfig(t)
	path := filepath.Join(c.MusicDirectory, "track.mp3")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, out)
	}

	d := newDownloader(c, ToolSet{FFmpeg: ffmpeg})
	if err := d.tag(ctx, path, TrackMetadata{Title: "Example", Artist: "Artist", Album: "Album", Year: "2026"}); err != nil {
		t.Fatal(err)
	}

	out, err := exec.CommandContext(
		ctx,
		ffprobe,
		"-v",
		"quiet",
		"-show_entries",
		"format_tags=title,artist,album,date",
		"-of",
		"json",
		path,
	).Output()
	if err != nil {
		t.Fatal(err)
	}

	var info struct {
		Format struct{ Tags map[string]string }
	}
	if err = json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}

	if info.Format.Tags["title"] != "Example" || info.Format.Tags["artist"] != "Artist" {
		t.Fatalf("metadata missing: %s", out)
	}

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if err := d.tag(ctx, path, TrackMetadata{Title: "No artwork", Artist: "Artist", CoverURL: server.URL + "/front-500"}); err != nil {
		t.Fatal("missing artwork blocked tags:", err)
	}

	out, err = exec.CommandContext(ctx, ffprobe, "-v", "quiet", "-show_entries", "format_tags=title", "-of", "json", path).Output()
	if err != nil {
		t.Fatal(err)
	}

	if err = json.Unmarshal(out, &info); err != nil || info.Format.Tags["title"] != "No artwork" {
		t.Fatalf("tags not written without artwork: %s (%v)", out, err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	d.tools.FFmpeg = "/nonexistent/ffmpeg"
	if err = d.tag(ctx, path, TrackMetadata{Title: "Changed"}); err == nil {
		t.Fatal("expected FFmpeg failure")
	}

	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed tagging damaged the original")
	}

	entries, err := os.ReadDir(c.MusicDirectory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestCancellationKillsChildProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := toolCommand(ctx, "sh", "-c", "sleep 30 & wait").CombinedOutput()
	if err == nil {
		t.Fatal("expected cancellation")
	}

	if time.Since(start) > 3*time.Second {
		t.Fatalf("child process kept command alive: %s", out)
	}
}

func TestRefreshReplacesCoverWithoutChangingAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}

	resetMetadata(t)
	c := testConfig(t)
	c.MaxConcurrent = 1
	track := Track{Name: "Artist - Song", Artist: "Artist", Title: "Song", URL: "https://youtu.be/example"}
	path := filepath.Join(c.MusicDirectory, track.Name+".mp3")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}

	picture := func(c color.RGBA) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				img.SetRGBA(x, y, c)
			}
		}

		var b bytes.Buffer
		jpeg.Encode(&b, img, nil)
		return b.Bytes()
	}
	oldArt := picture(color.RGBA{R: 255, A: 255})
	newArt := picture(color.RGBA{G: 255, A: 255})
	d := newDownloader(c, ToolSet{FFmpeg: ffmpeg, YtDlp: "/must-not-download-audio"})
	d.client = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
		data := oldArt
		if req.URL.Host == "musicbrainz.org" {
			data = []byte(`{"recordings":[{"title":"Song","score":100,"artist-credit":[{"name":"Artist"}],"releases":[{"title":"Correct Album","status":"Official","id":"056e4f3e-d505-4dad-8ec1-d04f521cbb56","release-group":{"primary-type":"Album"}}]}]}`)
		} else if req.URL.Host == "coverartarchive.org" {
			data = newArt
		}

		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	if err = d.tag(
		ctx,
		path,
		TrackMetadata{
			Title:    "Song",
			Artist:   "Artist",
			Album:    "Wrong concert",
			CoverURL: "https://example.org/old.jpg",
		},
	); err != nil {
		t.Fatal(err)
	}

	audioDigest := func() string {
		out, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", path, "-map", "0:a", "-c", "copy", "-f", "md5", "-").Output()
		if err != nil {
			t.Fatal(err)
		}

		return string(out)
	}
	before := audioDigest()
	var states []TrackEvent
	err = d.refreshDetails(
		ctx,
		[]Track{track, {Name: "Missing", Title: "Missing", Artist: "Artist", URL: "https://youtu.be/missing"}},
		func(e TrackEvent) {
			states = append(states, e)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if after := audioDigest(); after != before {
		t.Fatal("refresh changed the encoded audio")
	}

	out, err := exec.CommandContext(
		ctx,
		ffmpeg,
		"-v",
		"error",
		"-i",
		path,
		"-map",
		"0:v",
		"-frames:v",
		"1",
		"-f",
		"image2pipe",
		"-vcodec",
		"png",
		"-",
	).Output()
	if err != nil {
		t.Fatal(err)
	}

	art, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}

	r, g, _, _ := art.At(16, 16).RGBA()
	if g <= r {
		t.Fatal("old red cover was not replaced by new green cover")
	}

	updated, skipped := false, false
	for _, e := range states {
		if e.Index == 0 && e.State == "Updated" {
			updated = true
		}

		if e.Index == 1 && e.State == "Skipped" {
			skipped = true
		}
	}

	if !updated || !skipped {
		t.Fatalf("unexpected refresh states: %+v", states)
	}

	if _, err = os.Stat(filepath.Join(c.MusicDirectory, "Missing.mp3")); !os.IsNotExist(err) {
		t.Fatal("refresh created a new audio file")
	}
}
