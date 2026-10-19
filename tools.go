package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseRepo = "vincedelmas/music-downloader-go"
const ffmpegRepo = "eugeneware/ffmpeg-static"

type ToolSet struct{ YtDlp, FFmpeg, FFprobe, Deno string }
type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type githubRelease struct {
	Tag    string         `json:"tag_name"`
	URL    string         `json:"html_url"`
	Assets []releaseAsset `json:"assets"`
}

var downloadClient = &http.Client{Timeout: 15 * time.Minute}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
func bundleRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
func toolSet(dir string) ToolSet {
	return ToolSet{filepath.Join(dir, executableName("yt-dlp")), filepath.Join(dir, executableName("ffmpeg")), filepath.Join(dir, executableName("ffprobe")), filepath.Join(dir, executableName("deno"))}
}
func (t ToolSet) validate() error {
	for _, p := range []string{t.YtDlp, t.FFmpeg, t.FFprobe, t.Deno} {
		s, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("missing bundled tool %s", filepath.Base(p))
		}
		if s.IsDir() {
			return fmt.Errorf("%s is not an executable", p)
		}
	}
	return nil
}
func resolveTools() (ToolSet, error) {
	data, err := dataDir()
	if err != nil {
		return ToolSet{}, err
	}
	var active struct {
		Directory string `json:"directory"`
	}
	if b, err := os.ReadFile(filepath.Join(data, "tools-current.json")); err == nil && json.Unmarshal(b, &active) == nil && strings.HasPrefix(active.Directory, "set-") && filepath.Base(active.Directory) == active.Directory {
		t := toolSet(filepath.Join(data, "tools", active.Directory))
		if t.validate() == nil {
			return t, nil
		}
	}
	for _, dir := range []string{filepath.Join(bundleRoot(), "tools"), filepath.Join(bundleRoot(), "..", "Resources", "tools")} {
		t := toolSet(dir)
		if t.validate() == nil {
			return t, nil
		}
	}
	return ToolSet{}, fmt.Errorf("download tools are missing. Use Updates → Install / update tools, or install a complete release package")
}
func latestRelease(ctx context.Context, repo string) (githubRelease, error) {
	var r githubRelease
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "music-downloader/"+version)
	res, err := downloadClient.Do(req)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return r, fmt.Errorf("GitHub returned %s for %s", res.Status, repo)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&r)
	return r, err
}
func (r githubRelease) asset(name string) (releaseAsset, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("release %s has no %s download", r.Tag, name)
}
func downloadFile(ctx context.Context, a releaseAsset, path string) error {
	u, err := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	if err != nil {
		return err
	}
	if u.URL.Scheme != "https" {
		return fmt.Errorf("download must use HTTPS")
	}
	res, err := downloadClient.Do(u)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", a.Name, res.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 1<<30))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if a.Digest != "" && a.Digest != "sha256:"+hex.EncodeToString(h.Sum(nil)) {
		return fmt.Errorf("checksum mismatch for %s", a.Name)
	}
	return os.Rename(f.Name(), path)
}
func toolAssets(goos, arch string) (string, string, string, error) {
	var yt, platform, triple string
	switch goos + "/" + arch {
	case "linux/amd64":
		yt = "yt-dlp_linux"
		platform = "linux-x64"
		triple = "x86_64-unknown-linux-gnu"
	case "windows/amd64":
		yt = "yt-dlp.exe"
		platform = "win32-x64"
		triple = "x86_64-pc-windows-msvc"
	case "darwin/amd64":
		yt = "yt-dlp_macos"
		platform = "darwin-x64"
		triple = "x86_64-apple-darwin"
	case "darwin/arm64":
		yt = "yt-dlp_macos"
		platform = "darwin-arm64"
		triple = "aarch64-apple-darwin"
	default:
		return "", "", "", fmt.Errorf("no bundled tools for %s/%s", goos, arch)
	}
	return yt, platform, "deno-" + triple + ".zip", nil
}
func updateTools(ctx context.Context, log func(string)) error {
	yt, platform, deno, err := toolAssets(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	data, err := dataDir()
	if err != nil {
		return err
	}
	root := filepath.Join(data, "tools")
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, "set-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(stage)
		}
	}()
	manifest := make(map[string]string)
	for _, spec := range []struct {
		repo  string
		names map[string]string
	}{
		{"yt-dlp/yt-dlp", map[string]string{yt: executableName("yt-dlp")}},
		{ffmpegRepo, map[string]string{"ffmpeg-" + platform: executableName("ffmpeg"), "ffprobe-" + platform: executableName("ffprobe"), platform + ".LICENSE": "FFmpeg-LICENSE.txt", platform + ".README": "FFmpeg-SOURCE.txt"}},
		{"denoland/deno", map[string]string{deno: "deno.zip"}},
	} {
		log("Checking " + spec.repo + "…")
		r, err := latestRelease(ctx, spec.repo)
		if err != nil {
			return err
		}
		manifest[spec.repo] = r.Tag
		for name, target := range spec.names {
			log("Downloading " + name + "…")
			a, err := r.asset(name)
			if err != nil {
				return err
			}
			if err = downloadFile(ctx, a, filepath.Join(stage, target)); err != nil {
				return err
			}
		}
	}
	zr, err := zip.OpenReader(filepath.Join(stage, "deno.zip"))
	if err != nil {
		return err
	}
	found := false
	for _, f := range zr.File {
		if f.Name != executableName("deno") {
			continue
		}
		src, err := f.Open()
		if err != nil {
			zr.Close()
			return err
		}
		dst, err := os.Create(filepath.Join(stage, executableName("deno")))
		if err != nil {
			src.Close()
			zr.Close()
			return err
		}
		_, err = io.Copy(dst, io.LimitReader(src, 512<<20))
		closeErr := dst.Close()
		src.Close()
		if err != nil {
			zr.Close()
			return err
		}
		if closeErr != nil {
			zr.Close()
			return closeErr
		}
		found = true
	}
	zr.Close()
	if !found {
		return fmt.Errorf("Deno archive did not contain the executable")
	}
	os.Remove(filepath.Join(stage, "deno.zip"))
	t := toolSet(stage)
	for _, p := range []string{t.YtDlp, t.FFmpeg, t.FFprobe, t.Deno} {
		if err = os.Chmod(p, 0755); err != nil {
			return err
		}
		log("Checking " + filepath.Base(p) + "…")
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		flag := "-version"
		if strings.Contains(filepath.Base(p), "yt-dlp") || strings.Contains(filepath.Base(p), "deno") {
			flag = "--version"
		}
		output, runErr := toolCommand(checkCtx, p, flag).CombinedOutput()
		cancel()
		if runErr != nil {
			return fmt.Errorf("%s cannot run: %w: %s", filepath.Base(p), runErr, output)
		}
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.WriteFile(filepath.Join(stage, "versions.json"), b, 0600); err != nil {
		return err
	}
	b, _ = json.Marshal(map[string]string{"directory": filepath.Base(stage)})
	if err = atomicWrite(filepath.Join(data, "tools-current.json"), b, 0600); err != nil {
		return err
	}
	keep = true
	log("All tools updated. Ready to download.")
	return nil
}
func appAssetName(tag, goos, arch string) string {
	base := "music-downloader-" + tag + "-" + goos + "-" + arch
	switch goos {
	case "windows":
		return base + "-setup.exe"
	case "darwin":
		return base + ".dmg"
	default:
		return base + ".tar.gz"
	}
}
func downloadAppUpdate(ctx context.Context, r githubRelease) (string, error) {
	a, err := r.asset(appAssetName(r.Tag, runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return "", err
	}
	if filepath.Base(a.Name) != a.Name || strings.ContainsAny(a.Name, `/\\`) {
		return "", fmt.Errorf("invalid release asset name")
	}
	sums, err := r.asset("checksums.txt")
	if err != nil {
		return "", err
	}
	data, err := dataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(data, "updates")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".pending-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err = downloadFile(ctx, sums, filepath.Join(tmp, "checksums.txt")); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(tmp, "checksums.txt"))
	if err != nil {
		return "", err
	}
	digest := ""
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == a.Name {
			digest = fields[0]
			break
		}
	}
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("release has no valid checksum for %s", a.Name)
	}
	a.Digest = "sha256:" + digest
	target := filepath.Join(tmp, a.Name)
	if err = downloadFile(ctx, a, target); err != nil {
		return "", err
	}
	out := filepath.Join(dir, a.Name)
	if err = os.Rename(target, out); err != nil {
		return "", err
	}
	return out, nil
}
