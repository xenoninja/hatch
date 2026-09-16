package hatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListEmptyInstallation(t *testing.T) {
	home := t.TempDir()
	contains(t, run(t, home, true, nil, "list"), "No experimental projects tracked.")
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("list initialized home: %v %v", entries, err)
	}
	outside := t.TempDir()
	config, data := filepath.Join(outside, "config"), filepath.Join(outside, "data")
	env := []string{"XDG_CONFIG_HOME=" + config, "XDG_DATA_HOME=" + data}
	contains(t, run(t, home, true, env, "list"), "No experimental projects tracked.")
	absent(t, config, data, filepath.Join(home, "experiments"))
}

func TestListMissingLocations(t *testing.T) {
	for _, replacement := range []string{"missing", "file"} {
		t.Run(replacement, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "gone")
			location := filepath.Join(home, "experiments", "2026-09-14-gone")
			if err := os.Remove(location); err != nil {
				t.Fatal(err)
			}
			if replacement == "file" {
				if err := os.WriteFile(location, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				out := run(t, home, true, nil, "list")
				contains(t, out, "gone", "2026-09-14", "active", "warning:", location)
			}
			contains(t, run(t, home, true, nil, "info", "gone"), "Status: active")
		})
	}
}

func TestListRecovery(t *testing.T) {
	for _, point := range []string{"before-storage-sync", "before-directory", "after-directory", "before-registry", "during-registry", "after-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "interrupted")
			location := filepath.Join(home, "experiments", "2026-09-14-interrupted")
			switch point {
			case "before-storage-sync", "before-directory":
				for i := 0; i < 2; i++ {
					contains(t, run(t, home, true, nil, "list"), "No experimental projects tracked.")
				}
				absent(t, location)
				run(t, home, true, nil, "new", "interrupted")
			case "after-directory":
				sentinel := filepath.Join(location, "valuable.txt")
				if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					contains(t, run(t, home, false, nil, "list"), "uncertain", location)
				}
				if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
					t.Fatal("uncertain recovery changed files", err)
				}
			default:
				for i := 0; i < 2; i++ {
					out := run(t, home, true, []string{"HATCH_TEST_TIME=2027-01-01T12:00:00Z"}, "list")
					if got := strings.Join(strings.Fields(out), " "); got != "NAME CREATED STATUS interrupted 2026-09-14 active" {
						t.Fatalf("recovered list: %q", got)
					}
				}
				contains(t, run(t, home, true, nil, "info", "interrupted"), "Status: active", location)
			}
		})
	}
}

func TestListConfiguredLocations(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	config, data := filepath.Join(outside, "config"), filepath.Join(outside, "data")
	oldRoot, newRoot := filepath.Join(outside, "old"), filepath.Join(outside, "new")
	env := []string{"XDG_CONFIG_HOME=" + config, "XDG_DATA_HOME=" + data}
	writeConfig(t, config, "experiments_dir = '"+oldRoot+"'")
	run(t, home, true, env, "new", "first")
	writeConfig(t, config, "experiments_dir = '"+newRoot+"'")
	out := run(t, home, true, env, "list")
	contains(t, out, "first", "active")
	if strings.Contains(out, "warning:") {
		t.Fatalf("list ignored stored location: %s", out)
	}
	absent(t, newRoot, filepath.Join(home, ".local"), filepath.Join(home, "experiments"))
	contains(t, run(t, home, true, nil, "list"), "No experimental projects tracked.")
	writeConfig(t, config, "invalid TOML")
	contains(t, run(t, home, false, env, "list"), "configuration")
	run(t, home, true, env, "list", "--help")
}

func TestListRecordedCreationOrder(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, []string{"HATCH_TEST_TIME=2026-09-13T12:00:00Z"}, "new", "oldest")
	run(t, home, true, nil, "new", "alpha")
	run(t, home, true, nil, "new", "zulu")
	// Recorded creation order wins even when the clock moves backwards.
	run(t, home, true, []string{"HATCH_TEST_TIME=2026-09-12T12:00:00Z"}, "new", "latest")
	if err := os.Mkdir(filepath.Join(home, "experiments", "untracked"), 0755); err != nil {
		t.Fatal(err)
	}
	want := "NAME CREATED STATUS latest 2026-09-12 active zulu 2026-09-14 active alpha 2026-09-14 active oldest 2026-09-13 active"
	for i := 0; i < 2; i++ {
		out := run(t, home, true, nil, "list")
		if got := strings.Join(strings.Fields(out), " "); got != want {
			t.Fatalf("list: got %q, want %q", got, want)
		}
	}
}
