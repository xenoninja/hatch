package hatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromotedStatusIsTerminal(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "graduated")
	source := filepath.Join(home, "experiments", "2026-09-14-graduated")
	if err := os.WriteFile(filepath.Join(source, "keep"), []byte("project contents"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(home, "destination")
	run(t, home, true, nil, "promote", "graduated", destination)
	want := run(t, home, true, nil, "info", "graduated")
	for _, status := range []string{"active", "completed", "abandoned", "promoted"} {
		contains(t, run(t, home, false, nil, "status", "graduated", status), "promot")
		if got := run(t, home, true, nil, "info", "graduated"); got != want {
			t.Fatalf("changed promoted record: %s", got)
		}
		data, err := os.ReadFile(filepath.Join(destination, "keep"))
		if err != nil || string(data) != "project contents" {
			t.Fatalf("changed project files: %q %v", data, err)
		}
	}
}
