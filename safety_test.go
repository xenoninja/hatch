package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFreshInspectionAndHelp(t *testing.T) {
	home := t.TempDir()
	contains(t, run(t, home, false, nil, "info", "unknown"), "unknown experimental project")
	for _, args := range [][]string{{"--help"}, {"new", "--help"}, {"info", "-h"}, {"list", "--help"}, {"list", "-h"}, {"help"}} {
		contains(t, run(t, home, true, nil, args...), "new <name>", "info <name>", "hatch list")
	}
	run(t, home, false, nil, "new")
	run(t, home, true, nil, "list")
	for _, args := range [][]string{{"list", "extra"}, {"list", "--json"}, {"list", "--help", "extra"}} {
		run(t, home, false, nil, args...)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("inspection initialized home: %v %v", entries, err)
	}
}

func TestNamesAndCollisions(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"", "Upper", "has space", "a/b", "a--b", "-a", "a-", "é", "a_b", "../escape"} {
		contains(t, run(t, home, false, nil, "new", name), "invalid name")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatal("invalid names initialized storage")
	}
	for _, name := range []string{"0", "a", "abc-123-def"} {
		run(t, home, true, nil, "new", name)
	}
	contains(t, run(t, home, false, []string{"HATCH_TEST_TIME=2027-01-01T12:00:00Z"}, "new", "a"), "already tracked")
	location := filepath.Join(home, "experiments", "2026-09-14-collision")
	if err := os.Mkdir(location, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(location, "valuable.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, nil, "new", "collision"), "destination already exists", location)
	contains(t, run(t, home, false, nil, "info", "collision"), "unknown")
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "keep me" {
		t.Fatal("collision changed files")
	}
	for _, name := range []string{"file", "link"} {
		path := filepath.Join(home, "experiments", "2026-09-14-"+name)
		if name == "file" {
			err = os.WriteFile(path, []byte("keep"), 0600)
		} else {
			err = os.Symlink("missing", path)
		}
		if err != nil {
			t.Fatal(err)
		}
		contains(t, run(t, home, false, nil, "new", name), "destination already exists")
	}
}

func TestMissingLocationPreservesStatus(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "gone")
	location := filepath.Join(home, "experiments", "2026-09-14-gone")
	if err := os.Remove(location); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, true, nil, "info", "gone"), "Status: active", "warning:", location)
	contains(t, run(t, home, false, nil, "new", "gone"), "already tracked")
}

func TestConcurrentCreation(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "default"
		if configured {
			name = "configured"
		}
		t.Run(name, func(t *testing.T) { testConcurrentCreation(t, configured) })
	}
}

func testConcurrentCreation(t *testing.T, configured bool) {
	home := t.TempDir()
	experiments := filepath.Join(home, "experiments")
	var env []string
	if configured {
		outside := t.TempDir()
		experiments = filepath.Join(outside, "nested", "experiments")
		config := filepath.Join(outside, "config")
		writeConfig(t, config, "experiments_dir = '"+experiments+"'")
		env = []string{"XDG_CONFIG_HOME=" + config, "XDG_DATA_HOME=" + filepath.Join(outside, "nested", "data")}
	}
	var wg sync.WaitGroup
	results := make(chan bool, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := invoke(home, env, "new", "same"); results <- err == nil }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for ok := range results {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("%d successful concurrent creates", successes)
	}
	contains(t, run(t, home, true, env, "info", "same"), "Status: active", experiments)
	entries, err := os.ReadDir(experiments)
	if err != nil || len(entries) != 1 {
		t.Fatalf("destinations: %v %v", entries, err)
	}
}

func TestInterruptedCreation(t *testing.T) {
	for _, point := range []string{"before-storage-sync", "before-directory", "after-directory", "before-registry", "during-registry", "after-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			out, err := invoke(home, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "interrupted")
			if err == nil || !strings.Contains(err.Error(), "86") {
				t.Fatalf("fault did not fire: %s %v", out, err)
			}
			location := filepath.Join(home, "experiments", "2026-09-14-interrupted")
			switch point {
			case "before-storage-sync", "before-directory":
				contains(t, run(t, home, false, nil, "info", "interrupted"), "unknown")
				if _, err := os.Lstat(location); !os.IsNotExist(err) {
					t.Fatalf("unexpected path: %v", err)
				}
				run(t, home, true, nil, "new", "interrupted")
			case "after-directory":
				sentinel := filepath.Join(location, "valuable.txt")
				if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				contains(t, run(t, home, false, nil, "info", "interrupted"), "uncertain", location)
				contains(t, run(t, home, false, nil, "new", "other"), "mutations blocked", location)
				data, err := os.ReadFile(sentinel)
				if err != nil || string(data) != "keep" {
					t.Fatal("uncertain recovery lost files")
				}
				if _, err := os.Lstat(filepath.Join(home, "experiments", "2026-09-14-other")); !os.IsNotExist(err) {
					t.Fatal("blocked mutation created files")
				}
			default:
				contains(t, run(t, home, true, nil, "info", "interrupted"), "Created: 2026-09-14", "Status: active", location)
				contains(t, run(t, home, false, nil, "new", "interrupted"), "already tracked")
				run(t, home, true, nil, "new", "other")
			}
		})
	}
}

func TestRecoveryRejectsChangedLocation(t *testing.T) {
	for _, change := range []string{"missing", "replacement", "symlink", "intent-collision"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			point := "before-registry"
			if change == "intent-collision" {
				point = "before-directory"
			}
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "changed")
			location := filepath.Join(home, "experiments", "2026-09-14-changed")
			if change != "intent-collision" {
				if err := os.Rename(location, location+"-saved"); err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "replacement", "intent-collision":
				if err := os.Mkdir(location, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(location+"-saved", location); err != nil {
					t.Fatal(err)
				}
			}
			contains(t, run(t, home, false, nil, "list"), "uncertain", location)
			contains(t, run(t, home, false, nil, "new", "blocked"), "uncertain", location, "mutations blocked")
			if change != "intent-collision" {
				if _, err := os.Stat(location + "-saved"); err != nil {
					t.Fatal("saved files lost")
				}
			}
		})
	}
}
