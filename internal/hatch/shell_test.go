package hatch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellCommands(t *testing.T) {
	home := t.TempDir()
	for _, shell := range []string{"bash", "zsh", "fish"} {
		out := run(t, home, true, nil, "shell-init", shell)
		if out == "" {
			t.Fatal("empty initialization")
		}
	}
	for _, args := range [][]string{{"shell-init"}, {"shell-init", "sh"}, {"shell-init", "bash", "extra"}, {"cd"}, {"cd", "one", "two"}} {
		assertPath(t, home, nil, "", "usage:", args...)
	}
	assertPath(t, home, nil, "", "shell integration", "cd", "example")
	for _, name := range []string{"", "Upper", "../escape"} {
		assertPath(t, home, nil, "", "invalid name", "cd", name)
	}
	for _, sub := range []string{"cd", "shell-init"} {
		for _, flag := range []string{"--help", "-h"} {
			contains(t, run(t, home, true, nil, sub, flag), "hatch cd <name>", "shell-init bash", "shell-init zsh", "shell-init fish")
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("initialized home: %v %v", entries, err)
	}
}

// Observe both the command and the working directory in the same real shell.
func shellRun(t *testing.T, shell, home string, controls []string, args ...string) (string, string, string, error) {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Fatalf("required test shell %s: %v", shell, err)
	}
	init := run(t, home, true, nil, "shell-init", shell)
	script := init + "\nhatch \"$@\"\nresult=$?\nprintf '%s' \"$PWD\" > \"$HATCH_CWD_FILE\"\nexit \"$result\"\n"
	flags := []string{"--noprofile", "--norc", "-c", script, "hatch-test"}
	if shell == "zsh" {
		flags = []string{"-f", "-c", script, "hatch-test"}
	}
	if shell == "fish" {
		script = init + "\nhatch $argv\nset -l result $status\nprintf '%s' \"$PWD\" > \"$HATCH_CWD_FILE\"\nexit $result\n"
		flags = []string{"--no-config", "-c", script}
	}
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	cmd := exec.Command(shellPath, append(flags, args...)...)
	cmd.Env = command(home, controls).Env
	cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"), "HATCH_CWD_FILE="+cwdFile)
	cmd.Dir = home
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	cwd, readErr := os.ReadFile(cwdFile)
	if readErr != nil {
		t.Fatalf("shell did not record cwd: %v; stderr %s", readErr, &stderr)
	}
	return stdout.String(), stderr.String(), string(cwd), err
}

func TestShellNavigation(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			run(t, home, true, nil, "new", "example")
			want := filepath.Join(home, "experiments", "2026-09-14-example")
			out, diagnostic, cwd, err := shellRun(t, shell, home, nil, "cd", "example")
			if err != nil || out != "" || diagnostic != "" || cwd != want {
				t.Fatalf("stdout %q stderr %q cwd %q err %v", out, diagnostic, cwd, err)
			}
		})
	}
}

func assertNavigation(t *testing.T, shell, home string, controls []string, want, diagnostic string, args ...string) {
	t.Helper()
	out, stderr, cwd, err := shellRun(t, shell, home, controls, args...)
	if cwd != want || out != "" || (err == nil) != (diagnostic == "") {
		t.Fatalf("%v: stdout %q stderr %q cwd %q (want %q) error %v", args, out, stderr, cwd, want, err)
	}
	if diagnostic == "" && stderr != "" {
		t.Fatalf("unexpected stderr %q", stderr)
	}
	if diagnostic != "" {
		contains(t, stderr, diagnostic)
	}
}

func TestShellNavigationContracts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(home, "config")
			env := []string{"XDG_CONFIG_HOME=" + config}
			original := filepath.Join(home, "old experiments")
			writeConfig(t, config, "experiments_dir = '"+original+"'")
			run(t, home, true, env, "new", "example")
			writeConfig(t, config, "experiments_dir = '"+filepath.Join(home, "new experiments")+"'")
			location := filepath.Join(original, "2026-09-14-example")
			for _, status := range []string{"active", "completed", "abandoned"} {
				run(t, home, true, env, "status", "example", status)
				assertNavigation(t, shell, home, env, location, "", "cd", "example")
			}
			// Forward a path with spaces, quotes, glob characters and embedded/trailing newlines.
			target := filepath.Join(home, "promoted ' \" $;* [x]\nproject\n")
			out, stderr, cwd, err := shellRun(t, shell, home, env, "promote", "example", target)
			if err != nil || stderr != "" || cwd != home {
				t.Fatalf("forward promotion: %q %q %q %v", out, stderr, cwd, err)
			}
			contains(t, out, "Location: "+target)
			assertNavigation(t, shell, home, env, target, "", "cd", "example")
			for _, args := range [][]string{nil, {"path", "example"}, {"info", "example"}, {"info", "unknown"}, {"new", "two words"}} {
				cmd := command(home, env, args...)
				var expectedOut, expectedErr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &expectedOut, &expectedErr
				directErr := cmd.Run()
				out, stderr, cwd, err := shellRun(t, shell, home, env, args...)
				if out != expectedOut.String() || stderr != expectedErr.String() || cwd != home || exitCode(err) != exitCode(directErr) {
					t.Fatalf("forward %v: stdout %q stderr %q cwd %q error %v; direct %q %q %v", args, out, stderr, cwd, err, &expectedOut, &expectedErr, directErr)
				}
			}
			for _, name := range []string{"unknown", "exam", "2026-09-14-example", "Upper", "../example", "", "two words"} {
				diagnostic := "unknown experimental project"
				if strings.ContainsAny(name, "U/. ") || name == "" {
					diagnostic = "invalid name"
				}
				assertNavigation(t, shell, home, env, home, diagnostic, "cd", name)
			}
			for _, args := range [][]string{{"cd"}, {"cd", "example", "extra"}, {"cd", "--help", "extra"}} {
				assertNavigation(t, shell, home, env, home, "usage:", args...)
			}
			for _, flag := range []string{"--help", "-h"} {
				out, stderr, cwd, err := shellRun(t, shell, home, env, "cd", flag)
				if err != nil || stderr != "" || cwd != home {
					t.Fatalf("help: %q %q %v", stderr, cwd, err)
				}
				contains(t, out, "hatch cd <name>")
			}
			// Stat succeeds without search permission on the final directory, but cd fails.
			if err := os.Chmod(target, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(target, 0700) })
			assertPath(t, home, env, target+"\n", "", "path", "example")
			assertNavigation(t, shell, home, env, home, "cd:", "cd", "example")
			if err := os.Chmod(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			assertNavigation(t, shell, home, env, home, "unavailable", "cd", "example")
			if err := os.WriteFile(target, nil, 0600); err != nil {
				t.Fatal(err)
			}
			assertNavigation(t, shell, home, env, home, "unavailable", "cd", "example")
		})
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	return -1
}

func TestShellUncertainRecovery(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			run(t, home, false, []string{"HATCH_TEST_INTERRUPT=after-directory"}, "new", "example")
			assertNavigation(t, shell, home, nil, home, "uncertain", "cd", "example")
		})
	}
}
