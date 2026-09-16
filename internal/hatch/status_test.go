package hatch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStatusMissingLocationAndChangedConfiguration(t *testing.T) {
	for _, replacement := range []string{"missing", "file"} {
		t.Run(replacement, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "experiment")
			location := filepath.Join(home, "experiments", "2026-09-14-experiment")
			if err := os.Remove(location); err != nil {
				t.Fatal(err)
			}
			if replacement == "file" {
				if err := os.WriteFile(location, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			newRoot := filepath.Join(home, "elsewhere")
			writeConfig(t, filepath.Join(home, ".config"), "experiments_dir = '"+newRoot+"'")
			for _, status := range []string{"completed", "abandoned", "active"} {
				for _, args := range [][]string{{"status", "experiment", status}, {"info", "experiment"}} {
					contains(t, run(t, home, true, nil, args...), "Name: experiment", "Created: 2026-09-14", "Status: "+status, "Location: "+location, "warning:")
				}
				contains(t, run(t, home, true, nil, "list"), status, "warning:", location)
			}
			absent(t, newRoot)
			if replacement == "missing" {
				absent(t, location)
			} else if data, err := os.ReadFile(location); err != nil || string(data) != "keep" {
				t.Fatalf("replacement changed: %q %v", data, err)
			}
		})
	}
}

func TestStatusPendingRecovery(t *testing.T) {
	for _, point := range []string{"before-directory", "after-directory", "before-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "existing")
			before := run(t, home, true, nil, "info", "existing")
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "interrupted")
			if point == "after-directory" {
				contains(t, run(t, home, false, nil, "status", "existing", "completed"), "mutations blocked")
				// Resolve the uncertain empty directory externally, then inspect through CLI.
				location := filepath.Join(home, "experiments", "2026-09-14-interrupted")
				entries, err := os.ReadDir(location)
				if err != nil || len(entries) != 0 {
					t.Fatalf("pending directory changed: %v %v", entries, err)
				}
				if err := os.Remove(location); err != nil {
					t.Fatal(err)
				}
				if got := run(t, home, true, nil, "info", "existing"); got != before {
					t.Fatalf("blocked status changed record: %s", got)
				}
			} else {
				run(t, home, true, nil, "status", "existing", "completed")
				contains(t, run(t, home, true, nil, "info", "existing"), "Status: completed")
				if point == "before-registry" {
					contains(t, run(t, home, true, nil, "info", "interrupted"), "Status: active")
				}
			}
		})
	}
}

func TestConcurrentStatusUpdates(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "experiment")
	location := filepath.Join(home, "experiments", "2026-09-14-experiment")
	sentinel := filepath.Join(location, "valuable.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		status := []string{"active", "completed", "abandoned"}[i%3]
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := invoke(home, nil, "status", "experiment", status)
			if err != nil || !strings.Contains(out, "Status: "+status+"\n") {
				t.Errorf("concurrent status: %s %v", out, err)
			}
		}()
	}
	wg.Wait()
	out := run(t, home, true, nil, "info", "experiment")
	contains(t, out, "Name: experiment", "Created: 2026-09-14", "Location: "+location)
	if !strings.Contains(out, "Status: active\n") && !strings.Contains(out, "Status: completed\n") && !strings.Contains(out, "Status: abandoned\n") {
		t.Fatalf("invalid final status: %s", out)
	}
	list := strings.Join(strings.Fields(run(t, home, true, nil, "list")), " ")
	if len(strings.Fields(list)) != 6 {
		t.Fatalf("unexpected records: %s", list)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("contents changed: %q %v", data, err)
	}
}

func TestInterruptedStatus(t *testing.T) {
	for _, point := range []string{"during-status", "after-status"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "experiment")
			out, err := invoke(home, []string{"HATCH_TEST_INTERRUPT=" + point}, "status", "experiment", "completed")
			if err == nil || !strings.Contains(err.Error(), "86") {
				t.Fatalf("fault did not fire: %s %v", out, err)
			}
			want := "active"
			if point == "after-status" {
				want = "completed"
			}
			contains(t, run(t, home, true, nil, "info", "experiment"), "Status: "+want, "Created: 2026-09-14", filepath.Join(home, "experiments", "2026-09-14-experiment"))
			run(t, home, true, nil, "status", "experiment", "abandoned")
			contains(t, run(t, home, true, nil, "info", "experiment"), "Status: abandoned")
		})
	}
}

func TestStatusHelp(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"status", "--help"}, {"status", "-h"}} {
		contains(t, run(t, home, true, nil, args...), "status <name> <status>", "active", "completed", "abandoned", "Promoted projects stay listed, but cannot", "be changed or removed through Hatch")
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help initialized storage: %v %v", entries, err)
	}
}

func TestStatusErrors(t *testing.T) {
	home := t.TempDir()
	contains(t, run(t, home, false, nil, "status", "unknown", "active"), "unknown experimental project")
	absent(t, filepath.Join(home, ".local"), filepath.Join(home, "experiments"))
	run(t, home, true, nil, "new", "experiment")
	before := run(t, home, true, nil, "info", "experiment")
	for _, status := range []string{"", "missing", "Active", "finished", "promoted"} {
		out := run(t, home, false, nil, "status", "experiment", status)
		if status == "promoted" {
			contains(t, out, "promotion")
		} else {
			contains(t, out, "invalid status", "active", "completed", "abandoned")
		}
		if got := run(t, home, true, nil, "info", "experiment"); got != before {
			t.Fatalf("failed command changed metadata: %s", got)
		}
	}
	contains(t, run(t, home, false, nil, "status", "unknown", "completed"), "unknown experimental project")
	contains(t, run(t, home, false, nil, "status", "Bad/name", "active"), "invalid name")
	for _, args := range [][]string{{"status"}, {"status", "experiment"}, {"status", "experiment", "active", "extra"}} {
		contains(t, run(t, home, false, nil, args...), "status <name> <status>")
	}
}

func TestStatusTransitions(t *testing.T) {
	for _, from := range []string{"active", "completed", "abandoned"} {
		for _, to := range []string{"active", "completed", "abandoned"} {
			t.Run(from+"-to-"+to, func(t *testing.T) {
				home := t.TempDir()
				run(t, home, true, nil, "new", "experiment")
				location := filepath.Join(home, "experiments", "2026-09-14-experiment")
				sentinel := filepath.Join(location, "valuable.txt")
				if err := os.WriteFile(sentinel, []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
				run(t, home, true, nil, "status", "experiment", from)
				run(t, home, true, nil, "new", "later")
				env := []string{"HATCH_TEST_TIME=2027-01-01T12:00:00Z"}
				want := "Name: experiment\nCreated: 2026-09-14\nStatus: " + to + "\nLocation: " + location + "\n"
				if out := run(t, home, true, env, "status", "experiment", to); out != want {
					t.Fatalf("status: %q, want %q", out, want)
				}
				if out := run(t, home, true, env, "info", "experiment"); out != want {
					t.Fatalf("info: %q, want %q", out, want)
				}
				out := run(t, home, true, nil, "list")
				if got := strings.Join(strings.Fields(out), " "); got != "NAME CREATED STATUS later 2026-09-14 active experiment 2026-09-14 "+to {
					t.Fatalf("list: %q", got)
				}
				if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep me" {
					t.Fatalf("contents changed: %q %v", data, err)
				}
			})
		}
	}
}
