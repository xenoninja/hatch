package hatch

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// A snapshot carries the registry identity independently of mutable status.
type browserCandidate struct {
	project
	sequence    int64
	unavailable bool
}

type browserMatchCategory int

const (
	browserMatchExact browserMatchCategory = iota
	browserMatchPrefix
	browserMatchSubstring
	browserMatchGapped
)

type browserMatch struct {
	browserCandidate
	positions []int
	category  browserMatchCategory
	gaps      int
}

func matchBrowserCandidates(candidates []browserCandidate, query string) []browserMatch {
	query = strings.ToLower(query)
	var matches []browserMatch
	for _, candidate := range candidates {
		match := browserMatch{browserCandidate: candidate}
		name := candidate.Name // Tracked names are lowercase ASCII.
		if query != "" {
			if start := strings.Index(name, query); start >= 0 {
				match.category = browserMatchSubstring
				if name == query {
					match.category = browserMatchExact
				} else if start == 0 {
					match.category = browserMatchPrefix
				}
				for i := range len(query) {
					match.positions = append(match.positions, start+i)
				}
			} else {
				match.category = browserMatchGapped
				// Try each starting character: the earliest match may skip
				// more characters than a later, tighter span in the same name.
				for start := range len(name) {
					if name[start] != query[0] {
						continue
					}
					positions := []int{start}
					next := start + 1
					for i := 1; i < len(query); i++ {
						offset := strings.IndexByte(name[next:], query[i])
						if offset < 0 {
							break
						}
						positions = append(positions, next+offset)
						next += offset + 1
					}
					gaps := next - start - len(query)
					if len(positions) == len(query) && (match.positions == nil || gaps < match.gaps) {
						match.positions, match.gaps = positions, gaps
					}
				}
				if match.positions == nil {
					continue
				}
			}
		}
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if query != "" {
			if a.category != b.category {
				return a.category < b.category
			}
			if a.gaps != b.gaps {
				return a.gaps < b.gaps
			}
			if len(a.Name) != len(b.Name) {
				return len(a.Name) < len(b.Name)
			}
		}
		return a.Name < b.Name
	})
	return matches
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
	rows, err := s.db.Query(`SELECT sequence,name,created_date,status,location FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []browserCandidate
	for rows.Next() {
		var c browserCandidate
		if err := rows.Scan(&c.sequence, &c.Name, &c.Date, &c.Status, &c.Location); err != nil {
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
func validateBrowserCandidate(root, experiments string, candidate browserCandidate) (available, changed bool, err error) {
	s, err := openStore(root, experiments, false)
	if err != nil {
		return false, false, err
	}
	if s == nil {
		return false, true, nil
	}
	defer s.close()
	if err := s.reconcile(); err != nil {
		return false, false, err
	}
	var sequence int64
	var location string
	err = s.db.QueryRow(`SELECT sequence,location FROM projects WHERE name=?`, candidate.Name).Scan(&sequence, &location)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (sequence != candidate.sequence || location != candidate.Location)) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	info, err := os.Stat(location)
	return err == nil && info.IsDir(), false, nil
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
	query := ""
	matches := matchBrowserCandidates(candidates, query)
	reader := bufio.NewReader(os.Stdin)
	diagnostic := ""
	if _, err := io.WriteString(os.Stderr, "\r\n"); err != nil {
		return "", err
	}
	if err := view.draw(matches, selected, query, ""); err != nil {
		return "", err
	}
	for {
		key, err := readBrowserKey(reader)
		if err != nil {
			return "", err
		}
		previousQuery := query
		refresh := false
		switch key {
		case 3, 27:
			return "", nil
		case 14:
			selected = max(0, min(selected+1, len(matches)-1))
		case 16:
			selected = max(selected-1, 0)
		case 18:
			refresh = true
			diagnostic = ""
		case 8, 127:
			if query != "" {
				_, size := utf8.DecodeLastRuneInString(query)
				query = query[:len(query)-size]
			}
		case 21:
			query = ""
		case '\r', '\n':
			if len(matches) == 0 {
				continue
			}
			available, changed, err := validateBrowserCandidate(root, experiments, matches[selected].browserCandidate)
			if err != nil {
				return "", err
			}
			if changed {
				diagnostic = "Selected project changed while browsing; review results and press Enter again"
				refresh = true
			} else if !available {
				diagnostic = "project location unavailable or not a directory; choose another project"
				matches[selected].unavailable = true
				for i := range candidates {
					if candidates[i].sequence == matches[selected].sequence {
						candidates[i].unavailable = true
					}
				}
			} else {
				return matches[selected].Location, nil
			}
		default:
			if unicode.IsPrint(key) {
				query += string(key)
			}
		}
		if refresh {
			candidates, err = loadBrowserCandidates(root, experiments)
			if err != nil {
				return "", err
			}
			if len(candidates) == 0 {
				if err := view.clear(); err != nil {
					return "", err
				}
				_, err := io.WriteString(os.Stderr, "No experimental projects tracked.\r\n")
				return "", err
			}
		}
		if refresh || query != previousQuery {
			matches = matchBrowserCandidates(candidates, query)
			selected = 0
			if query != previousQuery {
				diagnostic = ""
			}
		}
		if err := view.draw(matches, selected, query, diagnostic); err != nil {
			return "", err
		}
	}
}

func readBrowserKey(reader *bufio.Reader) (rune, error) {
	key, _, err := reader.ReadRune()
	if err != nil {
		return 0, err
	}
	if key != 27 {
		return key, nil
	}
	// A lone Escape cancels; terminal cursor keys start with Escape followed
	// immediately by CSI or SS3. Bound the wait so Escape never needs Enter.
	readNext := func() (byte, bool, error) {
		if reader.Buffered() == 0 {
			fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
			n, err := unix.Poll(fds, 50)
			if err != nil || n == 0 {
				return 0, false, err
			}
		}
		key, err := reader.ReadByte()
		return key, true, err
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

func (v *browserView) draw(matches []browserMatch, selected int, query, diagnostic string) error {
	if err := v.clear(); err != nil {
		return err
	}
	width, height, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil {
		return err
	}
	visible := min(10, len(matches), max(1, height-3))
	start := max(0, selected-visible+1)
	// Measure all matches so columns stay aligned while scrolling. Reserve
	// space for metadata before clipping long names to the terminal width.
	nameWidth, statusWidth, annotationWidth := 0, 0, 0
	for _, c := range matches {
		nameWidth = max(nameWidth, len(c.Name))
		statusWidth = max(statusWidth, len(c.Status))
		if c.unavailable {
			annotationWidth = len(" (unavailable)")
		}
	}
	nameWidth = min(nameWidth, max(1, width-1-2-2-statusWidth-2-10-annotationWidth))
	lines := []string{"Search: " + query}
	for i := start; i < start+visible; i++ {
		c := matches[i]
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		name := runewidth.Truncate(c.Name, nameWidth, "…")
		row := fmt.Sprintf("%s%-*s  %-*s  %s", prefix, nameWidth, name, statusWidth, c.Status, c.Date)
		if c.unavailable {
			row += " (unavailable)"
		}
		lines = append(lines, row)
	}
	if diagnostic != "" {
		lines = append(lines, diagnostic)
	}
	for i, line := range lines {
		// Clip by terminal columns before styling, so wide query characters
		// cannot wrap and escape sequences cannot be partially truncated.
		runes := []rune(runewidth.Truncate(line, max(0, width-1), ""))
		var positions []int
		if i >= 1 && i < 1+visible {
			match := matches[start+i-1]
			for _, position := range match.positions {
				if position >= nameWidth || (len(match.Name) > nameWidth && position == nameWidth-1) {
					break
				}
				positions = append(positions, position)
			}
		}
		var styled strings.Builder
		for column, char := range runes {
			if len(positions) > 0 && column == positions[0]+2 {
				styled.WriteString("\x1b[1;4m")
				styled.WriteRune(char)
				styled.WriteString("\x1b[0m")
				positions = positions[1:]
			} else {
				styled.WriteRune(char)
			}
		}
		lines[i] = styled.String()
	}
	v.lines = len(lines)
	_, err = io.WriteString(os.Stderr, "\x1b[?25l"+strings.Join(lines, "\r\n"))
	return err
}
