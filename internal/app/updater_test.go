//go:build !windows

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestToolUpdateIsTransactional(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", root)
	yt, platform, deno, err := toolAssets(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}

	script := []byte("#!/bin/sh\nexit 0\n")
	var zipBody bytes.Buffer
	zw := zip.NewWriter(&zipBody)
	entry, _ := zw.Create("deno")
	entry.Write(script)
	zw.Close()
	var server *httptest.Server
	fail := true
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			name := strings.TrimPrefix(r.URL.Path, "/assets/")
			if fail && name == "ffprobe-"+platform {
				http.Error(w, "broken upstream", 500)
				return
			}

			if name == deno {
				w.Write(zipBody.Bytes())
			} else if strings.HasSuffix(name, ".LICENSE") || strings.HasSuffix(name, ".README") {
				fmt.Fprint(w, "license / source information")
			} else {
				w.Write(script)
			}

			return
		}

		var names []string
		switch {
		case strings.Contains(r.URL.Path, "yt-dlp/yt-dlp"):
			names = []string{yt}
		case strings.Contains(r.URL.Path, ffmpegRepo):
			names = []string{"ffmpeg-" + platform, "ffprobe-" + platform, platform + ".LICENSE", platform + ".README"}
		case strings.Contains(r.URL.Path, "denoland/deno"):
			names = []string{deno}
		default:
			http.NotFound(w, r)
			return
		}

		rel := githubRelease{Tag: "test"}
		for _, n := range names {
			rel.Assets = append(rel.Assets, releaseAsset{Name: n, URL: server.URL + "/assets/" + n})
		}

		json.NewEncoder(w).Encode(rel)
	}))
	defer server.Close()
	old := downloadClient
	client := server.Client()
	transport := client.Transport
	client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			r = r.Clone(r.Context())
			u := *r.URL
			u.Scheme = "https"
			u.Host = strings.TrimPrefix(server.URL, "https://")
			r.URL = &u
		}

		return transport.RoundTrip(r)
	})
	downloadClient = client
	defer func() {
		downloadClient = old
	}()
	data, _ := dataDir()
	if err = os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}

	pointer := filepath.Join(data, "tools-current.json")
	previous := []byte(`{"directory":"set-previous"}`)
	if err = os.WriteFile(pointer, previous, 0600); err != nil {
		t.Fatal(err)
	}

	if err = updateTools(context.Background(), func(string) {}); err == nil {
		t.Fatal("broken upstream should fail")
	}

	after, _ := os.ReadFile(pointer)
	if string(after) != string(previous) {
		t.Fatal("failed update changed active tools")
	}

	entries, _ := os.ReadDir(filepath.Join(data, "tools"))
	if len(entries) != 0 {
		t.Fatal("failed update leaked partial tools")
	}

	fail = false
	if err = updateTools(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}

	tools, err := resolveTools()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(tools.YtDlp, filepath.Join(data, "tools")) {
		t.Fatalf("new tools not activated: %+v", tools)
	}

	if _, err = os.Stat(filepath.Join(filepath.Dir(tools.YtDlp), "versions.json")); err != nil {
		t.Fatal("missing version manifest:", err)
	}

	for i := 0; i < 3; i++ {
		if err = updateTools(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err = os.ReadDir(filepath.Join(data, "tools"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("expected three completed tool sets after four updates: %v, %v", entries, err)
	}
	previous, err = os.ReadFile(pointer)
	if err != nil {
		t.Fatal(err)
	}
	fail = true
	if err = updateTools(context.Background(), func(string) {}); err == nil {
		t.Fatal("broken update should fail without pruning tool history")
	}
	after, err = os.ReadFile(pointer)
	if err != nil || string(after) != string(previous) {
		t.Fatal("failed update changed the active tool set")
	}
	entries, err = os.ReadDir(filepath.Join(data, "tools"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("failed update changed tool history: %v, %v", entries, err)
	}
}
