// Command bundle builds a self-contained package for the current operating system.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type target struct {
	yt     string
	ffmpeg string
	deno   string
}

var targets = map[string]target{
	"linux/amd64":   {"yt-dlp_linux", "linux-x64", "x86_64-unknown-linux-gnu"},
	"windows/amd64": {"yt-dlp.exe", "win32-x64", "x86_64-pc-windows-msvc"},
	"darwin/amd64":  {"yt-dlp_macos", "darwin-x64", "x86_64-apple-darwin"},
	"darwin/arm64":  {"yt-dlp_macos", "darwin-arm64", "aarch64-apple-darwin"},
}

func main() {
	version := flag.String("version", "dev", "Package version (dev or semantic version)")
	toolsOnly := flag.Bool("tools-only", false, "Download tools into ./tools for development")
	flag.Parse()
	if err := bundle(*version, *toolsOnly); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(name string, args ...string) error {
	fmt.Println("+", name, strings.Join(args, " "))
	command := exec.Command(name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func bundle(version string, toolsOnly bool) error {
	if !regexp.MustCompile(`^(dev|v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?)$`).MatchString(version) {
		return fmt.Errorf("version must be dev or a semantic version")
	}
	platform, ok := targets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("unsupported target: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	// Run from the repository root, like go build and go test.
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run this command from the repository root: %w", err)
	}
	if toolsOnly {
		return bundleTools("tools", platform)
	}

	stage := filepath.Join("build", "package")
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(stage, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	executable := filepath.Join(stage, executableName("music-downloader"))
	flags := "-s -w -X main.version=" + version
	if runtime.GOOS == "windows" {
		flags += " -H=windowsgui"
		resource := "icon_windows_amd64.syso"
		if _, err := os.Lstat(resource); !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s before packaging", resource)
		}
		defer os.Remove(resource)
		icon, err := filepath.Abs(filepath.Join("assets", "icon.ico"))
		if err != nil {
			return err
		}
		rc := []byte("1 ICON \"" + filepath.ToSlash(icon) + "\"\n")
		if err := os.WriteFile(filepath.Join("build", "icon.rc"), rc, 0644); err != nil {
			return err
		}
		if err := run("windres", "-i", filepath.Join("build", "icon.rc"), "-o", resource, "-O", "coff"); err != nil {
			return err
		}
	}
	if err := run("go", "build", "-trimpath", "-ldflags", flags, "-o", executable, "."); err != nil {
		return err
	}
	if err := bundleTools(filepath.Join(stage, "tools"), platform); err != nil {
		return err
	}
	if err := notices(filepath.Join(stage, "licenses")); err != nil {
		return err
	}
	for source, destination := range map[string]string{"assets/icon.png": "icon.png", "README.md": "README.md"} {
		if err := copyTree(filepath.FromSlash(source), filepath.Join(stage, destination)); err != nil {
			return err
		}
	}
	base := "music-downloader-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH
	switch runtime.GOOS {
	case "linux":
		installer := filepath.Join(stage, "install.sh")
		if err := copyTree(filepath.Join("scripts", "install.sh"), installer); err != nil {
			return err
		}
		if err := os.Chmod(installer, 0755); err != nil {
			return err
		}
		if err := archive(stage, filepath.Join("dist", base+".tar.gz"), false); err != nil {
			return err
		}
	case "windows":
		if err := archive(stage, filepath.Join("dist", base+".zip"), true); err != nil {
			return err
		}
		compiler, err := exec.LookPath("iscc")
		if err != nil {
			compiler = `C:\Program Files (x86)\Inno Setup 6\ISCC.exe`
		}
		if err := run(compiler, "/DAppVersion="+strings.TrimPrefix(version, "v"), "/DArtifactName="+base+"-setup", filepath.Join("scripts", "windows.iss")); err != nil {
			return err
		}
	case "darwin":
		if err := macPackage(stage, executable, base, version); err != nil {
			return err
		}
	}
	fmt.Println("Packages ready in dist/")
	return nil
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func bundleTools(destination string, platform target) error {
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	versions := make(map[string]string)
	for _, spec := range []struct {
		repo, tag string
		files     map[string]string
	}{
		{"yt-dlp/yt-dlp", "", map[string]string{platform.yt: executableName("yt-dlp")}},
		{"eugeneware/ffmpeg-static", "b6.1.1", map[string]string{
			"ffmpeg-" + platform.ffmpeg:  executableName("ffmpeg"),
			"ffprobe-" + platform.ffmpeg: executableName("ffprobe"),
			platform.ffmpeg + ".LICENSE": "FFmpeg-LICENSE.txt",
			platform.ffmpeg + ".README":  "FFmpeg-SOURCE.txt",
		}},
		{"denoland/deno", "", map[string]string{"deno-" + platform.deno + ".zip": "deno.zip"}},
	} {
		endpoint := "latest"
		if spec.tag != "" {
			endpoint = "tags/" + spec.tag
		}
		response, err := request("https://api.github.com/repos/" + spec.repo + "/releases/" + endpoint)
		if err != nil {
			return err
		}
		var release struct {
			Tag    string  `json:"tag_name"`
			Assets []asset `json:"assets"`
		}
		err = json.NewDecoder(response.Body).Decode(&release)
		response.Body.Close()
		if err != nil {
			return err
		}
		versions[spec.repo] = release.Tag
		for name, filename := range spec.files {
			found := false
			for _, item := range release.Assets {
				if item.Name != name {
					continue
				}
				found = true
				if err := download(item.URL, filepath.Join(destination, filename), item.Digest); err != nil {
					return err
				}
				break
			}
			if !found {
				return fmt.Errorf("missing release asset: %s", name)
			}
		}
	}
	if err := extractDeno(destination); err != nil {
		return err
	}
	for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe", "deno"} {
		path, err := filepath.Abs(filepath.Join(destination, executableName(name)))
		if err != nil {
			return err
		}
		if err := os.Chmod(path, 0755); err != nil {
			return err
		}
		argument := "--version"
		if name == "ffmpeg" || name == "ffprobe" {
			argument = "-version"
		}
		if err := run(path, argument); err != nil {
			return err
		}
	}
	manifest, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "versions.json"), append(manifest, '\n'), 0644)
}

func notices(destination string) error {
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	licenses := filepath.Join("build", "go-licenses")
	if _, err := os.Stat(licenses); err == nil {
		if err := copyTree(licenses, filepath.Join(destination, "go")); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := copyTree("THIRD_PARTY_NOTICES.txt", filepath.Join(destination, "THIRD_PARTY_NOTICES.txt")); err != nil {
		return err
	}
	for _, source := range []struct{ repo, ref, file, name string }{
		{"yt-dlp/yt-dlp", "master", "LICENSE", "yt-dlp"},
		{"denoland/deno", "main", "LICENSE.md", "denoland"},
		{"eugeneware/ffmpeg-static", "master", "LICENSE", "eugeneware"},
	} {
		if err := download("https://raw.githubusercontent.com/"+source.repo+"/"+source.ref+"/"+source.file, filepath.Join(destination, source.name+"-LICENSE.txt"), ""); err != nil {
			return err
		}
	}
	return nil
}
