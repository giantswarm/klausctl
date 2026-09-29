// Package fsutil holds filesystem helpers shared by the instance and
// workspace code paths.
package fsutil

import (
	"errors"
	"io/fs"
	"os"
)

// RemoveAll removes path and everything below it, like os.RemoveAll, and
// also succeeds on trees with read-only directories, such as a Go module
// cache (0555 directories, 0444 files) an agent left in its workspace.
// Unlinking needs write permission on the parent directory only, so when the
// first attempt fails, every directory in the tree gains owner write and
// execute permission and the removal is retried. The walk is scoped to path
// through os.Root, so a symlink in the tree never leads it outside.
func RemoveAll(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if root, rootErr := os.OpenRoot(path); rootErr == nil {
		_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, _ error) error {
			if d != nil && d.IsDir() {
				if info, infoErr := d.Info(); infoErr == nil {
					_ = root.Chmod(p, info.Mode().Perm()|0o700)
				}
			}
			return nil
		})
		_ = root.Close()
	}
	return os.RemoveAll(path)
}
