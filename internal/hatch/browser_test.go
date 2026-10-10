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

// Wait on the current visible terminal, not text from an earlier redraw.
func (s *browserSession) waitScreen(check func(vt10x.Terminal) bool) vt10x.Terminal {
	s.t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		screen := s.screen()
		if check(screen) {
			return screen
		}
		select {
		case <-s.changed:
		case <-timer.C:
			s.t.Fatalf("unexpected browser screen: %q", screen.String())
		}
	}
}

func browserRows(screen vt10x.Terminal) []string {
	var names []string
	for _, row := range strings.Split(screen.String(), "\n") {
		if !strings.HasPrefix(row, "  ") && !strings.HasPrefix(row, "> ") {
			continue
		}
		fields := strings.Fields(row)
		if len(fields) > 0 && fields[0] == ">" {
			fields = fields[1:]
		}
		if len(fields) >= 2 {
			switch fields[1] {
			case "active", "completed", "abandoned", "promoted":
				names = append(names, fields[0])
			}
		}
	}
	return names
}

func (s *browserSession) searchScreen(query, selected string, names ...string) vt10x.Terminal {
	s.t.Helper()
	return s.waitScreen(func(screen vt10x.Terminal) bool {
		queryShown := false
		for _, row := range strings.Split(screen.String(), "\n") {
			queryShown = queryShown || strings.TrimSpace(row) == "Search: "+query || (query == "" && strings.TrimSpace(row) == "Search:")
		}
		return queryShown && strings.Join(browserRows(screen), ",") == strings.Join(names, ",") &&
			(selected == "" || strings.Contains(screen.String(), "> "+selected+"  "))
	})
}

func TestBrowserSearchRanking(t *testing.T) {
	home := browserHome(t)
	for _, name := range []string{"other", "a-x-b", "a-xxxx-a-b", "za-b", "ya-b", "a-b", "dab", "cab", "ab-long", "ab-z", "ab"} {
		run(t, home, true, nil, "new", name)
	}
	s := startBrowser(t, "bash", home, nil, false)
	s.waitFor("Enter")
	s.send("AB")
	want := "ab,ab-z,ab-long,cab,dab,a-b,ya-b,za-b,a-xxxx-a-b,a-x-b"
	screen := s.waitScreen(func(screen vt10x.Terminal) bool {
		return strings.Contains(screen.String(), "Search: AB") && strings.Join(browserRows(screen), ",") == want
	})
	contains(t, screen.String(), "10 matches / 11 projects", "> ab  active")
	s.send("\r")
	s.finish(filepath.Join(home, "experiments", "2026-09-14-ab"), true)
}

func TestBrowserSearchHighlighting(t *testing.T) {
	home := browserHome(t)
	for _, name := range []string{"my-web-app", "x-mwa", "mwa-test", "mwa"} {
		run(t, home, true, nil, "new", name)
	}
	s := startBrowser(t, "bash", home, nil, false)
	s.waitFor("Enter")
	s.send("MWA")
	screen := s.waitScreen(func(screen vt10x.Terminal) bool {
		return strings.Contains(screen.String(), "Search: MWA") && len(browserRows(screen)) == 4
	})
	for name, positions := range map[string][]int{
		"mwa": {0, 1, 2}, "mwa-test": {0, 1, 2}, "x-mwa": {2, 3, 4}, "my-web-app": {0, 3, 7},
	} {
		for y, row := range strings.Split(screen.String(), "\n") {
			x := strings.Index(row, name+"  active")
			if x < 0 {
				continue
			}
			plain := screen.Cell(x+len(name)+2, y)
			for i := range len(name) {
				cell := screen.Cell(x+i, y)
				styled := cell.Mode != plain.Mode || cell.FG != plain.FG || cell.BG != plain.BG
				want := false
				for _, position := range positions {
					want = want || position == i
				}
				if styled != want {
					t.Fatalf("%s character %d highlighted=%v, want %v", name, i, styled, want)
				}
			}
		}
	}
	s.send("\x0e\x0e\x0e\r")
	s.finish(filepath.Join(home, "experiments", "2026-09-14-my-web-app"), true)
}

func TestBrowserSearchEditingAndConfirmation(t *testing.T) {
	home := browserHome(t)
	for _, name := range []string{"zebra", "beta", "alpha"} {
		run(t, home, true, nil, "new", name)
	}
	s := startBrowser(t, "bash", home, nil, false)
	s.searchScreen("", "alpha", "alpha", "beta", "zebra")
	s.send("\x0e\x0e")
	s.searchScreen("", "zebra", "alpha", "beta", "zebra")
	s.send("a")
	s.searchScreen("a", "alpha", "alpha", "beta", "zebra")
	s.send("l")
	s.searchScreen("al", "alpha", "alpha")
	// A single match must still accept more input, without navigating.
	s.send("?\x0e\x10\r")
	s.searchScreen("al?", "")
	s.send("\x7f")
	s.searchScreen("al", "alpha", "alpha")
	s.send("\x08")
	s.searchScreen("a", "alpha", "alpha", "beta", "zebra")
	s.send("\x0e")
	s.searchScreen("a", "beta", "alpha", "beta", "zebra")
	s.send("\x15")
	screen := s.searchScreen("", "alpha", "alpha", "beta", "zebra")
	contains(t, screen.String(), "Backspace", "Ctrl-U")
	s.send("\x7f\x10\r")
	s.finish(filepath.Join(home, "experiments", "2026-09-14-alpha"), true)
}

func TestBrowserSearchUsesLiteralNamesOnly(t *testing.T) {
	home := browserHome(t)
	run(t, home, true, nil, "new", "alpha")
	run(t, home, true, nil, "new", "my-web-app")
	run(t, home, true, nil, "status", "my-web-app", "completed")
	target := filepath.Join(home, "secret-target")
	run(t, home, true, nil, "promote", "alpha", target)
	marker := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	before := run(t, home, true, nil, "list")
	s := startBrowser(t, "bash", home, nil, false)
	s.waitFor("Enter")
	for _, query := range []string{"active", "completed", "promoted", "secret-target", ".", "*", "[", "?", "apw", "a p", "é"} {
		s.send("\x15" + query + "\r")
		screen := s.searchScreen(query, "")
		contains(t, screen.String(), "0 matches / 2 projects")
	}
	// Backspace removes the entire Unicode character, then the user can search.
	s.send("\x7fa")
	s.searchScreen("a", "alpha", "alpha", "my-web-app")
	s.send("\x03")
	s.finish(home, true)
	if after := run(t, home, true, nil, "list"); after != before {
		t.Fatalf("search changed tracking: %q -> %q", before, after)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "untouched" {
		t.Fatalf("search changed project files: %q, %v", contents, err)
	}
}

func TestBrowserSearchWideQueryClipping(t *testing.T) {
	home := browserHome(t)
	run(t, home, true, nil, "new", "alpha")
	s := startBrowser(t, "bash", home, nil, false)
	s.waitFor("Enter")
	s.send(strings.Repeat("界", 60))
	// At 120 columns, the eight-column label and 55 double-width characters
	// fit with a spare column. A 56th character would wrap on a real terminal.
	s.waitScreen(func(screen vt10x.Terminal) bool {
		for _, row := range strings.Split(screen.String(), "\n") {
			if strings.TrimSpace(row) == "Search: "+strings.Repeat("界", 55) {
				return strings.Contains(screen.String(), "0 matches / 1 projects")
			}
		}
		return false
	})
	// Clipping affects presentation only: all 60 query characters are editable.
	s.send(strings.Repeat("\x7f", 60) + "a")
	s.searchScreen("a", "alpha", "alpha")
	s.send("\x03")
	s.finish(home, true)
	if strings.Contains(s.screen().String(), "Search:") || strings.Contains(s.screen().String(), "界") {
		t.Fatal("query text remained after cancellation")
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
			s.send("ALP")
			s.searchScreen("ALP", "alpha", "alpha")
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
				s.send("ONLY")
				s.searchScreen("ONLY", "only-project", "only-project")
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
			visibleNames := func(start int) []string {
				var names []string
				for i := start; i < start+10; i++ {
					names = append(names, browserName(i))
				}
				return names
			}
			s.send("P")
			s.searchScreen("P", "project-00", visibleNames(0)...)
			s.send(keys.up + keys.down)
			s.searchScreen("P", "project-01", visibleNames(0)...)
			s.send(strings.Repeat(keys.down, 15))
			s.searchScreen("P", "project-11", visibleNames(2)...)
			// Editing resets both the highlight and the scrolled viewport.
			s.send("R")
			s.searchScreen("PR", "project-00", visibleNames(0)...)
			s.send(strings.Repeat(keys.down, 15))
			s.searchScreen("PR", "project-11", visibleNames(2)...)
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
			s.send("ALP")
			screen := s.searchScreen("ALP", "alpha", "alpha")
			contains(t, screen.String(), "alpha  abandoned (unavailable)")
			s.send("\r")
			s.waitFor("project location unavailable")
			if strings.Contains(s.text(), "HATCH_TEST_DONE") {
				t.Fatal("unavailable selection closed browser")
			}
			s.send("\x15B")
			s.searchScreen("B", "beta", "beta")
			s.send("\r")
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

func TestBrowserRefreshPreservesQueryAndOrdering(t *testing.T) {
	home := browserHome(t)
	for _, name := range []string{"other", "a-x-b", "cab", "ab-long"} {
		run(t, home, true, nil, "new", name)
	}
	s := startBrowser(t, "bash", home, nil, false)
	s.searchScreen("", "a-x-b", "a-x-b", "ab-long", "cab", "other")
	s.send("AB")
	s.searchScreen("AB", "ab-long", "ab-long", "cab", "a-x-b")
	runWhileBrowsing(t, home, true, nil, "new", "ab")
	runWhileBrowsing(t, home, true, nil, "status", "cab", "completed")
	// Another key redraws the captured snapshot without incorporating changes.
	s.send("\x0e")
	screen := s.searchScreen("AB", "cab", "ab-long", "cab", "a-x-b")
	contains(t, screen.String(), "3 matches / 4 projects", "cab  active")
	s.send("\x12")
	screen = s.searchScreen("AB", "ab", "ab", "ab-long", "cab", "a-x-b")
	contains(t, screen.String(), "4 matches / 5 projects", "cab  completed", "Ctrl-R")
	// Refresh releases the lock before the next key and cancellation never cd's.
	runWhileBrowsing(t, home, true, nil, "info", "ab")
	s.send("\x03")
	s.finish(home, true)
}

func TestBrowserConcurrentChanges(t *testing.T) {
	for _, change := range []string{"new", "status", "remove", "replace", "promote", "recovery"} {
		t.Run(change, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			run(t, home, true, nil, "new", "alpha-next")
			location := filepath.Join(home, "experiments", "2026-09-14-alpha")
			s := startBrowser(t, "bash", home, nil, false)
			s.searchScreen("", "alpha", "alpha", "alpha-next")
			s.send("ALP")
			s.searchScreen("ALP", "alpha", "alpha", "alpha-next")
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
			} else if change == "recovery" {
				contains(t, s.finish(home, false), "uncertain")
			} else {
				s.waitScreen(func(screen vt10x.Terminal) bool {
					return strings.Contains(screen.String(), "changed while browsing")
				})
				wantLocation := location
				if change == "remove" {
					s.searchScreen("ALP", "alpha-next", "alpha-next")
					wantLocation = filepath.Join(home, "experiments", "2026-09-14-alpha-next")
				} else {
					screen := s.searchScreen("ALP", "alpha", "alpha", "alpha-next")
					if change == "promote" {
						wantLocation = filepath.Join(home, "promoted")
						contains(t, screen.String(), "alpha  promoted")
					}
				}
				if strings.Contains(s.text(), "HATCH_TEST_DONE") {
					t.Fatal("changed selection handed off a destination before a second Enter")
				}
				runWhileBrowsing(t, home, true, nil, "info", "alpha-next")
				s.send("\r")
				s.finish(wantLocation, true)
			}
		})
	}
}

func TestBrowserRefreshLocationAndAvailability(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			s := startBrowser(t, shell, home, nil, false)
			s.searchScreen("", "alpha", "alpha")
			s.send("ALP")
			s.searchScreen("ALP", "alpha", "alpha")
			target := filepath.Join(home, "promoted ' \" $;* [x]\nproject\n\n")
			runWhileBrowsing(t, home, true, nil, "promote", "alpha", target)
			if err := os.Rename(target, target+"-missing"); err != nil {
				t.Fatal(err)
			}
			s.send("\x12")
			screen := s.waitScreen(func(screen vt10x.Terminal) bool {
				return strings.Contains(screen.String(), "alpha  promoted (unavailable)")
			})
			contains(t, screen.String(), "Search: ALP")
			if strings.Contains(screen.String(), home) || strings.Contains(screen.String(), "[x]") {
				t.Fatalf("refresh displayed a location: %q", screen.String())
			}
			s.send("\r")
			s.waitFor("project location unavailable")
			contains(t, runWhileBrowsing(t, home, true, nil, "info", "alpha"), "Status: promoted", "Location: "+target)
			if err := os.Rename(target+"-missing", target); err != nil {
				t.Fatal(err)
			}
			s.send("\x12")
			s.waitScreen(func(screen vt10x.Terminal) bool {
				return strings.Contains(screen.String(), "alpha  promoted") && !strings.Contains(screen.String(), "unavailable")
			})
			s.searchScreen("ALP", "alpha", "alpha")
			s.send("\r")
			s.finish(target, true)
		})
	}
}

func TestBrowserRefreshEmptyResults(t *testing.T) {
	for _, scenario := range []string{"no-matches", "no-matches-selection", "empty-refresh", "empty-selection"} {
		t.Run(scenario, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			if strings.HasPrefix(scenario, "no-matches") {
				run(t, home, true, nil, "new", "beta")
			}
			s := startBrowser(t, "bash", home, nil, false)
			s.waitFor("Enter")
			s.send("ALP")
			s.searchScreen("ALP", "alpha", "alpha")
			if err := os.Remove(filepath.Join(home, "experiments", "2026-09-14-alpha")); err != nil {
				t.Fatal(err)
			}
			runWhileBrowsing(t, home, true, nil, "remove", "alpha", "--force")
			key := "\x12"
			if strings.HasSuffix(scenario, "selection") {
				key = "\r"
			}
			s.send(key)
			if strings.HasPrefix(scenario, "no-matches") {
				screen := s.searchScreen("ALP", "")
				contains(t, screen.String(), "0 matches / 1 projects")
				// Enter has no effect; subsequent input must still edit the query.
				s.send("\r?")
				s.searchScreen("ALP?", "")
				runWhileBrowsing(t, home, true, nil, "info", "beta")
				s.send("\x03")
				s.finish(home, true)
			} else {
				contains(t, s.finish(home, true), "No experimental projects tracked.")
			}
		})
	}
}

func TestBrowserRefreshRecovery(t *testing.T) {
	for _, point := range []string{"after-directory", "before-registry"} {
		t.Run(point, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			s := startBrowser(t, "bash", home, nil, false)
			s.searchScreen("", "alpha", "alpha")
			s.send("A")
			s.searchScreen("A", "alpha", "alpha")
			runWhileBrowsing(t, home, false, []string{"HATCH_TEST_INTERRUPT=" + point}, "new", "beta")
			s.send("\x12")
			if point == "after-directory" {
				contains(t, s.finish(home, false), "uncertain", "manual investigation")
			} else {
				screen := s.searchScreen("A", "alpha", "alpha", "beta")
				contains(t, screen.String(), "2 matches / 2 projects")
				runWhileBrowsing(t, home, true, nil, "info", "beta")
				s.send("\x03")
				s.finish(home, true)
			}
		})
	}
}

func TestBrowserChangedSelectionCancellation(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			home := browserHome(t)
			run(t, home, true, nil, "new", "alpha")
			s := startBrowser(t, shell, home, nil, false)
			s.searchScreen("", "alpha", "alpha")
			s.send("ALP")
			s.searchScreen("ALP", "alpha", "alpha")
			runWhileBrowsing(t, home, true, nil, "promote", "alpha", filepath.Join(home, "promoted"))
			s.send("\r")
			screen := s.waitScreen(func(screen vt10x.Terminal) bool {
				return strings.Contains(screen.String(), "changed while browsing") && strings.Contains(screen.String(), "alpha  promoted")
			})
			contains(t, screen.String(), "Search: ALP", "press Enter again")
			runWhileBrowsing(t, home, true, nil, "info", "alpha")
			s.send("\x1b")
			s.finish(home, true)
		})
	}
}

func TestBrowserChangedLongNameExplainsConfirmation(t *testing.T) {
	home := browserHome(t)
	name := strings.Repeat("long-name-", 16) + "end"
	run(t, home, true, nil, "new", name)
	s := startBrowser(t, "bash", home, nil, false)
	s.waitFor("Enter")
	if err := pty.Setsize(s.terminal, &pty.Winsize{Rows: 30, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	s.send("long")
	s.waitScreen(func(screen vt10x.Terminal) bool {
		return strings.Contains(screen.String(), "Search: long")
	})
	runWhileBrowsing(t, home, true, nil, "promote", name, filepath.Join(home, "promoted"))
	s.send("\r")
	screen := s.waitScreen(func(screen vt10x.Terminal) bool {
		return strings.Contains(screen.String(), "changed while browsing") && strings.Contains(screen.String(), "press Enter again")
	})
	contains(t, screen.String(), "Search: long")
	if err := pty.Setsize(s.terminal, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	s.send("\x03")
	s.finish(home, true)
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
