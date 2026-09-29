// Package fsutil holds filesystem helpers shared by the instance and
// workspace code paths.
package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// RemoveAll removes path and everything below it, like os.RemoveAll, and
// also succeeds on trees with read-only directories, such as a Go module
// cache (0555 directories, 0444 files) an agent left in its workspace.
// Unlinking needs write permission on the parent directory only, so when the
// first attempt fails, every directory in the tree gains owner write and
// execute permission and the removal is retried.
func RemoveAll(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if d != nil && d.IsDir() {
			if info, infoErr := d.Info(); infoErr == nil {
				_ = os.Chmod(p, info.Mode().Perm()|0o700)
			}
		}
		return nil
	})
	return os.RemoveAll(path)
}
