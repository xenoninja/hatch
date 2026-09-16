package hatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxTrashMetadataAndNameReuse(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with % space")
	run(t, home, true, nil, "new", "example")
	source := filepath.Join(home, "experiments", "2026-09-14-example")
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("precious contents"), 0600); err != nil {
		t.Fatal(err)
	}
	out := run(t, home, true, nil, "remove", "example", "--force")
	target := trashPath(t, out)
	data, err := os.ReadFile(filepath.Join(target, "keep.txt"))
	if err != nil || string(data) != "precious contents" {
		t.Fatalf("payload: %q %v", data, err)
	}
	info := filepath.Join(home, ".local", "share", "Trash", "info", filepath.Base(target)+".trashinfo")
	data, err = os.ReadFile(info)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, string(data), "[Trash Info]\nPath=", "/home%20with%20%25%20space/experiments/2026-09-14-example\n", "DeletionDate=2026-09-14T18:30:00\n")
	absent(t, source)
	run(t, home, false, nil, "info", "example")
	run(t, home, true, nil, "new", "example")
	second := trashPath(t, run(t, home, true, nil, "remove", "example", "--force"))
	if second == target {
		t.Fatal("reused existing trash entry")
	}
	data, err = os.ReadFile(filepath.Join(target, "keep.txt"))
	if err != nil || string(data) != "precious contents" {
		t.Fatalf("original trash changed: %q %v", data, err)
	}
}
