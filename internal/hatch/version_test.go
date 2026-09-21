package hatch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	for _, tc := range []struct {
		name, release, module, want string
	}{
		{"release overrides metadata", "v1.2.3", "v0.9.0", "v1.2.3"},
		{"module installation", "", "v1.2.3", "v1.2.3"},
		{"prerelease", "v1.2.3-rc.1", "", "v1.2.3-rc.1"},
		{"source build", "", "(devel)", "dev"},
		{"missing module version", "", "", "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.module}}
			if got := buildVersion(tc.release, info); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := buildVersion("", nil); got != "dev" {
		t.Fatalf("missing build info: %q", got)
	}
	local := &debug.BuildInfo{
		Main:     debug.Module{Version: "v1.2.3+dirty"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
	}
	if got := buildVersion("", local); got != "dev" {
		t.Fatalf("local checkout must not report a release: %q", got)
	}
	if got := buildVersion("v1.2.3", local); got != "v1.2.3" {
		t.Fatalf("embedded release must override local metadata: %q", got)
	}
}

func TestHelpAndVersion(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".config", "hatch")
	if err := os.MkdirAll(config, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("invalid = ["), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"path", "--help"}} {
		assertPath(t, home, nil, "hatch dev\n\n"+help, "", args...)
	}
	for _, args := range [][]string{{"--version"}, {"version"}} {
		assertPath(t, home, nil, "hatch dev\n", "", args...)
	}
	absent(t, filepath.Join(home, ".local"), filepath.Join(home, "experiments"))
}

func TestUnknownCommand(t *testing.T) {
	home := t.TempDir()
	cmd := command(home, nil, "ljldsjf")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("expected exit 1, got %v", err)
	}
	want := "hatch: unknown command \"ljldsjf\"\nRun 'hatch help' for available commands and examples.\n"
	if stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("command initialized home: %v %v", entries, err)
	}
}
