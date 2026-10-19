package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(output, input)
		closeErr := output.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func archive(source, destination string, windows bool) error {
	pending, err := os.CreateTemp(filepath.Dir(destination), ".package-*")
	if err != nil {
		return err
	}
	defer os.Remove(pending.Name())
	defer pending.Close()
	var writer io.Writer
	var closeArchive func() error
	var addHeader func(string, os.FileInfo) error
	if windows {
		zipped := zip.NewWriter(pending)
		closeArchive = zipped.Close
		addHeader = func(name string, info os.FileInfo) error {
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = name
			header.Method = zip.Deflate
			writer, err = zipped.CreateHeader(header)
			return err
		}
	} else {
		compressed := gzip.NewWriter(pending)
		tarred := tar.NewWriter(compressed)
		closeArchive = func() error {
			tarErr := tarred.Close()
			gzipErr := compressed.Close()
			if tarErr != nil {
				return tarErr
			}
			return gzipErr
		}
		writer = tarred
		addHeader = func(name string, info os.FileInfo) error {
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = name
			return tarred.WriteHeader(header)
		}
	}
	walkErr := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := "music-downloader/" + filepath.ToSlash(relative)
		if err := addHeader(name, info); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		_, err = io.Copy(writer, input)
		return err
	})
	archiveErr := closeArchive()
	fileErr := pending.Close()
	if walkErr != nil {
		return walkErr
	}
	if archiveErr != nil {
		return archiveErr
	}
	if fileErr != nil {
		return fileErr
	}
	return os.Rename(pending.Name(), destination)
}

func extractDeno(directory string) error {
	path := filepath.Join(directory, "deno.zip")
	zipped, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	err = func() error {
		defer zipped.Close()
		for _, entry := range zipped.File {
			// Extract only the expected executable; never use upstream paths as destinations.
			if entry.Name != executableName("deno") {
				continue
			}
			input, err := entry.Open()
			if err != nil {
				return err
			}
			defer input.Close()
			output, err := os.OpenFile(filepath.Join(directory, executableName("deno")), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			_, err = io.Copy(output, input)
			closeErr := output.Close()
			if err != nil {
				return err
			}
			return closeErr
		}
		return fmt.Errorf("Deno archive is missing %s", executableName("deno"))
	}()
	if err != nil {
		return err
	}
	return os.Remove(path)
}
