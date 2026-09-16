package hatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hatch-tests-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "hatch")
	cmd := exec.Command("go", "build", "-tags=hatchtest", "-o", binary, "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func command(home string, controls []string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "HOME=") && !strings.HasPrefix(e, "TZ=") && !strings.HasPrefix(e, "HATCH_TEST_") && !strings.HasPrefix(e, "XDG_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "TZ=America/Los_Angeles", "HATCH_TEST_TIME=2026-09-15T01:30:00Z")
	cmd.Env = append(cmd.Env, controls...)
	return cmd
}

func invoke(home string, controls []string, args ...string) (string, error) {
	out, err := command(home, controls, args...).CombinedOutput()
	return string(out), err
}
func run(t *testing.T, home string, success bool, controls []string, args ...string) string {
	t.Helper()
	out, err := invoke(home, controls, args...)
	if (err == nil) != success {
		t.Fatalf("%v: output %s, error %v", args, out, err)
	}
	return out
}
func contains(t *testing.T, out string, values ...string) {
	t.Helper()
	for _, v := range values {
		if !strings.Contains(out, v) {
			t.Fatalf("%q missing %q", out, v)
		}
	}
}
func TestCreateAndInspect(t *testing.T) {
	home := t.TempDir()
	run(t, home, true, nil, "new", "project-alpha")
	location := filepath.Join(home, "experiments", "2026-09-14-project-alpha")
	out := run(t, home, true, []string{"HATCH_TEST_TIME=2027-01-01T12:00:00Z"}, "info", "project-alpha")
	contains(t, out, "project-alpha", "2026-09-14", "active", location)
	entries, err := os.ReadDir(location)
	if err != nil || len(entries) != 0 {
		t.Fatalf("project contents: %v %v", entries, err)
	}
}
