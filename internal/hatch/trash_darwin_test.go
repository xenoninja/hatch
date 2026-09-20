package hatch

import (
	"os"
	"path/filepath"
	"testing"
)

// Native trash can return an accessible item in a parent that macOS will not
// let Hatch open. Exercise both normal completion and persisted-receipt recovery.
func TestDarwinRemovalWithoutTrashParentReadAccess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "completion", true: "recovery"}[interrupted], func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			parent := filepath.Join(home, "trash")
			if err := os.Mkdir(parent, 0300); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(parent, 0700); err != nil {
					t.Error(err)
				}
			})
			if f, err := os.Open(parent); err == nil {
				f.Close()
				t.Fatal("fixture must deny opening the trash parent")
			}
			target := filepath.Join(parent, "example")
			controls := []string{"HATCH_TEST_TRASH_TARGET=" + target}
			if interrupted {
				controls = append(controls, "HATCH_TEST_INTERRUPT=after-trash-receipt")
			}
			run(t, home, !interrupted, controls, "remove", "example", "--force")
			contains(t, run(t, home, true, nil, "list"), "No experimental projects")
			if _, err := directoryIdentity(target); err != nil {
				t.Fatal(err)
			}
		})
	}
}
