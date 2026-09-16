package hatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveHelpAndUsage(t *testing.T) {
	home := t.TempDir()
	contains(t, run(t, home, true, nil, "remove", "--help"), "remove <name> [--force]", "Move to trash and stop tracking")
	for _, args := range [][]string{{"remove"}, {"remove", "example", "--unknown"}, {"remove", "example", "--force", "extra"}, {"remove", "Upper"}} {
		run(t, home, false, nil, args...)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help/errors initialized storage: %v %v", entries, err)
	}
}

func TestRemoveEligibleStatusesAndTrashCollision(t *testing.T) {
	for _, status := range []string{"active", "completed", "abandoned"} {
		t.Run(status, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			run(t, home, true, nil, "status", "example", status)
			target := filepath.Join(home, "trashed")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "unrelated"), []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			controls := []string{"HATCH_TEST_TRASH_TARGET=" + target}
			run(t, home, false, controls, "remove", "example", "--force")
			contains(t, run(t, home, true, nil, "info", "example"), "Status: "+status)
			data, err := os.ReadFile(filepath.Join(target, "unrelated"))
			if err != nil || string(data) != "untouched" {
				t.Fatalf("unrelated trash: %q %v", data, err)
			}
			run(t, home, true, []string{"HATCH_TEST_TRASH_TARGET=" + target + "-new"}, "remove", "example", "--force")
			run(t, home, true, nil, "new", "example")
		})
	}
}

func TestRemoveFailuresAndLifecycle(t *testing.T) {
	for _, kind := range []string{"noninteractive", "trash-failure", "promoted", "promoted-missing", "symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			controls := []string{"HATCH_TEST_TRASH_FAIL=1"}
			args := []string{"remove", "--force", "example"}
			switch kind {
			case "noninteractive":
				args = []string{"remove", "example"}
			case "promoted", "promoted-missing":
				source = filepath.Join(home, "promoted")
				run(t, home, true, nil, "promote", "example", source)
				if kind == "promoted-missing" {
					if err := os.Remove(source); err != nil {
						t.Fatal(err)
					}
				}
			case "symlink", "file":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(home, source); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(source, []byte("unrelated"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			out := run(t, home, false, controls, args...)
			if strings.HasPrefix(kind, "promoted") {
				contains(t, out, "promoted", "terminal")
			}
			if kind == "trash-failure" {
				contains(t, out, "trash failed", "tracking retained")
			}
			run(t, home, true, nil, "info", "example")
			if kind != "promoted-missing" {
				if _, err := os.Lstat(source); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRemoveInterruptedNativeCallRetainsEvidence(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	target := filepath.Join(home, "trashed")
	run(t, home, false, []string{"HATCH_TEST_TRASH_TARGET=" + target, "HATCH_TEST_INTERRUPT=trash-in-flight"}, "remove", "example", "--force")
	// The source still exists, but an orphaned native helper could yet move it.
	// Recovery must not clear intent based on source presence in this window.
	for i := 0; i < 2; i++ {
		contains(t, run(t, home, false, []string{"HATCH_TEST_TRASH_FAIL=1"}, "remove", "example", "--force"), "uncertain interrupted removal", "pending evidence")
	}
	absent(t, target)
}

func TestRemoveRecovery(t *testing.T) {
	for _, point := range []string{"before-trash", "after-trash", "after-trash-receipt", "during-removal-registry", "after-removal-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			target := filepath.Join(home, "trashed")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			run(t, home, false, []string{"HATCH_TEST_TRASH_TARGET=" + target, "HATCH_TEST_INTERRUPT=" + point}, "remove", "example", "--force")
			if point == "after-trash" {
				for _, args := range [][]string{{"info", "example"}, {"list"}, {"new", "other"}, {"status", "example", "completed"}, {"promote", "example", filepath.Join(home, "outside")}, {"remove", "example", "--force"}} {
					contains(t, run(t, home, false, nil, args...), "uncertain interrupted removal", "pending evidence", "mutations blocked")
				}
				if _, err := os.Stat(target); err != nil {
					t.Fatal(err)
				}
				return
			}
			if point == "before-trash" {
				contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
				absent(t, target)
				run(t, home, true, []string{"HATCH_TEST_TRASH_TARGET=" + target}, "remove", "example", "--force")
			} else {
				contains(t, run(t, home, true, nil, "list"), "No experimental projects")
			}
			absent(t, source)
			if _, err := os.Stat(target); err != nil {
				t.Fatal(err)
			}
			run(t, home, true, nil, "new", "example")
		})
	}
}

func TestRemoveAmbiguousReceipt(t *testing.T) {
	for _, kind := range []string{"both-present", "both-missing", "replaced-target", "replaced-source"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			target := filepath.Join(home, "trashed")
			point := "after-trash-receipt"
			if kind == "replaced-source" {
				point = "before-trash"
			}
			run(t, home, false, []string{"HATCH_TEST_TRASH_TARGET=" + target, "HATCH_TEST_INTERRUPT=" + point}, "remove", "example", "--force")
			switch kind {
			case "both-present":
				if err := os.Mkdir(source, 0700); err != nil {
					t.Fatal(err)
				}
			case "both-missing":
				if err := os.Rename(target, target+"-saved"); err != nil {
					t.Fatal(err)
				}
			case "replaced-target":
				if err := os.Rename(target, target+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "replaced-source":
				if err := os.Rename(source, source+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(source, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				contains(t, run(t, home, false, nil, "remove", "example", "--force"), "uncertain interrupted removal", source, "pending evidence")
			}
		})
	}
}

func TestRemoveRecordOnlyAtomicityAndDestination(t *testing.T) {
	for _, point := range []string{"during-removal-registry", "after-removal-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "remove", "example", "--force")
			run(t, home, point == "during-removal-registry", nil, "info", "example")
			if point == "during-removal-registry" {
				run(t, home, true, nil, "remove", "example", "--force")
			}
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			contains(t, run(t, home, false, nil, "new", "example"), "destination already exists")
			run(t, home, true, []string{"HATCH_TEST_TIME=2026-09-16T12:00:00Z"}, "new", "example")
		})
	}
}

func TestCompetingRemovals(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	target := filepath.Join(home, "trashed")
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := invoke(home, []string{"HATCH_TEST_TRASH_TARGET=" + target}, "remove", "example", "--force")
			results <- err
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful competing removals: %d", successes)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, true, nil, "list"), "No experimental projects")
}

func TestRemoveConfirmation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, answer := range []string{"yes\n", "y\n", "no\n", "\n", "yes"} {
			t.Run(fmt.Sprintf("missing=%t/%q", missing, answer), func(t *testing.T) {
				home := t.TempDir()
				run(t, home, true, nil, "new", "example")
				source := filepath.Join(home, "experiments", "2026-09-14-example")
				target := filepath.Join(home, "trashed")
				if missing {
					if err := os.Remove(source); err != nil {
						t.Fatal(err)
					}
				}
				cmd := command(home, []string{"HATCH_TEST_TERMINAL=1", "HATCH_TEST_TRASH_TARGET=" + target}, "remove", "example")
				cmd.Stdin = strings.NewReader(answer)
				out, err := cmd.CombinedOutput()
				if (err != nil) != (answer == "yes") {
					t.Fatalf("confirmation: %s %v", out, err)
				}
				accepted := answer == "yes\n" || answer == "y\n"
				run(t, home, !accepted, nil, "info", "example")
				contains(t, string(out), "Continue? [y/N]")
				if missing {
					contains(t, string(out), "record-only")
				}
				if !accepted {
					absent(t, target)
				}
			})
		}
	}
}

func TestRemoveFiles(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	source := filepath.Join(home, "experiments", "2026-09-14-example")
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("precious contents"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := directoryIdentity(source)
	if err != nil {
		t.Fatal(err)
	}
	out := run(t, home, true, nil, "remove", "example", "--force")
	contains(t, out, "Trashed to: ", "Removed")
	target := trashPath(t, out)
	data, err := os.ReadFile(filepath.Join(target, "keep.txt"))
	if err != nil || string(data) != "precious contents" {
		t.Fatalf("trash contents: %q %v", data, err)
	}
	// Delete only this disposable project's returned trash entry, never the trash directory.
	t.Cleanup(func() {
		actual, err := directoryIdentity(target)
		if err == nil && actual == identity {
			if err := os.RemoveAll(target); err != nil {
				t.Error(err)
			}
		} else {
			t.Errorf("disposable trash identity changed; left untouched: %s", target)
		}
	})
	absent(t, source)
	run(t, home, false, nil, "info", "example")
	run(t, home, true, nil, "new", "example")
}

func trashPath(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Trashed to: ") {
			return strings.TrimPrefix(line, "Trashed to: ")
		}
	}
	t.Fatalf("missing trash receipt: %s", out)
	return ""
}

func TestRemoveMissingRequiresConsent(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	source := filepath.Join(home, "experiments", "2026-09-14-example")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, nil, "remove", "example"), "record-only", "--force")
	contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
	contains(t, run(t, home, true, nil, "remove", "example", "--force"), "record-only", "Removed")
	run(t, home, false, nil, "info", "example")
	run(t, home, true, nil, "new", "example")
}
