package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Tracked locations must remain independent: moving or trashing one project
// must never move another project's files. Missing locations remain reserved.
// Call under the registry lock, before recording intent or changing files.
func (s *store) guardIndependentLocation(location, exceptName string) error {
	projects, err := s.list()
	if err != nil {
		return err
	}
	resolved, err := resolveDirectory(location)
	if err != nil {
		return fmt.Errorf("inspect project location %s: %w", location, err)
	}
	for _, p := range projects {
		if p.Name == exceptName {
			continue
		}
		other, err := resolveDirectory(p.Location)
		if err != nil {
			return fmt.Errorf("inspect tracked experimental project %q at %s: %w", p.Name, p.Location, err)
		}
		overlap := within(resolved, other) || within(other, resolved)
		if !overlap {
			// EvalSymlinks preserves case spelling on case-insensitive volumes.
			for _, pair := range [][2]string{{resolved, other}, {other, resolved}} {
				inside, err := withinByDirectoryIdentity(pair[0], pair[1])
				if err != nil {
					return err
				}
				overlap = overlap || inside
			}
		}
		if overlap {
			return fmt.Errorf("location %s overlaps already tracked experimental project %q at %s", location, p.Name, p.Location)
		}
	}
	return nil
}

func withinByDirectoryIdentity(path, directory string) (bool, error) {
	expected, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", directory, err)
	}
	for current := path; ; current = filepath.Dir(current) {
		actual, err := os.Stat(current)
		if err == nil && os.SameFile(expected, actual) {
			return true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect %s: %w", current, err)
		}
		if current == filepath.Dir(current) {
			return false, nil
		}
	}
}
