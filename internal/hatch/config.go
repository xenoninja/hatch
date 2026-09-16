package hatch

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
		directory = home + string(filepath.Separator) + directory[2:]
	}
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("configuration %s: experiments_dir must be absolute or begin with ~/", path)
	}
	directory, err = resolveConfiguredDirectory(directory)
	if err != nil {
		return "", fmt.Errorf("configuration %s: invalid experiments_dir: %w", path, err)
	}
	return directory, nil
}

// Resolve components before processing later ".." entries: lexical cleaning
// would discard symlinks (and invalid existing components). Missing directories
// remain valid without being created until a mutation needs them.
func resolveConfiguredDirectory(path string) (string, error) {
	current := string(filepath.Separator)
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			current, err = filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			info, err = os.Stat(current)
			if err != nil {
				return "", err
			}
		}
		if !info.IsDir() {
			return "", fmt.Errorf("not a directory: %s", current)
		}
	}
	return current, nil
}

// XDG homes must be absolute; empty and relative values use the defaults.
func xdgHome(name, fallback string) string {
	if value := os.Getenv(name); filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return fallback
}
