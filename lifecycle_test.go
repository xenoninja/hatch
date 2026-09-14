package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPromotedStatusIsTerminal(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "registry"), t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	// Promotion is not implemented yet. Seed its persisted result only as
	// fixture setup; exercise and observe behavior through the service seam.
	want := project{"graduated", "2026-09-14", "promoted", filepath.Join(t.TempDir(), "destination")}
	if _, err := s.db.Exec(`INSERT INTO projects(name,created_date,status,location) VALUES(?,?,?,?)`, want.Name, want.Date, want.Status, want.Location); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"active", "completed", "abandoned", "promoted"} {
		if _, err := s.changeStatus(want.Name, status); err == nil || !strings.Contains(err.Error(), "promot") {
			t.Fatalf("status %s: %v", status, err)
		}
		if got, err := s.info(want.Name); err != nil || got != want {
			t.Fatalf("changed promoted record: %+v %v", got, err)
		}
	}
}
