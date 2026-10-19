package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func runtimeInstallAvailable() bool { return runtime.GOOS == "linux" }
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected file %s", path)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}
func desktopQuote(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(strings.ReplaceAll(path, "%", "%%")) + `"`
}
func installForUser() error {
	if !runtimeInstallAvailable() {
		return fmt.Errorf("use the installer for this operating system")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".local", "opt")
	if err = os.MkdirAll(root, 0755); err != nil {
		return err
	}
	dest := filepath.Join(root, "music-downloader")
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".music-downloader-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = copyFile(exe, filepath.Join(stage, "music-downloader"), 0755); err != nil {
		return err
	}
	tools, err := resolveTools()
	if err != nil {
		return err
	}
	if err = copyTree(filepath.Dir(tools.YtDlp), filepath.Join(stage, "tools")); err != nil {
		return err
	}
	licenses := filepath.Join(bundleRoot(), "licenses")
	if _, err = os.Stat(licenses); err == nil {
		if err = copyTree(licenses, filepath.Join(stage, "licenses")); err != nil {
			return err
		}
	}
	if err = os.WriteFile(filepath.Join(stage, "icon.png"), appIcon.Content(), 0644); err != nil {
		return err
	}
	backup := dest + ".previous"
	if _, err = os.Stat(backup); err == nil {
		return fmt.Errorf("previous installation backup exists at %s; remove it before reinstalling", backup)
	}
	hadPrevious := false
	if _, err = os.Stat(dest); err == nil {
		if err = os.Rename(dest, backup); err != nil {
			return err
		}
		hadPrevious = true
	}
	if err = os.Rename(stage, dest); err != nil {
		if hadPrevious {
			os.Rename(backup, dest)
		}
		return err
	}
	appsBase := os.Getenv("XDG_DATA_HOME")
	if appsBase == "" {
		appsBase = filepath.Join(home, ".local", "share")
	}
	apps := filepath.Join(appsBase, "applications")
	if err = os.MkdirAll(apps, 0755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=Music Downloader\nComment=Download music from your browser bookmarks\nExec=" + desktopQuote(filepath.Join(dest, "music-downloader")) + "\nIcon=" + filepath.Join(dest, "icon.png") + "\nTerminal=false\nCategories=AudioVideo;Audio;\nStartupWMClass=Music Downloader\n"
	if err = atomicWrite(filepath.Join(apps, appID+".desktop"), []byte(entry), 0644); err != nil {
		return err
	}
	if hadPrevious {
		os.RemoveAll(backup)
	}
	return nil
}
