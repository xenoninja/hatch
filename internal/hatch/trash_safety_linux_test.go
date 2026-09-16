package hatch

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLinuxUnavailableTrashRetainsTracking(t *testing.T) {
	for _, kind := range []string{"symlink", "file", "permissions", "info-symlink", "files-symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			root := filepath.Join(home, ".local/share/Trash")
			outside := filepath.Join(home, "unrelated")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(outside, "keep")
			if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(outside, root); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(root, []byte("not trash"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "permissions" {
					if err := os.Chmod(root, 0755); err != nil {
						t.Fatal(err)
					}
				} else {
					name := "info"
					if kind == "files-symlink" {
						name = "files"
					}
					if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			contains(t, run(t, home, false, nil, "remove", "example", "--force"), "Linux trash unavailable", "tracking retained", root)
			contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
			run(t, home, true, nil, "status", "example", "completed")
			if _, err := os.Stat(filepath.Join(home, "experiments", "2026-09-14-example")); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "untouched" {
				t.Fatalf("unrelated entry changed: %q %v", data, err)
			}
		})
	}
}

func TestLinuxTrashRejectsDifferentFilesystem(t *testing.T) {
	home := t.TempDir()
	outside, err := os.MkdirTemp("/dev/shm", "hatch-trash-")
	if err != nil {
		t.Skip("no writable second filesystem: ", err)
	}
	defer os.RemoveAll(outside)
	a, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if a.Sys().(*syscall.Stat_t).Dev == b.Sys().(*syscall.Stat_t).Dev {
		t.Skip("temporary paths share a filesystem")
	}
	config := filepath.Join(home, "config")
	writeConfig(t, config, "experiments_dir = '"+outside+"'")
	controls := []string{"XDG_CONFIG_HOME=" + config}
	run(t, home, true, controls, "new", "example")
	contains(t, run(t, home, false, controls, "remove", "example", "--force"), "another filesystem", "no copy/delete fallback", "tracking retained")
	contains(t, run(t, home, true, controls, "info", "example"), "Status: active", outside)
	if _, err := os.Stat(filepath.Join(outside, "2026-09-14-example")); err != nil {
		t.Fatal(err)
	}
	absent(t, filepath.Join(home, ".local/share/Trash"))
}

func TestLinuxTrashCompetingOperations(t *testing.T) {
	for _, other := range []string{"remove", "promote"} {
		t.Run(other, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			args := []string{other, "example", "--force"}
			if other == "promote" {
				args = []string{"promote", "example", filepath.Join(home, "promoted")}
			}
			results := make(chan error, 2)
			go func() { _, err := invoke(home, nil, "remove", "example", "--force"); results <- err }()
			go func() { _, err := invoke(home, nil, args...); results <- err }()
			successes := 0
			for i := 0; i < 2; i++ {
				if <-results == nil {
					successes++
				}
			}
			if successes != 1 {
				t.Fatalf("successful competing operations: %d", successes)
			}
			if _, err := os.Stat(filepath.Join(home, "promoted")); err == nil {
				contains(t, run(t, home, true, nil, "info", "example"), "Status: promoted")
				contains(t, run(t, home, false, nil, "remove", "example", "--force"), "promoted", "terminal")
			} else {
				entries, err := os.ReadDir(filepath.Join(home, ".local/share/Trash/files"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("trash entries: %v %v", entries, err)
				}
				info, err := os.ReadDir(filepath.Join(home, ".local/share/Trash/info"))
				if err != nil || len(info) != 1 || info[0].Name() != entries[0].Name()+".trashinfo" {
					t.Fatalf("trash metadata: %v %v", info, err)
				}
				contains(t, run(t, home, true, nil, "list"), "No experimental projects")
				run(t, home, true, nil, "new", "example")
			}
		})
	}
}
