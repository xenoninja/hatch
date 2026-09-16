package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromotionCannotNestTrackedProjects(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "outer")
	run(t, home, true, nil, "new", "inner")
	outer := filepath.Join(home, "experiments", "2026-09-14-outer")
	writeConfig(t, filepath.Join(home, ".config"), "experiments_dir = '"+filepath.Join(home, "elsewhere")+"'")
	target := filepath.Join(outer, "graduated")
	contains(t, run(t, home, false, nil, "promote", "inner", target), "tracked", "outer", outer)
	absent(t, target)
	contains(t, run(t, home, true, nil, "info", "inner"), "active")
	run(t, home, true, nil, "list")
}

func TestCreationCannotNestInTrackedProject(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "outer")
	outer := filepath.Join(home, "experiments", "2026-09-14-outer")
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(outer, alias); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".config"), "experiments_dir = '"+alias+"'")
	contains(t, run(t, home, false, nil, "new", "inner"), "tracked", "outer")
	absent(t, filepath.Join(outer, "2026-09-14-inner"))
	run(t, home, true, nil, "list")
}

func TestPromotionCannotContainMissingTrackedProject(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "first")
	run(t, home, true, nil, "new", "second")
	root := filepath.Join(home, "destination")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	run(t, home, true, nil, "promote", "first", filepath.Join(root, "graduated"))
	if err := os.Rename(root, filepath.Join(home, "moved")); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, nil, "promote", "second", root), "tracked", "first")
	absent(t, root)
	contains(t, run(t, home, true, nil, "info", "second"), "active")
}

func TestMutationsProtectExistingTrackedDescendants(t *testing.T) {
	for _, operation := range []string{"promote", "remove"} {
		t.Run(operation, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "outer")
			outer := filepath.Join(home, "experiments", "2026-09-14-outer")
			run(t, home, true, nil, "new", "inner")
			root := filepath.Join(home, "independent")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			run(t, home, true, nil, "promote", "inner", filepath.Join(root, "graduated"))
			// Simulate pre-existing nesting/external rearrangement without seeding
			// private registry state. The recorded parent now aliases outer/nested.
			nested := filepath.Join(outer, "nested")
			if err := os.Rename(root, nested); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(nested, root); err != nil {
				t.Fatal(err)
			}
			before := run(t, home, true, nil, "info", "inner")
			args := []string{"remove", "outer", "--force"}
			if operation == "promote" {
				args = []string{"promote", "outer", filepath.Join(home, "destination")}
			}
			contains(t, run(t, home, false, nil, args...), "tracked", "inner")
			if _, err := os.Stat(filepath.Join(nested, "graduated")); err != nil {
				t.Fatal(err)
			}
			if got := run(t, home, true, nil, "info", "inner"); got != before {
				t.Fatalf("changed descendant: %s", got)
			}
			contains(t, run(t, home, true, nil, "info", "outer"), "active")
		})
	}
}

func TestCreationRejectsMissingTrackedLocation(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "first")
	root := filepath.Join(home, "destination")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "2026-09-14-second")
	run(t, home, true, nil, "promote", "first", target)
	if err := os.Rename(target, filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".config"), "experiments_dir = '"+root+"'")
	contains(t, run(t, home, false, nil, "new", "second"), "tracked", "first", target)
	absent(t, target)
	contains(t, run(t, home, true, nil, "list"), "first", "promoted")
	run(t, home, true, nil, "new", "third")
}
