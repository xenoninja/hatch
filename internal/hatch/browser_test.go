package hatch

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Drive the installed shell function on a real terminal, recording cwd and
// terminal settings independently of Hatch's destination output.
type browserSession struct {
	t        *testing.T
	cmd      *exec.Cmd
	terminal *os.File
	files    string
	mu       sync.Mutex
	output   strings.Builder
	changed  chan struct{}
	done     chan error
}

func startBrowser(t *testing.T, shell, home string, controls []string, direct bool) *browserSession {
	t.Helper()
	init := run(t, home, true, nil, "shell-init", shell)
	invocation := "hatch"
	if direct {
		invocation = "command hatch"
	}
	script := init + "\nstty -a > \"$HATCH_TERMINAL_FILES/before\"\nprintf 'PROMPT> '\n" + invocation + "\nresult=$?\nprintf '%s' \"$PWD\" > \"$HATCH_TERMINAL_FILES/cwd\"\nstty -a > \"$HATCH_TERMINAL_FILES/after\"\nprintf 'HATCH_TEST_DONE\\n'\nexit \"$result\"\n"
	flags := []string{"--noprofile", "--norc", "-i", "-c", script}
	if shell == "zsh" {
		flags = []string{"-f", "-i", "-c", script}
	}
	if shell == "fish" {
		script = init + "\nstty -a > \"$HATCH_TERMINAL_FILES/before\"\nprintf 'PROMPT> '\n" + invocation + "\nset -l result $status\nprintf '%s' \"$PWD\" > \"$HATCH_TERMINAL_FILES/cwd\"\nstty -a > \"$HATCH_TERMINAL_FILES/after\"\nprintf 'HATCH_TEST_DONE\\n'\nexit $result\n"
		flags = []string{"--no-config", "--interactive", "-c", script}
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Fatal(err)
	}
	s := &browserSession{t: t, files: t.TempDir(), changed: make(chan struct{}, 1), done: make(chan error, 1)}
	s.cmd = exec.Command(path, flags...)
	s.cmd.Dir = home
	s.cmd.Env = append(command(home, controls).Env, "TERM=xterm-256color", "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"), "HATCH_TERMINAL_FILES="+s.files)
	s.terminal, err = pty.StartWithSize(s.cmd, &pty.Winsize{Rows: 30, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL); s.terminal.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.terminal.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.output.Write(buf[:n])
				s.mu.Unlock()
				select {
				case s.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { s.done <- s.cmd.Wait() }()
	return s
}

func (s *browserSession) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output.String()
}

func (s *browserSession) screen() vt10x.Terminal {
	s.t.Helper()
	terminal := vt10x.New(vt10x.WithSize(120, 30))
	if _, err := terminal.Write([]byte(s.text())); err != nil {
		s.t.Fatal(err)
	}
	return terminal
}

func (s *browserSession) waitFor(value string) {
	s.t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for !strings.Contains(s.text(), value) {
		select {
		case <-s.changed:
		case <-timer.C:
			s.t.Fatalf("terminal did not show %q: %q", value, s.text())
		}
	}
}

func (s *browserSession) send(keys string) {
	s.t.Helper()
	if _, err := s.terminal.WriteString(keys); err != nil {
		s.t.Fatal(err)
	}
}

func (s *browserSession) finish(wantCwd string, success bool) string {
	s.t.Helper()
	s.waitFor("HATCH_TEST_DONE")
	select {
	case err := <-s.done:
		if (err == nil) != success {
			s.t.Fatalf("browser exit: %v; %q", err, s.text())
		}
	case <-time.After(8 * time.Second):
		s.t.Fatal("shell did not exit")
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(s.files, name))
		if err != nil {
			s.t.Fatal(err)
		}
		return string(data)
	}
	if got := read("cwd"); got != wantCwd {
		s.t.Fatalf("cwd %q, want %q; %q", got, wantCwd, s.text())
	}
	// PENDIN is a transient kernel flag: macOS sets it on tcsetattr.
	normalize := func(value string) string { return regexp.MustCompile(`-?\bpendin\b`).ReplaceAllString(value, "") }
	if before, after := normalize(read("before")), normalize(read("after")); before != after {
		s.t.Fatalf("terminal settings changed: %q -> %q", before, after)
	}
	terminal := s.screen()
	if !terminal.CursorVisible() {
		s.t.Fatal("browser left cursor hidden")
	}
	if strings.Contains(terminal.String(), "Enter: enter") || strings.Contains(terminal.String(), "> alpha") {
		s.t.Fatalf("browser was not cleared: %q", terminal.String())
	}
	return s.text()
}

func browserHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestBrowserShellSelection(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "zebra")
			run(t, home, true, nil, "new", "alpha")
			s := startBrowser(t, shell, home, nil, false)
			s.waitFor("Enter")
			out := s.text()
			contains(t, out, "2 projects", "> alpha", "active", "Esc")
			if strings.Index(out, "alpha") > strings.Index(out, "zebra") {
				t.Fatalf("not alphabetical: %q", out)
			}
			if strings.Contains(out, "2026-09-14") || strings.Contains(out, home) {
				t.Fatalf("location/date shown: %q", out)
			}
			rows := strings.Split(s.screen().String(), "\n")
			if !strings.Contains(rows[0], "PROMPT>") || strings.Contains(rows[0], "projects") || !strings.Contains(rows[1], "2 projects") {
				t.Fatalf("browser is not below prompt: %q", rows)
			}
			s.send("\r")
			s.finish(filepath.Join(home, "experiments", "2026-09-14-alpha"), true)
		})
	}
}

func TestBrowserNoninteractive(t *testing.T) {
	home := browserHome(t)
	assertPath(t, home, nil, "", "hatch help")
	contains(t, run(t, home, false, nil), "noninteractive", "hatch list")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		out, diagnostic, cwd, err := shellRun(t, shell, home, nil)
		if out != "" || cwd != home || err == nil {
			t.Fatalf("%s: %q %q %q %v", shell, out, diagnostic, cwd, err)
		}
		contains(t, diagnostic, "noninteractive", "hatch help", "hatch list")
	}
	absent(t, filepath.Join(home, ".local", "share", "hatch"), filepath.Join(home, "experiments"))
}

func TestBrowserCancellation(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		for _, key := range []string{"\x1b", "\x03"} {
			t.Run(shell+fmt.Sprintf("-%x", key), func(t *testing.T) {
				home := browserHome(t)
				run(t, home, true, nil, "new", "only-project")
				s := startBrowser(t, shell, home, nil, false)
				s.waitFor("Enter")
				// Even a single result waits for explicit confirmation.
				if strings.Contains(s.text(), "HATCH_TEST_DONE") {
					t.Fatal("selected without Enter")
				}
				s.send(key)
				out := s.finish(home, true)
				if strings.Contains(out, "hatch:") {
					t.Fatalf("cancellation diagnostic: %q", out)
				}
				contains(t, run(t, home, true, nil, "list"), "only-project", "active")
			})
		}
	}
}

// Used by scrolling tests below; names sort the same way as their numeric suffix.
func browserName(i int) string { return fmt.Sprintf("project-%02d", i) }

func TestBrowserMovementAndScrolling(t *testing.T) {
	for _, keys := range []struct{ name, down, up string }{{"arrows", "\x1b[B", "\x1b[A"}, {"control", "\x0e", "\x10"}} {
		t.Run(keys.name, func(t *testing.T) {
			home := browserHome(t)
			for i := 11; i >= 0; i-- {
				run(t, home, true, nil, "new", browserName(i))
			}
			s := startBrowser(t, "bash", home, nil, false)
			s.waitFor("Enter")
			if strings.Contains(s.text(), "project-10") {
				t.Fatalf("more than ten rows: %q", s.text())
			}
			s.send(keys.up + keys.down)
			s.waitFor("> project-01")
			s.send(strings.Repeat(keys.down, 15))
			s.waitFor("> project-11")
			s.send(keys.up + "\r")
			s.finish(filepath.Join(home, "experiments", "2026-09-14-project-10"), true)
		})
	}
}

func TestBrowserUnavailableSelection(t *testing.T) {
	for _, nonDirectory := range []bool{false, true} {
		t.Run(fmt.Sprint(nonDirectory), func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			run(t, home, true, nil, "status", "alpha", "abandoned")
			run(t, home, true, nil, "new", "beta")
			location := filepath.Join(home, "experiments", "2026-09-14-alpha")
			if err := os.Remove(location); err != nil {
				t.Fatal(err)
			}
			if nonDirectory {
				if err := os.WriteFile(location, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := startBrowser(t, "bash", home, nil, false)
			s.waitFor("Enter")
			contains(t, s.text(), "alpha  abandoned (unavailable)")
			s.send("\r")
			s.waitFor("project location unavailable")
			if strings.Contains(s.text(), "HATCH_TEST_DONE") {
				t.Fatal("unavailable selection closed browser")
			}
			s.send("\x0e\r")
			s.finish(filepath.Join(home, "experiments", "2026-09-14-beta"), true)
			contains(t, run(t, home, true, nil, "info", "alpha"), "Status: abandoned", "Location: "+location)
		})
	}
}

func runWhileBrowsing(t *testing.T, home string, success bool, controls []string, args ...string) string {
	t.Helper()
	cmd := command(home, controls, args...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if (err == nil) != success {
			t.Fatalf("%v: %s; %v", args, &output, err)
		}
	case <-time.After(4 * time.Second):
		cmd.Process.Kill()
		<-done
		t.Fatalf("command %v blocked while browser awaited input: %s", args, &output)
	}
	return output.String()
}

func TestBrowserConcurrentChanges(t *testing.T) {
	for _, change := range []string{"new", "status", "remove", "replace", "promote", "recovery"} {
		t.Run(change, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			location := filepath.Join(home, "experiments", "2026-09-14-alpha")
			s := startBrowser(t, "bash", home, nil, false)
			s.waitFor("Enter")
			switch change {
			case "new":
				runWhileBrowsing(t, home, true, nil, "new", "beta")
			case "status":
				runWhileBrowsing(t, home, true, nil, "status", "alpha", "completed")
			case "remove", "replace":
				if err := os.Remove(location); err != nil {
					t.Fatal(err)
				}
				runWhileBrowsing(t, home, true, nil, "remove", "alpha", "--force")
				if change == "replace" {
					runWhileBrowsing(t, home, true, nil, "new", "alpha")
				}
			case "promote":
				runWhileBrowsing(t, home, true, nil, "promote", "alpha", filepath.Join(home, "promoted"))
			case "recovery":
				runWhileBrowsing(t, home, false, []string{"HATCH_TEST_INTERRUPT=after-directory"}, "new", "beta")
			}
			s.send("\r")
			if change == "new" || change == "status" {
				s.finish(location, true)
			} else {
				out := s.finish(home, false)
				if change == "recovery" {
					contains(t, out, "uncertain")
				} else {
					contains(t, out, "changed")
				}
			}
		})
	}
}

func TestBrowserInvocationAndRecovery(t *testing.T) {
	for _, scenario := range []string{"direct", "absent", "empty", "configuration", "storage", "uncertain", "recovered"} {
		t.Run(scenario, func(t *testing.T) {
			home := browserHome(t)
			switch scenario {
			case "empty":
				run(t, home, true, nil, "new", "alpha")
				if err := os.Remove(filepath.Join(home, "experiments", "2026-09-14-alpha")); err != nil {
					t.Fatal(err)
				}
				run(t, home, true, nil, "remove", "alpha", "--force")
			case "configuration":
				config := filepath.Join(home, ".config", "hatch")
				if err := os.MkdirAll(config, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("invalid = ["), 0600); err != nil {
					t.Fatal(err)
				}
			case "storage":
				root := filepath.Join(home, ".local", "share", "hatch")
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "hatch.db"), []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "uncertain", "recovered":
				point := "after-directory"
				if scenario == "recovered" {
					point = "before-registry"
				}
				run(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "alpha")
			}
			s := startBrowser(t, "bash", home, nil, scenario == "direct")
			if scenario == "recovered" {
				s.waitFor("Enter")
				s.send("\r")
				s.finish(filepath.Join(home, "experiments", "2026-09-14-alpha"), true)
				contains(t, run(t, home, true, nil, "info", "alpha"), "Status: active")
				return
			}
			out := s.finish(home, scenario == "absent" || scenario == "empty")
			switch scenario {
			case "direct":
				contains(t, out, "shell integration", "shell-init bash", "shell-init zsh", "shell-init fish")
				absent(t, filepath.Join(home, ".local", "share", "hatch"), filepath.Join(home, "experiments"))
			case "absent", "empty":
				contains(t, out, "No experimental projects tracked.")
				if scenario == "absent" {
					absent(t, filepath.Join(home, ".local", "share", "hatch"), filepath.Join(home, "experiments"))
				}
			case "configuration":
				contains(t, out, "configuration")
			case "storage":
				contains(t, out, "database")
			case "uncertain":
				contains(t, out, "uncertain", "manual investigation")
			}
			if strings.Contains(out, "Enter: enter") {
				t.Fatalf("opened browser before prerequisites: %q", out)
			}
		})
	}
}

func TestBrowserLifecycleAndLiteralPaths(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home := browserHome(t)
			for _, name := range []string{"alpha", "beta", "delta", "gamma"} {
				run(t, home, true, nil, "new", name)
			}
			run(t, home, true, nil, "status", "beta", "completed")
			run(t, home, true, nil, "status", "gamma", "abandoned")
			target := filepath.Join(home, "promoted ' \" $;* [x]\nproject\n\n")
			run(t, home, true, nil, "promote", "delta", target)
			// Later configuration must not redirect any stored location.
			config := filepath.Join(home, ".config", "hatch")
			if err := os.MkdirAll(config, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("experiments_dir = '/unused-hatch-experiments'"), 0600); err != nil {
				t.Fatal(err)
			}
			s := startBrowser(t, shell, home, nil, false)
			s.waitFor("Enter")
			contains(t, s.text(), "alpha  active", "beta  completed", "delta  promoted", "gamma  abandoned")
			if strings.Contains(s.text(), "promoted '") {
				t.Fatalf("location preview: %q", s.text())
			}
			s.send("\x0e\x0e\r")
			s.finish(target, true)
			// Validation can succeed while the shell's final cd lacks permission.
			if err := os.Chmod(target, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(target, 0700) })
			s = startBrowser(t, shell, home, nil, false)
			s.waitFor("Enter")
			s.send("\x0e\x0e\r")
			contains(t, s.finish(home, false), "cd:")
		})
	}
}
