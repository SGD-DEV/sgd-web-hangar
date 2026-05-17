package php

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/config"
)

type DownloadProgress struct {
	Version     string  `json:"version"`
	Percent     float64 `json:"percent"`
	Downloaded  int64   `json:"downloaded"`
	Total       int64   `json:"total"`
	SpeedMBps   float64 `json:"speed_mbps"`
	Status      string  `json:"status"`
	Error       string  `json:"error,omitempty"`
}

type VersionEntry struct {
	Version  string `json:"version"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type Downloader struct {
	paths      config.Paths
	store      *config.Store
	progressCh chan DownloadProgress
}

func NewDownloader(paths config.Paths, store *config.Store) *Downloader {
	return &Downloader{
		paths:      paths,
		store:      store,
		progressCh: make(chan DownloadProgress, 10),
	}
}

func (d *Downloader) ProgressChan() <-chan DownloadProgress {
	return d.progressCh
}

func (d *Downloader) Download(entry VersionEntry) error {
	d.emitProgress(entry.Version, 0, 0, 0, "downloading", "")

	tmpDir := filepath.Join(d.paths.DataPath(), "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("php downloader: creating tmp dir: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, entry.Filename)
	defer os.Remove(tmpFile)

	if err := d.downloadFile(entry, tmpFile); err != nil {
		d.emitProgress(entry.Version, 0, 0, 0, "error", err.Error())
		return err
	}

	d.emitProgress(entry.Version, 95, 0, 0, "verifying", "")

	if entry.SHA256 != "" {
		if err := d.verifyHash(tmpFile, entry.SHA256); err != nil {
			d.emitProgress(entry.Version, 0, 0, 0, "error", err.Error())
			return err
		}
	}

	d.emitProgress(entry.Version, 97, 0, 0, "extracting", "")

	destDir := d.paths.PHPPath(entry.Version)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("php downloader: creating dest dir: %w", err)
	}

	if err := d.extractZip(tmpFile, destDir); err != nil {
		d.emitProgress(entry.Version, 0, 0, 0, "error", err.Error())
		return fmt.Errorf("php downloader: extracting: %w", err)
	}

	d.emitProgress(entry.Version, 100, 0, 0, "complete", "")
	return nil
}

func (d *Downloader) downloadFile(entry VersionEntry, dest string) error {
	resp, err := http.Get(entry.URL)
	if err != nil {
		return fmt.Errorf("php downloader: fetching %s: %w", entry.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("php downloader: HTTP %d for %s", resp.StatusCode, entry.URL)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("php downloader: creating file: %w", err)
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("php downloader: writing: %w", writeErr)
			}
			downloaded += int64(n)
			if total > 0 {
				pct := float64(downloaded) / float64(total) * 90.0
				d.emitProgress(entry.Version, pct, downloaded, total, "downloading", "")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("php downloader: reading: %w", err)
		}
	}

	return nil
}

func (d *Downloader) verifyHash(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("php downloader: opening for hash: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("php downloader: computing hash: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("php downloader: hash mismatch: expected %s, got %s", expectedHash, actual)
	}

	return nil
}

func (d *Downloader) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fPath := filepath.Join(destDir, f.Name)

		if !strings.HasPrefix(filepath.Clean(fPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fPath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fPath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Downloader) emitProgress(version string, pct float64, downloaded, total int64, status, errMsg string) {
	select {
	case d.progressCh <- DownloadProgress{
		Version:    version,
		Percent:    pct,
		Downloaded: downloaded,
		Total:      total,
		Status:     status,
		Error:      errMsg,
	}:
	default:
	}
}
