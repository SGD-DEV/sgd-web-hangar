// Package atomicfile writes files in a way that survives a power loss or
// process crash mid-write: write to a sibling .tmp file, fsync it, then rename
// over the target. The rename is atomic on Windows (MoveFileEx with REPLACE)
// and on POSIX. Without this, every os.WriteFile in the codebase is a corrupt-
// the-user's-file-if-the-power-blinks bug.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write replaces dest with data atomically. On error, dest is left untouched.
// perm is applied to the final file (after rename); on Windows it is a no-op.
func Write(dest string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("atomicfile: making dir %s: %w", dir, err)
	}

	// Use the dest dir for the tmp file so the rename is on the same volume
	// (cross-volume os.Rename falls back to copy and is no longer atomic).
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(dest)+"-*")
	if err != nil {
		return fmt.Errorf("atomicfile: creating tmp: %w", err)
	}
	tmpName := tmp.Name()

	// Best-effort cleanup if anything below fails before the rename.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("atomicfile: writing: %w", err)
	}
	// fsync ensures the bytes are on disk before we expose the file via
	// rename — without this, a power loss between rename and flush leaves an
	// empty file at dest.
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("atomicfile: syncing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: closing tmp: %w", err)
	}

	if perm != 0 {
		if err := os.Chmod(tmpName, perm); err != nil && !errors.Is(err, os.ErrPermission) {
			// Best-effort; not fatal on Windows where chmod is largely cosmetic.
		}
	}

	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: renaming over %s: %w", dest, err)
	}
	return nil
}

// WriteFromReader is the same as Write but streams from r — useful for large
// files (e.g. downloaded archives) where holding the full bytes in memory
// would be wasteful.
func WriteFromReader(dest string, r io.Reader, perm os.FileMode) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("atomicfile: making dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(dest)+"-*")
	if err != nil {
		return fmt.Errorf("atomicfile: creating tmp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: copying: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: syncing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: closing: %w", err)
	}
	if perm != 0 {
		_ = os.Chmod(tmpName, perm)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicfile: renaming over %s: %w", dest, err)
	}
	return nil
}
