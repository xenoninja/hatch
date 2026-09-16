package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPromotionResolvesParentBeforeCleaning(t *testing.T) {
	for _, kind := range []string{"external-alias", "experiments-alias", "missing-component"} {
		t.Run(kind, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			run(t, home, true, nil, "new", "example")
			parent := filepath.Join(home, "external")
			if kind == "experiments-alias" {
				parent = filepath.Join(home, "experiments")
			}
			target := home + "/missing/../target"
			if kind != "missing-component" {
				if err := os.MkdirAll(filepath.Join(parent, "nested"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(parent, "nested"), filepath.Join(home, "alias")); err != nil {
					t.Fatal(err)
				}
				target = home + "/alias/../target"
			}
			run(t, home, kind == "external-alias", nil, "promote", "example", target)
			if kind == "external-alias" {
				contains(t, run(t, home, true, nil, "info", "example"), "Status: promoted", "Location: "+filepath.Join(parent, "target"))
				if _, err := os.Stat(filepath.Join(parent, "target")); err != nil {
					t.Fatal(err)
				}
			} else {
				contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
			}
			absent(t, filepath.Join(home, "target"))
		})
	}
}

func TestPromotionDoesNotReuseTrackedLocation(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, home, true, nil, "new", "first")
	run(t, home, true, nil, "new", "second")
	target := filepath.Join(home, "target")
	run(t, home, true, nil, "promote", "first", target)
	if err := os.Rename(target, target+"-saved"); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, nil, "promote", "second", target), "already tracked")
	contains(t, run(t, home, true, nil, "info", "first"), "Status: promoted", "Location: "+target, "warning:")
	contains(t, run(t, home, true, nil, "info", "second"), "Status: active", "Location: "+filepath.Join(home, "experiments", "2026-09-14-second"))
	absent(t, target)
	run(t, home, true, nil, "promote", "second", target+"-second")
}

func TestPromotionHelpAndUsage(t *testing.T) {
	home := t.TempDir()
	contains(t, run(t, home, true, nil, "promote", "--help"), "promote <name> <target-path>", "exact final location", "Cross-filesystem")
	for _, args := range [][]string{{"promote"}, {"promote", "example"}, {"promote", "example", "target", "extra"}, {"promote", "Upper", "target"}, {"promote", "unknown", "target"}} {
		run(t, home, false, nil, args...)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help/errors initialized storage: %v %v", entries, err)
	}
}

func TestPromotionRejectsCaseAlias(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	alias := filepath.Join(home, "EXPERIMENTS")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem")
	} else if err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, nil, "promote", "example", filepath.Join(alias, "target")), "outside")
	contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
}

func TestPromotionAcrossRealFilesystems(t *testing.T) {
	home := t.TempDir()
	// Linux commonly provides a second filesystem here. Other environments
	// still exercise the kernel EXDEV failure path using the tagged fault hook.
	outside, err := os.MkdirTemp("/dev/shm", "hatch-promotion-")
	if err != nil {
		t.Skip("no writable second filesystem: ", err)
	}
	defer os.RemoveAll(outside)
	sourceInfo, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if sourceInfo.Sys().(*syscall.Stat_t).Dev == targetInfo.Sys().(*syscall.Stat_t).Dev {
		t.Skip("temporary paths share a filesystem")
	}
	run(t, home, true, nil, "new", "example")
	source := filepath.Join(home, "experiments", "2026-09-14-example")
	target := filepath.Join(outside, "target")
	contains(t, run(t, home, false, nil, "promote", "example", target), "cross-filesystem")
	contains(t, run(t, home, true, nil, "info", "example"), "Status: active", "Location: "+source)
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
	absent(t, target)
}

func TestPromotionRelativeTargetAndChangedConfiguration(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, home, true, nil, "new", "example")
	config := filepath.Join(home, "config")
	// The configured directory may be missing behind a symlinked ancestor.
	if err := os.Symlink(home, filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, config, "experiments_dir = '"+filepath.Join(home, "alias", "missing", "experiments")+"'")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, filepath.Join(home, "alias", "target"))
	if err != nil {
		t.Fatal(err)
	}
	run(t, home, true, []string{"XDG_CONFIG_HOME=" + config}, "promote", "example", relative)
	contains(t, run(t, home, true, nil, "info", "example"), "Status: promoted", "Location: "+filepath.Join(home, "target"))
	absent(t, filepath.Join(home, "missing"))
}

func TestPromotionCrossFilesystemFailure(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "example")
	source := filepath.Join(home, "experiments", "2026-09-14-example")
	target := filepath.Join(home, "target")
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	contains(t, run(t, home, false, []string{"HATCH_TEST_RENAME_EXDEV=1"}, "promote", "example", target), "filesystem")
	contains(t, run(t, home, true, nil, "info", "example"), "Status: active", "Location: "+source)
	data, err := os.ReadFile(filepath.Join(source, "keep.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatal("source changed")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target created: %v", err)
	}
	run(t, home, true, nil, "promote", "example", target)
}

func TestAmbiguousPromotionRecovery(t *testing.T) {
	for _, change := range []string{"both", "neither", "replacement", "symlink", "source-replacement", "intent-collision"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			resolvedHome, err := filepath.EvalSymlinks(home)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(resolvedHome, "target")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0600))
			point := "after-promotion-move"
			if change == "source-replacement" || change == "intent-collision" {
				point = "before-promotion-move"
			}
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "promote", "example", target)
			switch change {
			case "both":
				must(os.Mkdir(source, 0755))
			case "neither", "replacement", "symlink":
				must(os.Rename(target, target+"-saved"))
				if change == "replacement" {
					must(os.Mkdir(target, 0755))
				}
				if change == "symlink" {
					must(os.Symlink(target+"-saved", target))
				}
			case "source-replacement":
				must(os.Rename(source, target+"-saved"))
				must(os.Mkdir(source, 0755))
			case "intent-collision":
				must(os.Mkdir(target, 0755))
			}
			for _, args := range [][]string{{"info", "example"}, {"list"}, {"new", "blocked"}, {"status", "example", "completed"}, {"promote", "example", target + "-other"}} {
				contains(t, run(t, home, false, nil, args...), "uncertain", "mutations blocked", source, target)
			}
			run(t, home, true, nil, "--help")
			// Restore only the test's external disturbance. Successful recovery now
			// proves blocked commands retained the original durable intent and files.
			switch change {
			case "both":
				must(os.Remove(source))
			case "replacement", "symlink":
				must(os.Remove(target))
				must(os.Rename(target+"-saved", target))
			case "neither":
				must(os.Rename(target+"-saved", target))
			case "source-replacement":
				must(os.Remove(source))
				must(os.Rename(target+"-saved", source))
			case "intent-collision":
				must(os.Remove(target))
			}
			if point == "before-promotion-move" {
				contains(t, run(t, home, true, nil, "info", "example"), "Status: active")
				run(t, home, true, nil, "promote", "example", target)
			}
			contains(t, run(t, home, true, nil, "info", "example"), "Status: promoted", "Location: "+target)
			data, err := os.ReadFile(filepath.Join(target, "keep.txt"))
			if err != nil || string(data) != "keep" {
				t.Fatal("recovery changed contents")
			}
		})
	}
}

func TestInterruptedPromotion(t *testing.T) {
	for _, point := range []string{"before-promotion-move", "after-promotion-move", "during-promotion-registry", "after-promotion-registry"} {
		t.Run(point, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			target := filepath.Join(home, "target")
			out, err := invoke(home, []string{"HATCH_TEST_INTERRUPT=" + point}, "promote", "example", target)
			if err == nil || !strings.Contains(err.Error(), "86") {
				t.Fatalf("fault did not fire: %s %v", out, err)
			}
			// Recovery uses durable locations, not the latest configuration.
			config := filepath.Join(home, "config")
			writeConfig(t, config, "experiments_dir = '"+filepath.Join(home, "elsewhere")+"'")
			env := []string{"XDG_CONFIG_HOME=" + config}
			if point == "before-promotion-move" {
				contains(t, run(t, home, true, env, "info", "example"), "Status: active", "Location: "+source)
				run(t, home, true, env, "promote", "example", target)
			}
			contains(t, run(t, home, true, env, "info", "example"), "Created: 2026-09-14", "Status: promoted", "Location: "+target)
			run(t, home, true, env, "new", "other")
		})
	}
}

func TestPromotionRejectsUnsafePaths(t *testing.T) {
	for _, kind := range []string{"directory", "file", "dangling", "dangling-slash", "missing-parent", "inside-experiments", "experiments-alias", "inside-original-source", "missing-source", "source-file", "source-symlink", "empty"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			target := filepath.Join(home, "target")
			var env []string
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "directory":
				must(os.Mkdir(target, 0755))
			case "file":
				must(os.WriteFile(target, []byte("keep"), 0600))
			case "dangling", "dangling-slash":
				must(os.Symlink("missing", target))
				if kind == "dangling-slash" {
					target += "/"
				}
			case "missing-parent":
				target = filepath.Join(home, "missing", "target")
			case "inside-experiments":
				target = filepath.Join(home, "experiments", "target")
			case "experiments-alias":
				must(os.Symlink(filepath.Join(home, "experiments"), filepath.Join(home, "alias")))
				target = filepath.Join(home, "alias", "target")
			case "inside-original-source":
				config := filepath.Join(home, "config")
				writeConfig(t, config, "experiments_dir = '"+filepath.Join(home, "elsewhere")+"'")
				env = []string{"XDG_CONFIG_HOME=" + config}
				must(os.Symlink(source, filepath.Join(home, "alias")))
				target = filepath.Join(home, "alias", "target")
			case "missing-source":
				must(os.Remove(source))
			case "source-file":
				must(os.Remove(source))
				must(os.WriteFile(source, []byte("keep"), 0600))
			case "source-symlink":
				must(os.Rename(source, source+"-saved"))
				must(os.Symlink(source+"-saved", source))
			case "empty":
				target = ""
			}
			run(t, home, false, env, "promote", "example", target)
			contains(t, run(t, home, true, env, "info", "example"), "Status: active", "Location: "+source)
			if kind == "file" {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "keep" {
					t.Fatal("collision overwritten")
				}
			}
			if kind == "missing-parent" {
				if _, err := os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
					t.Fatal("parent created")
				}
			}
		})
	}
}

func TestPromoteRetainsIdentity(t *testing.T) {
	for _, status := range []string{"active", "completed", "abandoned"} {
		t.Run(status, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "example")
			run(t, home, true, nil, "status", "example", status)
			run(t, home, true, nil, "new", "newer")
			source := filepath.Join(home, "experiments", "2026-09-14-example")
			if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			resolvedHome, err := filepath.EvalSymlinks(home)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(resolvedHome, "exact-target")
			run(t, home, true, nil, "promote", "example", target)
			contains(t, run(t, home, true, nil, "info", "example"), "Name: example", "Created: 2026-09-14", "Status: promoted", "Location: "+target)
			if _, err := os.Lstat(source); !os.IsNotExist(err) {
				t.Fatalf("source remains: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(target, "keep.txt"))
			if err != nil || string(data) != "original" {
				t.Fatalf("contents: %s %v", data, err)
			}
			listed := strings.Join(strings.Fields(run(t, home, true, nil, "list")), " ")
			if listed != "NAME CREATED STATUS newer 2026-09-14 active example 2026-09-14 promoted" {
				t.Fatalf("list identity/order: %s", listed)
			}
			for _, next := range []string{"active", "completed", "abandoned", "promoted"} {
				run(t, home, false, nil, "status", "example", next)
			}
			run(t, home, false, nil, "promote", "example", target+"-again")
			contains(t, run(t, home, false, nil, "new", "example"), "already tracked")
		})
	}
}
