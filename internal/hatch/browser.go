package hatch

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// A snapshot carries the registry identity independently of mutable status.
type browserCandidate struct {
	project
	sequence    int64
	unavailable bool
}

func browse() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return fmt.Errorf("noninteractive bare hatch requires a terminal; use hatch help or hatch list")
	}
	if os.Getenv("HATCH_SHELL_NAVIGATION") != "1" {
		return fmt.Errorf("bare hatch requires shell integration\n%s", navigationHelp)
	}
	root, experiments, err := storageDirectories()
	if err != nil {
		return err
	}
	candidates, err := loadBrowserCandidates(root, experiments)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(os.Stderr, "No experimental projects tracked.")
		return err
	}
	destination, err := runBrowser(root, experiments, candidates)
	if err != nil || destination == "" {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, destination)
	return err
}

func loadBrowserCandidates(root, experiments string) ([]browserCandidate, error) {
	s, err := openStore(root, experiments, false)
	if err != nil || s == nil {
		return nil, err
	}
	defer s.close()
	if err := s.reconcile(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT sequence,name,status,location FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []browserCandidate
	for rows.Next() {
		var c browserCandidate
		if err := rows.Scan(&c.sequence, &c.Name, &c.Status, &c.Location); err != nil {
			return nil, err
		}
		info, err := os.Stat(c.Location)
		c.unavailable = err != nil || !info.IsDir()
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// Selection shares recovery with every registry command and compares both the
// creation sequence and location under the lock. Status changes are harmless.
func validateBrowserCandidate(root, experiments string, candidate browserCandidate) (bool, error) {
	s, err := openStore(root, experiments, false)
	if err != nil {
		return false, err
	}
	if s == nil {
		return false, fmt.Errorf("experimental project %q changed while browsing; run hatch again", candidate.Name)
	}
	defer s.close()
	if err := s.reconcile(); err != nil {
		return false, err
	}
	var sequence int64
	var location string
	err = s.db.QueryRow(`SELECT sequence,location FROM projects WHERE name=?`, candidate.Name).Scan(&sequence, &location)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (sequence != candidate.sequence || location != candidate.Location)) {
		return false, fmt.Errorf("experimental project %q changed while browsing; run hatch again", candidate.Name)
	}
	if err != nil {
		return false, err
	}
	info, err := os.Stat(location)
	return err == nil && info.IsDir(), nil
}

func runBrowser(root, experiments string, candidates []browserCandidate) (destination string, err error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	view := browserView{}
	defer func() {
		clearErr := view.clear()
		restoreErr := term.Restore(fd, state)
		if err == nil {
			err = clearErr
		}
		if err == nil {
			err = restoreErr
		}
	}()
	selected := 0
	diagnostic := ""
	if _, err := io.WriteString(os.Stderr, "\r\n"); err != nil {
		return "", err
	}
	if err := view.draw(candidates, selected, ""); err != nil {
		return "", err
	}
	for {
		key, err := readBrowserKey()
		if err != nil {
			return "", err
		}
		switch key {
		case 3, 27:
			return "", nil
		case 14:
			selected = min(selected+1, len(candidates)-1)
		case 16:
			selected = max(selected-1, 0)
		case '\r', '\n':
			available, err := validateBrowserCandidate(root, experiments, candidates[selected])
			if err != nil {
				return "", err
			}
			if !available {
				diagnostic = "project location unavailable or not a directory; choose another project"
				candidates[selected].unavailable = true
			} else {
				return candidates[selected].Location, nil
			}
		}
		if err := view.draw(candidates, selected, diagnostic); err != nil {
			return "", err
		}
	}
}

func readBrowserKey() (byte, error) {
	var key [1]byte
	if _, err := io.ReadFull(os.Stdin, key[:]); err != nil {
		return 0, err
	}
	if key[0] != 27 {
		return key[0], nil
	}
	// A lone Escape cancels; terminal cursor keys start with Escape followed
	// immediately by CSI or SS3. Bound the wait so Escape never needs Enter.
	readNext := func() (byte, bool, error) {
		fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 50)
		if err != nil || n == 0 {
			return 0, false, err
		}
		_, err = io.ReadFull(os.Stdin, key[:])
		return key[0], true, err
	}
	prefix, ok, err := readNext()
	if err != nil {
		return 0, err
	}
	if !ok || (prefix != '[' && prefix != 'O') {
		return 27, nil
	}
	for {
		code, ok, err := readNext()
		if err != nil {
			return 0, err
		}
		if !ok {
			return 27, nil
		}
		if code >= 0x40 && code <= 0x7e {
			switch code {
			case 'A':
				return 16, nil
			case 'B':
				return 14, nil
			}
			return 0, nil
		}
	}
}

// The browser occupies a bounded area below the prompt, without an alternate
// screen or location preview. Each redraw starts at that area's first line.
type browserView struct{ lines int }

func (v *browserView) clear() error {
	if v.lines == 0 {
		return nil
	}
	var out strings.Builder
	if v.lines > 1 {
		fmt.Fprintf(&out, "\x1b[%dA", v.lines-1)
	}
	out.WriteString("\r\x1b[J\x1b[?25h")
	v.lines = 0
	_, err := io.WriteString(os.Stderr, out.String())
	return err
}

func (v *browserView) draw(candidates []browserCandidate, selected int, diagnostic string) error {
	if err := v.clear(); err != nil {
		return err
	}
	width, height, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil {
		return err
	}
	visible := min(10, len(candidates), max(1, height-4))
	start := max(0, selected-visible+1)
	lines := []string{fmt.Sprintf("%d projects", len(candidates))}
	for i := start; i < start+visible; i++ {
		c := candidates[i]
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		row := prefix + c.Name + "  " + c.Status
		if c.unavailable {
			row += " (unavailable)"
		}
		lines = append(lines, row)
	}
	lines = append(lines, "Up/Down · Ctrl-P/Ctrl-N · Enter: enter · Esc/Ctrl-C: cancel", diagnostic)
	for i, line := range lines {
		// Names and statuses are ASCII; diagnostics are fixed text.
		runes := []rune(line)
		lines[i] = string(runes[:min(len(runes), max(0, width-1))])
	}
	v.lines = len(lines)
	_, err = io.WriteString(os.Stderr, "\x1b[?25l"+strings.Join(lines, "\r\n"))
	return err
}
