package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func macPackage(stage, executable, base, version string) error {
	app := filepath.Join("build", "Music Downloader.app")
	if err := os.RemoveAll(app); err != nil {
		return err
	}
	contents := filepath.Join(app, "Contents")
	resources := filepath.Join(contents, "Resources")
	macos := filepath.Join(contents, "MacOS")
	for _, directory := range []string{resources, macos} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
	}
	if err := os.Rename(executable, filepath.Join(macos, "music-downloader")); err != nil {
		return err
	}
	for _, name := range []string{"tools", "licenses"} {
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(resources, name)); err != nil {
			return err
		}
	}
	icons := filepath.Join("build", "AppIcon.iconset")
	if err := os.RemoveAll(icons); err != nil {
		return err
	}
	if err := os.MkdirAll(icons, 0755); err != nil {
		return err
	}
	for _, size := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			suffix := ""
			if scale == 2 {
				suffix = "@2x"
			}
			name := fmt.Sprintf("icon_%dx%d%s.png", size, size, suffix)
			pixels := fmt.Sprint(size * scale)
			if err := run("sips", "-z", pixels, pixels, filepath.Join("assets", "icon.png"), "--out", filepath.Join(icons, name)); err != nil {
				return err
			}
		}
	}
	if err := run("iconutil", "-c", "icns", icons, "-o", filepath.Join(resources, "AppIcon.icns")); err != nil {
		return err
	}
	appVersion := strings.TrimPrefix(version, "v")
	if appVersion == "dev" {
		appVersion = "0.0.0"
	}
	// Version is validated before it reaches this XML document.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>music-downloader</string>
<key>CFBundleIdentifier</key><string>io.github.vincedelmas.music-downloader</string>
<key>CFBundleName</key><string>Music Downloader</string>
<key>CFBundleDisplayName</key><string>Music Downloader</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>CFBundleShortVersionString</key><string>%s</string>
<key>CFBundleVersion</key><string>%s</string>
<key>NSHighResolutionCapable</key><true/>
<key>LSMinimumSystemVersion</key><string>12.0</string>
</dict></plist>
`, appVersion, appVersion)
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0644); err != nil {
		return err
	}
	// Ad-hoc signatures support Apple Silicon without a Developer ID certificate.
	for _, tool := range []string{"yt-dlp", "ffmpeg", "ffprobe", "deno"} {
		if err := run("codesign", "--force", "--sign", "-", filepath.Join(resources, "tools", tool)); err != nil {
			return err
		}
	}
	if err := run("codesign", "--force", "--deep", "--sign", "-", app); err != nil {
		return err
	}
	dmg := filepath.Join("build", "dmg")
	if err := os.RemoveAll(dmg); err != nil {
		return err
	}
	if err := os.MkdirAll(dmg, 0755); err != nil {
		return err
	}
	if err := copyTree(app, filepath.Join(dmg, filepath.Base(app))); err != nil {
		return err
	}
	if err := os.Symlink("/Applications", filepath.Join(dmg, "Applications")); err != nil {
		return err
	}
	return run("hdiutil", "create", "-volname", "Music Downloader", "-srcfolder", dmg, "-ov", "-format", "UDZO", filepath.Join("dist", base+".dmg"))
}
