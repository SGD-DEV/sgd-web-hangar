package ssl

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed bin/mkcert.exe
var mkcertBinary []byte

// ensureMkcert copies the embedded mkcert.exe to dest if it doesn't already
// exist there. Returns dest on success.
//
// The NSIS installer only ships hangar.exe; mkcert.exe used to live at
// assets/binaries/mkcert.exe (next to the dev repo) and was missing on
// every installed copy, so SSL features silently failed with "mkcert not
// found at C:\Users\…\AppData\Local\Hangar\assets\binaries\mkcert.exe".
// Embedding + extract-on-first-use makes SSL work without a separate
// installer step.
func ensureMkcert(dest string) (string, error) {
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, mkcertBinary, 0755); err != nil {
		return "", err
	}
	return dest, nil
}
