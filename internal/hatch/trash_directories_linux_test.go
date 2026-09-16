package hatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxTrashSymlinkedDirectoriesBlockRecovery(t *testing.T) {
	for _, directory := range []string{".", "files", "info"} {
		t.Run(directory, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=after-trash-receipt"}, "remove", "example", "--force")
			path := filepath.Join(home, ".local/share/Trash", directory)
			saved := filepath.Join(home, "saved-trash-directory")
			if err := os.Rename(path, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(saved, path); err != nil {
				t.Fatal(err)
			}
			contains(t, run(t, home, false, nil, "list"), "uncertain interrupted removal", "mutations blocked", path)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(saved, path); err != nil {
				t.Fatal(err)
			}
			contains(t, run(t, home, true, nil, "list"), "No experimental projects")
		})
	}
}
