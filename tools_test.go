package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadPreservesExistingOnFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}

		fmt.Fprint(w, "new tool")
	}))
	defer server.Close()
	original := downloadClient
	downloadClient = server.Client()
	defer func() {
		downloadClient = original
	}()
	dest := filepath.Join(t.TempDir(), "tool")
	os.WriteFile(dest, []byte("working tool"), 0600)
	for _, asset := range []releaseAsset{
		{Name: "missing", URL: server.URL + "/missing"},
		{Name: "bad digest", URL: server.URL + "/tool", Digest: "sha256:bad"},
	} {
		if err := downloadFile(context.Background(), asset, dest); err == nil {
			t.Fatal("expected download failure")
		}

		b, _ := os.ReadFile(dest)
		if string(b) != "working tool" {
			t.Fatalf("previous tool replaced with %q", b)
		}
	}

	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("new tool")))
	if err := downloadFile(context.Background(), releaseAsset{Name: "tool", URL: server.URL + "/tool", Digest: digest}, dest); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(dest)
	if string(b) != "new tool" {
		t.Fatal("verified file not installed")
	}

	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v", entries)
	}
}

func TestSupportedPlatforms(t *testing.T) {
	for _, p := range [][2]string{{"linux", "amd64"}, {"windows", "amd64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		a, b, c, err := toolAssets(p[0], p[1])
		if err != nil || a == "" || b == "" || c == "" {
			t.Fatalf("%v: %s %s %s %v", p, a, b, c, err)
		}
	}

	if _, _, _, err := toolAssets("plan9", "amd64"); err == nil {
		t.Fatal("unsupported platform accepted")
	}

	if got := appAssetName("v1.2.3", "windows", "amd64"); got != "music-downloader-v1.2.3-windows-amd64-setup.exe" {
		t.Fatal(got)
	}
}
