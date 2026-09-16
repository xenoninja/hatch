package hatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxTrashInterruptedEvidence(t *testing.T) {
	for _, point := range []string{"linux-trash-planned", "linux-trash-metadata-created", "linux-trash-metadata-written", "linux-trash-metadata-synced", "linux-trash-payload-moved", "after-trash", "after-trash-receipt", "during-removal-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "remove", "example", "--force")
			moved := point == "linux-trash-payload-moved" || point == "after-trash" || point == "after-trash-receipt" || point == "during-removal-registry"
			if moved {
				contains(t, run(t, home, true, nil, "list"), "No experimental projects")
				absent(t, source)
				entries, err := os.ReadDir(filepath.Join(home, ".local/share/Trash/files"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("payload lost: %v %v", entries, err)
				}
				run(t, home, true, nil, "new", "example")
			} else {
				for i := 0; i < 2; i++ {
					contains(t, run(t, home, false, nil, "remove", "example", "--force"), "uncertain interrupted removal", source, "/Trash/files/", "/Trash/info/", "mutations blocked")
				}
				if _, err := os.Stat(source); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLinuxTrashDamagedMetadataBlocksRecovery(t *testing.T) {
	for _, damage := range []string{"missing", "truncated", "symlink"} {
		t.Run(damage, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=after-trash-receipt"}, "remove", "example", "--force")
			entries, err := os.ReadDir(filepath.Join(home, ".local/share/Trash/info"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("metadata: %v %v", entries, err)
			}
			path := filepath.Join(home, ".local/share/Trash/info", entries[0].Name())
			saved := filepath.Join(home, "saved-info")
			if err := os.Rename(path, saved); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "truncated":
				if err := os.WriteFile(path, []byte("[Trash Info]\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(saved, path); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{{"list"}, {"new", "other"}, {"status", "example", "completed"}, {"remove", "example", "--force"}} {
				contains(t, run(t, home, false, nil, args...), "uncertain interrupted removal", path, "mutations blocked")
			}
			if damage != "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(saved, path); err != nil {
				t.Fatal(err)
			}
			contains(t, run(t, home, true, nil, "list"), "No experimental projects")
		})
	}
}
