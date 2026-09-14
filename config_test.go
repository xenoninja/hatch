package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "hatch", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func absent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("expected absent %s: %v", path, err)
		}
	}
}

func TestConfiguredExperiments(t *testing.T) {
	for _, kind := range []string{"default-config", "xdg-config", "tilde"} {
		t.Run(kind, func(t *testing.T) {
			home, outside := t.TempDir(), t.TempDir()
			root, configured, want := filepath.Join(home, ".config"), filepath.Join(outside, "new", "nested", "experiments"), filepath.Join(outside, "new", "nested", "experiments")
			var env []string
			if kind == "xdg-config" {
				root = filepath.Join(outside, "config")
				env = []string{"XDG_CONFIG_HOME=" + root}
				writeConfig(t, filepath.Join(home, ".config"), "invalid TOML")
			}
			if kind == "tilde" {
				configured = "~/custom experiments"
				want = filepath.Join(home, "custom experiments")
			}
			writeConfig(t, root, "# Project storage\nexperiments_dir = '"+configured+"'\n")
			contains(t, run(t, home, false, env, "info", "first"), "unknown")
			absent(t, want, filepath.Join(home, ".local"))
			contains(t, run(t, home, true, env, "new", "first"), filepath.Join(want, "2026-09-14-first"))
			contains(t, run(t, home, true, env, "info", "first"), filepath.Join(want, "2026-09-14-first"))
			absent(t, filepath.Join(home, "experiments"))
		})
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"experiments_dir = [", "experiments_dir = 42", "experiments_dir = ''", "experiments_dir = 'relative/path'", "experiments_dir = '~other/path'", "experiments_dir = '~'", "experiments_dir = \"/bad\\u0000path\"", "file", "file-parent", "dangling-link"} {
		t.Run(value, func(t *testing.T) {
			home := t.TempDir()
			if value == "file" || value == "file-parent" || value == "dangling-link" {
				path := filepath.Join(home, "not-directory")
				if value == "dangling-link" {
					if err := os.Symlink("missing", path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if value == "file-parent" {
					path = filepath.Join(path, "child")
				}
				value = "experiments_dir = '" + path + "'"
			}
			writeConfig(t, filepath.Join(home, ".config"), value)
			for _, command := range []string{"new", "info"} {
				contains(t, run(t, home, false, nil, command, "first"), "configuration")
			}
			absent(t, filepath.Join(home, ".local"), filepath.Join(home, "experiments"))
			run(t, home, true, nil, "--help")
		})
	}
}

func TestConfigurationChangesPreserveLocationsAndRecovery(t *testing.T) {
	for _, point := range []string{"", "before-directory", "after-directory", "before-registry", "during-registry", "after-registry"} {
		t.Run(point, func(t *testing.T) {
			home, outside := t.TempDir(), t.TempDir()
			config, oldRoot, newRoot := filepath.Join(home, ".config"), filepath.Join(outside, "old"), filepath.Join(outside, "new")
			env := []string{"XDG_DATA_HOME=" + filepath.Join(outside, "data")}
			writeConfig(t, config, "experiments_dir = '"+oldRoot+"'")
			controls := append(append([]string{}, env...), "HATCH_TEST_INTERRUPT="+point)
			run(t, home, point == "", controls, "new", "first")
			writeConfig(t, config, "experiments_dir = '"+newRoot+"'")
			location := filepath.Join(oldRoot, "2026-09-14-first")
			switch point {
			case "before-directory":
				contains(t, run(t, home, false, env, "info", "first"), "unknown")
				absent(t, location, newRoot)
				contains(t, run(t, home, true, env, "new", "first"), filepath.Join(newRoot, "2026-09-14-first"))
			case "after-directory":
				sentinel := filepath.Join(location, "valuable.txt")
				if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				contains(t, run(t, home, false, env, "info", "first"), "uncertain", location)
				contains(t, run(t, home, false, env, "new", "second"), "mutations blocked", location)
				if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
					t.Fatal("files changed", err)
				}
				absent(t, newRoot)
			default:
				contains(t, run(t, home, true, env, "info", "first"), location, "Status: active")
				absent(t, newRoot)
				contains(t, run(t, home, false, env, "new", "first"), "already tracked")
				contains(t, run(t, home, true, env, "new", "second"), filepath.Join(newRoot, "2026-09-14-second"))
				if _, err := os.Stat(location); err != nil {
					t.Fatal(err)
				}
				absent(t, filepath.Join(newRoot, "2026-09-14-first"))
			}
		})
	}
}

func TestConfigurationDefaults(t *testing.T) {
	for _, kind := range []string{"empty-config", "unset-key", "empty-xdg", "relative-xdg"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			var env []string
			switch kind {
			case "empty-config":
				writeConfig(t, filepath.Join(home, ".config"), "")
			case "unset-key":
				writeConfig(t, filepath.Join(home, ".config"), "# No settings yet\n")
			case "empty-xdg":
				env = []string{"XDG_CONFIG_HOME=", "XDG_DATA_HOME="}
			case "relative-xdg":
				env = []string{"XDG_CONFIG_HOME=relative", "XDG_DATA_HOME=relative"}
			}
			contains(t, run(t, home, true, env, "new", "first"), filepath.Join(home, "experiments", "2026-09-14-first"))
			contains(t, run(t, home, true, env, "info", "first"), "Status: active")
			if _, err := os.Stat(filepath.Join(home, ".local", "share", "hatch", "hatch.db")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStorageBelowExecuteOnlyAncestor(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	home, ancestor := t.TempDir(), filepath.Join(t.TempDir(), "private")
	base := filepath.Join(ancestor, "usable")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, 0100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(ancestor, 0700); err != nil {
			t.Error(err)
		}
	})
	writeConfig(t, filepath.Join(home, ".config"), "experiments_dir = '"+filepath.Join(base, "experiments")+"'")
	env := []string{"XDG_DATA_HOME=" + filepath.Join(base, "data")}
	run(t, home, true, env, "new", "first")
	contains(t, run(t, home, true, env, "info", "first"), filepath.Join(base, "experiments", "2026-09-14-first"))
	// Retry storage initialization after interruption without reading the ancestor.
	interrupted := append(append([]string{}, env...), "HATCH_TEST_INTERRUPT=before-storage-sync")
	run(t, home, false, interrupted, "new", "second")
	run(t, home, true, env, "new", "second")
}

func TestXDGStorage(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	config, data := filepath.Join(outside, "config"), filepath.Join(outside, "nested", "data")
	env := []string{"XDG_CONFIG_HOME=" + config, "XDG_DATA_HOME=" + data}
	contains(t, run(t, home, false, env, "info", "unknown"), "unknown experimental project")
	absent(t, config, data, filepath.Join(home, "experiments"))
	contains(t, run(t, home, true, env, "new", "first"), filepath.Join(home, "experiments", "2026-09-14-first"))
	if _, err := os.Stat(filepath.Join(data, "hatch", "hatch.db")); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, true, env, "info", "first"), "Status: active")
	absent(t, config, filepath.Join(home, ".local"))
	contains(t, run(t, home, false, []string{"XDG_DATA_HOME=" + filepath.Join(outside, "other")}, "info", "first"), "unknown experimental project")
	absent(t, filepath.Join(outside, "other"))
}
