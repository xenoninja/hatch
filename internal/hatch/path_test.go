package hatch

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func assertPath(t *testing.T, home string, controls []string, want, diagnostic string, args ...string) {
	t.Helper()
	cmd := command(home, controls, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if (err == nil) != (diagnostic == "") {
		t.Fatalf("%v: stdout %q, stderr %q, error %v", args, stdout.String(), stderr.String(), err)
	}
	if stdout.String() != want {
		t.Fatalf("stdout: got %q, want %q", stdout.String(), want)
	}
	if diagnostic == "" {
		if stderr.Len() != 0 {
			t.Fatalf("unexpected stderr: %q", stderr.String())
		}
	} else {
		contains(t, stderr.String(), diagnostic)
	}
}

func TestPathUnavailableLocation(t *testing.T) {
	for _, replacement := range []string{"missing", "file"} {
		t.Run(replacement, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, true, nil, "new", "gone")
			location := filepath.Join(home, "experiments", "2026-09-14-gone")
			if err := os.Remove(location); err != nil {
				t.Fatal(err)
			}
			if replacement == "file" {
				if err := os.WriteFile(location, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			assertPath(t, home, nil, "", "project location unavailable or not a directory: "+location, "path", "gone")
			contains(t, run(t, home, true, nil, "info", "gone"), "Location: "+location)
			if replacement == "file" {
				if data, err := os.ReadFile(location); err != nil || string(data) != "keep" {
					t.Fatalf("location changed: %q %v", data, err)
				}
			} else {
				absent(t, location)
			}
		})
	}
}

func TestPathHelp(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"path", "--help"}, {"path", "-h"}} {
		contains(t, run(t, home, true, nil, args...), "path <name>", "absolute path")
	}
}

func TestPathErrors(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"path"}, {"path", "one", "two"}} {
		assertPath(t, home, nil, "", "expected", args...)
	}
	for _, name := range []string{"", "Upper", "../escape", "two--hyphens", "white space"} {
		assertPath(t, home, nil, "", "invalid name", "path", name)
	}
	assertPath(t, home, nil, "", "unknown experimental project", "path", "unknown")
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("lookup initialized home: %v %v", entries, err)
	}
	outside := t.TempDir()
	config, data := filepath.Join(outside, "config"), filepath.Join(outside, "data")
	assertPath(t, home, []string{"XDG_CONFIG_HOME=" + config, "XDG_DATA_HOME=" + data}, "", "unknown experimental project", "path", "unknown")
	absent(t, config, data)
	run(t, home, true, nil, "new", "tiny-search")
	for _, name := range []string{"tiny", "2026-09-14-tiny-search", "unknown"} {
		assertPath(t, home, nil, "", "unknown experimental project", "path", name)
	}
}

func TestPathStoredLocations(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, "config")
	env := []string{"XDG_CONFIG_HOME=" + config}
	original := filepath.Join(home, "old experiments")
	writeConfig(t, config, "experiments_dir = '"+original+"'")
	run(t, home, true, env, "new", "example")
	location := filepath.Join(original, "2026-09-14-example")
	assertPath(t, home, env, location+"\n", "", "path", "example")
	changed := filepath.Join(home, "new experiments")
	writeConfig(t, config, "experiments_dir = '"+changed+"'")
	assertPath(t, home, env, location+"\n", "", "path", "example")
	target := filepath.Join(home, "promoted project")
	run(t, home, true, env, "promote", "example", target)
	assertPath(t, home, env, target+"\n", "", "path", "example")
	absent(t, changed, location)
}

func TestPathCreationRecovery(t *testing.T) {
	for _, point := range []string{"before-directory", "after-directory", "before-registry", "during-registry", "after-registry"} {
		t.Run(point, func(t *testing.T) {
			home := t.TempDir()
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "example")
			location := filepath.Join(home, "experiments", "2026-09-14-example")
			for i := 0; i < 2; i++ {
				switch point {
				case "before-directory":
					assertPath(t, home, nil, "", "unknown experimental project", "path", "example")
					absent(t, location)
				case "after-directory":
					assertPath(t, home, nil, "", "uncertain", "path", "example")
					if info, err := os.Stat(location); err != nil || !info.IsDir() {
						t.Fatalf("recovery changed location: %v", err)
					}
				default:
					assertPath(t, home, nil, location+"\n", "", "path", "example")
				}
			}
		})
	}
}

func TestPathPromotionRecovery(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		home, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		run(t, home, true, nil, "new", "example")
		source := filepath.Join(home, "experiments", "2026-09-14-example")
		target := filepath.Join(home, "target")
		run(t, home, false, []string{"HATCH_TEST_INTERRUPT=after-promotion-move"}, "promote", "example", target)
		if ambiguous {
			if err := os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			assertPath(t, home, nil, "", "uncertain", "path", "example")
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
		}
		assertPath(t, home, nil, target+"\n", "", "path", "example")
	}
}

func TestPath(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "tiny-search")
	assertPath(t, home, nil, filepath.Join(home, "experiments", "2026-09-14-tiny-search")+"\n", "", "path", "tiny-search")
}
