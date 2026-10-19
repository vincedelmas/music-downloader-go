package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDownloadChecksum(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new executable")
	}))
	defer server.Close()
	original := client
	client = server.Client()
	defer func() { client = original }()
	destination := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(destination, []byte("working executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := download(server.URL, destination, "sha256:bad"); err == nil {
		t.Fatal("accepted incorrect checksum")
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "working executable" {
		t.Fatalf("previous executable changed: %q, %v", contents, err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("new executable")))
	if err := download(server.URL, destination, digest); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(destination)
	if err != nil || string(contents) != "new executable" {
		t.Fatalf("verified download missing: %q, %v", contents, err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary downloads leaked: %v, %v", entries, err)
	}
}

func TestArchivesPreserveLayoutAndExecutable(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tools", "ffmpeg"), []byte("tool contents"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, windows := range []bool{false, true} {
		t.Run(fmt.Sprint("zip=", windows), func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "package")
			if err := archive(source, destination, windows); err != nil {
				t.Fatal(err)
			}
			var reader io.ReadCloser
			var mode os.FileMode
			var name string
			if windows {
				zipped, err := zip.OpenReader(destination)
				if err != nil {
					t.Fatal(err)
				}
				defer zipped.Close()
				if len(zipped.File) != 1 {
					t.Fatalf("unexpected entries: %v", zipped.File)
				}
				entry := zipped.File[0]
				reader, err = entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				mode, name = entry.Mode(), entry.Name
			} else {
				file, err := os.Open(destination)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				compressed, err := gzip.NewReader(file)
				if err != nil {
					t.Fatal(err)
				}
				defer compressed.Close()
				tarred := tar.NewReader(compressed)
				header, err := tarred.Next()
				if err != nil {
					t.Fatal(err)
				}
				reader = io.NopCloser(tarred)
				mode, name = header.FileInfo().Mode(), header.Name
			}
			defer reader.Close()
			contents, err := io.ReadAll(reader)
			if err != nil || string(contents) != "tool contents" {
				t.Fatalf("invalid archived contents: %q, %v", contents, err)
			}
			if name != "music-downloader/tools/ffmpeg" {
				t.Fatalf("invalid archive path: %s", name)
			}
			if runtime.GOOS != "windows" && mode.Perm() != 0755 {
				t.Fatalf("lost executable permissions: %v", mode)
			}
		})
	}
}

func TestExtractDenoIgnoresOtherPaths(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "deno.zip")
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipped := zip.NewWriter(output)
	for _, name := range []string{"../outside", executableName("deno")} {
		entry, err := zipped.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("executable")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractDeno(directory); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != executableName("deno") {
		t.Fatalf("unexpected extracted files: %v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "..", "outside")); !os.IsNotExist(err) {
		t.Fatalf("unsafe path extracted: %v", err)
	}
}

func TestTargetsUseStandaloneYtDlp(t *testing.T) {
	if len(targets) != 4 {
		t.Fatalf("unexpected targets: %v", targets)
	}
	for name, target := range targets {
		if target.yt == "" || target.yt == "yt-dlp" || target.ffmpeg == "" || target.deno == "" {
			t.Fatalf("%s needs standalone tools: %+v", name, target)
		}
	}
}
