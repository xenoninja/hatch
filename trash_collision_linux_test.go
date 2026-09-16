package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The native trash boundary accepts a durable-plan callback. Simulate another
// trash client claiming either path after selection, before Hatch creates it.
func TestLinuxTrashNeverOverwritesConcurrentEntry(t *testing.T) {
	for _, collision := range []string{"metadata", "payload"} {
		t.Run(collision, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
			source := filepath.Join(home, "project")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "keep"), []byte("project contents"), 0600); err != nil {
				t.Fatal(err)
			}
			var occupied string
			_, err := nativeTrash(source, func(target, info string) error {
				if collision == "metadata" {
					occupied = filepath.Join(filepath.Dir(filepath.Dir(target)), "info", filepath.Base(target)+".trashinfo")
				} else {
					if err := os.Mkdir(target, 0700); err != nil {
						return err
					}
					occupied = filepath.Join(target, "keep")
				}
				return os.WriteFile(occupied, []byte("other client's entry"), 0600)
			})
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("expected exclusive collision failure, got %v", err)
			}
			data, err := os.ReadFile(occupied)
			if err != nil || string(data) != "other client's entry" {
				t.Fatalf("overwrote trash: %q %v", data, err)
			}
			data, err = os.ReadFile(filepath.Join(source, "keep"))
			if err != nil || string(data) != "project contents" {
				t.Fatalf("lost project: %q %v", data, err)
			}
		})
	}
}
