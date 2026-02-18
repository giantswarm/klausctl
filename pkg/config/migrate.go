package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MigrateLayout migrates legacy single-instance layout into instances/default.
// It is safe to call repeatedly.
func MigrateLayout(paths *Paths) error {
	defaultPaths := paths.ForInstance("default")
	if err := EnsureDir(paths.InstancesDir); err != nil {
		return fmt.Errorf("ensuring instances directory: %w", err)
	}

	legacyInstanceFile := filepath.Join(paths.ConfigDir, "instance.json")
	legacyRenderedDir := filepath.Join(paths.ConfigDir, "rendered")
	legacyConfigFile := filepath.Join(paths.ConfigDir, "config.yaml")

	legacyExists := fileExists(legacyInstanceFile) || fileExists(legacyConfigFile) || dirExists(legacyRenderedDir)
	if !legacyExists {
		return nil
	}

	if err := EnsureDir(defaultPaths.InstanceDir); err != nil {
		return fmt.Errorf("ensuring default instance directory: %w", err)
	}

	if err := moveIfExists(legacyConfigFile, defaultPaths.ConfigFile); err != nil {
		return fmt.Errorf("migrating legacy config.yaml: %w", err)
	}
	if err := moveIfExists(legacyInstanceFile, defaultPaths.InstanceFile); err != nil {
		return fmt.Errorf("migrating legacy instance.json: %w", err)
	}
	if err := moveIfExists(legacyRenderedDir, defaultPaths.RenderedDir); err != nil {
		return fmt.Errorf("migrating legacy rendered directory: %w", err)
	}

	return nil
}

func moveIfExists(src, dst string) error {
	if !fileExists(src) && !dirExists(src) {
		return nil
	}
	if fileExists(dst) || dirExists(dst) {
		return nil
	}
	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
