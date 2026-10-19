package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

var client = &http.Client{Timeout: 15 * time.Minute}

func request(url string) (*http.Response, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("download requires HTTPS: %s", url)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "music-downloader-build")
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GH_TOKEN"); token != "" && req.URL.Host == "api.github.com" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", url, response.StatusCode)
	}
	return response, nil
}

func download(url, destination, digest string) error {
	fmt.Println("Downloading", filepath.Base(destination))
	response, err := request(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	pending, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(pending.Name())
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(pending, hash), response.Body)
	closeErr := pending.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if digest != "" && digest != "sha256:"+hex.EncodeToString(hash.Sum(nil)) {
		return fmt.Errorf("checksum mismatch: %s", filepath.Base(destination))
	}
	return os.Rename(pending.Name(), destination)
}
