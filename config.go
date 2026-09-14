package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func experimentsDirectory(home string) (string, error) {
	path := filepath.Join(xdgHome("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "hatch", "config.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Join(home, "experiments"), nil
	}
	if err != nil {
		return "", fmt.Errorf("read configuration %s: %w", path, err)
	}
	var config struct {
		ExperimentsDir *string `toml:"experiments_dir"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("configuration %s: %w", path, err)
	}
	if config.ExperimentsDir == nil {
		return filepath.Join(home, "experiments"), nil
	}
	directory := *config.ExperimentsDir
	if strings.HasPrefix(directory, "~/") {
		directory = filepath.Join(home, directory[2:])
	}
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("configuration %s: experiments_dir must be absolute or begin with ~/", path)
	}
	directory = filepath.Clean(directory)
	if err := validateDirectory(directory); err != nil {
		return "", fmt.Errorf("configuration %s: invalid experiments_dir: %w", path, err)
	}
	return directory, nil
}

// Missing directories are valid and created lazily. Check existing ancestors
// too, so a file or broken symlink cannot masquerade as a usable directory.
func validateDirectory(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("not a directory: %s", current)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, linkErr := os.Lstat(current); linkErr == nil {
			return fmt.Errorf("unavailable directory: %s", current)
		} else if !errors.Is(linkErr, os.ErrNotExist) {
			return linkErr
		}
		if current == filepath.Dir(current) {
			return err
		}
	}
}

// XDG homes must be absolute; empty and relative values use the defaults.
func xdgHome(name, fallback string) string {
	if value := os.Getenv(name); filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return fallback
}
