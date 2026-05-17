package downloader

import (
	"archive/zip"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Progress struct {
	Name       string  `json:"name"`
	Percent    float64 `json:"percent"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	SpeedMBps  float64 `json:"speed_mbps"`
	ETA        int     `json:"eta_seconds"`
	Status     string  `json:"status"`
	Error      string  `json:"error,omitempty"`
}

type Request struct {
	Name      string
	URL       string
	TargetDir string
	SHA256    string
	Filename  string
}

type Downloader struct {
	progressCh chan Progress
}

func New() *Downloader {
	return &Downloader{
		progressCh: make(chan Progress, 10),
	}
}

func (d *Downloader) ProgressChan() <-chan Progress {
	return d.progressCh
}

func (d *Downloader) Download(req Request) error {
	d.emit(req.Name, 0, 0, 0, 0, 0, "downloading", "")

	tmpDir := filepath.Join(os.TempDir(), "devour-downloads")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("downloader: creating tmp dir: %w", err)
	}

	filename := req.Filename
	if filename == "" {
		filename = safeFilenameFromURL(req.URL)
	}
	tmpFile := filepath.Join(tmpDir, filename)
	defer os.Remove(tmpFile)

	// Download file; may update tmpFile path based on Content-Disposition header
	actualFile, err := d.downloadFile(req.Name, req.URL, tmpFile)
	if err != nil {
		d.emit(req.Name, 0, 0, 0, 0, 0, "error", err.Error())
		return err
	}
	if actualFile != "" {
		tmpFile = actualFile
		filename = filepath.Base(actualFile)
		defer os.Remove(actualFile)
	}

	if req.SHA256 != "" {
		d.emit(req.Name, 92, 0, 0, 0, 0, "verifying", "")
		if err := VerifySHA256(tmpFile, req.SHA256); err != nil {
			d.emit(req.Name, 0, 0, 0, 0, 0, "error", err.Error())
			return err
		}
	}

	d.emit(req.Name, 95, 0, 0, 0, 0, "extracting", "")

	if err := os.MkdirAll(req.TargetDir, 0755); err != nil {
		return fmt.Errorf("downloader: creating target dir: %w", err)
	}

	if strings.HasSuffix(strings.ToLower(tmpFile), ".zip") {
		if err := ExtractZip(tmpFile, req.TargetDir); err != nil {
			d.emit(req.Name, 0, 0, 0, 0, 0, "error", err.Error())
			return err
		}
	} else {
		destFile := filepath.Join(req.TargetDir, filename)
		if err := copyFile(tmpFile, destFile); err != nil {
			d.emit(req.Name, 0, 0, 0, 0, 0, "error", err.Error())
			return err
		}
	}

	d.emit(req.Name, 100, 0, 0, 0, 0, "complete", "")
	return nil
}

// safeFilenameFromURL extracts a safe filename from a URL, stripping query params.
// If the path component has no useful filename (e.g. getfile.jsp), generates one from the URL.
func safeFilenameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		// Fallback: split on / and strip query params
		parts := strings.Split(rawURL, "/")
		name := parts[len(parts)-1]
		if idx := strings.Index(name, "?"); idx >= 0 {
			name = name[:idx]
		}
		if name == "" {
			name = "download.zip"
		}
		return name
	}

	name := filepath.Base(parsed.Path)
	// If the filename looks like a server script (e.g. getfile.jsp, download.php),
	// generate a deterministic name from the URL
	ext := strings.ToLower(filepath.Ext(name))
	scriptExts := map[string]bool{".jsp": true, ".php": true, ".asp": true, ".aspx": true, ".cgi": true}
	if name == "" || name == "." || scriptExts[ext] {
		// Build a safe name from query params or use a hash
		query := parsed.Query()
		if fid := query.Get("fileid"); fid != "" {
			name = "download-" + fid + ".zip"
		} else {
			name = "download.zip"
		}
	}
	// Sanitize any remaining invalid characters for Windows
	for _, ch := range []string{"?", "&", "=", "#", "%", "*", "|", "<", ">"} {
		name = strings.ReplaceAll(name, ch, "_")
	}
	return name
}

// downloadFile downloads a URL to dest with automatic retry on transient
// failures. windows.php.net, dev.mysql.com, sourceforge etc. routinely
// return 5xx / time out / TLS-reset under load - without retry the welcome
// wizard fails out on the first hiccup and a fresh-install user is dead in
// the water. Permanent 4xx errors abort early.
//
// Returns (actualPath, error). actualPath is non-empty only if
// Content-Disposition provided a different filename.
func (d *Downloader) downloadFile(name, rawURL, dest string) (string, error) {
	delays := []time.Duration{0, 2 * time.Second, 5 * time.Second}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			d.emit(name, 0, 0, 0, 0, 0, "downloading",
				fmt.Sprintf("retrying after %s (attempt %d/%d): %v", delay, attempt+1, len(delays), lastErr))
			time.Sleep(delay)
		}
		actualPath, err := d.downloadFileOnce(name, rawURL, dest)
		if err == nil {
			return actualPath, nil
		}
		lastErr = err
		if isPermanentHTTPError(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("after %d attempts: %w", len(delays), lastErr)
}

// isPermanentHTTPError detects 4xx (non-transient) errors so we don't waste
// time retrying a 404 or 403.
func isPermanentHTTPError(err error) bool {
	msg := err.Error()
	for _, code := range []string{"HTTP 400", "HTTP 401", "HTTP 403", "HTTP 404", "HTTP 410"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// downloadHTTPClient is shared across retries so we don't re-establish the
// transport every attempt. HTTP/2 is explicitly disabled - windows.php.net's
// CDN routinely sends a GOAWAY frame after LastStreamID=1, and Go's HTTP/2
// client surfaces that as a fatal "reading: http2: server sent GOAWAY"
// instead of falling back to HTTP/1.1. Forcing HTTP/1.1 sidesteps the
// entire class of bug and costs us nothing - all the upstream hosts we
// download from (windows.php.net, dev.mysql.com, get.enterprisedb.com,
// nodejs.org, github.com releases) handle HTTP/1.1 fine.
//
// TLSNextProto = empty (non-nil) is the documented way to disable HTTP/2
// in net/http: see https://pkg.go.dev/net/http#Transport.TLSNextProto
var downloadHTTPClient = &http.Client{
	Timeout: 30 * time.Minute,
	Transport: &http.Transport{
		TLSNextProto:       map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
		ForceAttemptHTTP2:  false,
		MaxIdleConns:       10,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
	},
}

// downloadFileOnce performs a single download attempt without retry.
func (d *Downloader) downloadFileOnce(name, rawURL, dest string) (string, error) {
	resp, err := downloadHTTPClient.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	// Check Content-Disposition header for the real filename
	actualPath := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				// Sanitize for Windows
				for _, ch := range []string{"?", "&", "=", "#", "%", "*", "|", "<", ">"} {
					fn = strings.ReplaceAll(fn, ch, "_")
				}
				actualPath = filepath.Join(filepath.Dir(dest), fn)
				dest = actualPath
			}
		}
	}

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", dest, err)
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64
	startTime := time.Now()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return "", fmt.Errorf("writing: %w", writeErr)
			}
			downloaded += int64(n)

			if total > 0 {
				pct := float64(downloaded) / float64(total) * 90.0
				elapsed := time.Since(startTime).Seconds()
				speed := 0.0
				eta := 0
				if elapsed > 0 {
					speed = float64(downloaded) / elapsed / 1024 / 1024
					remaining := total - downloaded
					eta = int(float64(remaining) / (float64(downloaded) / elapsed))
				}
				d.emit(name, pct, downloaded, total, speed, eta, "downloading", "")
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("reading: %w", readErr)
		}
	}

	return actualPath, nil
}

func (d *Downloader) emit(name string, pct float64, downloaded, total int64, speed float64, eta int, status, errMsg string) {
	select {
	case d.progressCh <- Progress{
		Name:       name,
		Percent:    pct,
		Downloaded: downloaded,
		Total:      total,
		SpeedMBps:  speed,
		ETA:        eta,
		Status:     status,
		Error:      errMsg,
	}:
	default:
	}
}

func ExtractZip(zipPath, destDir string) error {
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
