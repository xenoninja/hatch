package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The test build can inject a kernel move failure at this filesystem boundary.
var moveDirectory = renameExclusive

func (s *store) completePromotion(name, source, target string) error {
	// Sync both rename entries before committing the new registry location.
	if err := syncMoveParents(source, target); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE projects SET status='promoted',location=? WHERE name=? AND location=? AND status IN ('active','completed','abandoned')`, target, name, source)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("promotion registry identity changed for %q: %s -> %s; pending evidence preserved", name, source, target)
	}
	if _, err := tx.Exec(`DELETE FROM pending_promotion WHERE singleton=1`); err != nil {
		return err
	}
	interrupt("during-promotion-registry")
	return tx.Commit()
}

func (s *store) reconcilePromotion() error {
	var name, source, target string
	var expected directoryID
	err := s.db.QueryRow(`SELECT name,source,target,device,inode FROM pending_promotion WHERE singleton=1`).Scan(&name, &source, &target, &expected.device, &expected.inode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sourceID, sourceErr := directoryIdentity(source)
	targetID, targetErr := directoryIdentity(target)
	if sourceErr == nil && sourceID == expected && errors.Is(targetErr, os.ErrNotExist) {
		_, err := s.db.Exec(`DELETE FROM pending_promotion WHERE singleton=1`)
		return err
	}
	if errors.Is(sourceErr, os.ErrNotExist) && targetErr == nil && targetID == expected {
		return s.completePromotion(name, source, target)
	}
	return fmt.Errorf("uncertain interrupted promotion of %q: %s -> %s; files preserved, mutations blocked (pending evidence in registry); manual investigation required", name, source, target)
}

// Resolve existing ancestors even when the configured experiments directory is
// absent. Lexical prefix checks alone would allow symlink aliases to bypass it.
func resolveDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, linkErr := os.Lstat(path); !errors.Is(linkErr, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = resolveDirectory(parent)
	return filepath.Join(resolved, filepath.Base(path)), err
}

func within(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *store) promotionTarget(source, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("promotion target must not be empty")
	}
	// Preserve meaningful components such as symlink/.. until the filesystem
	// resolves the parent. Abs, Join, and Dir would clean them too early.
	if !filepath.IsAbs(target) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		target = cwd + string(filepath.Separator) + target
	}
	// A trailing separator must not hide a dangling final symlink from Lstat.
	target = strings.TrimRight(target, string(filepath.Separator))
	if target == "" {
		target = string(filepath.Separator)
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("destination already exists: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	rawParent, finalName := filepath.Split(target)
	parent, err := filepath.EvalSymlinks(rawParent)
	if err != nil {
		return "", fmt.Errorf("promotion requires an existing destination parent: %w", err)
	}
	if _, err := directoryIdentity(parent); err != nil {
		return "", err
	}
	target = filepath.Join(parent, finalName)
	for _, directory := range []string{s.experiments, source} {
		resolved, err := resolveDirectory(directory)
		if err != nil {
			return "", err
		}
		inside := within(target, resolved)
		// Identity comparisons also catch case aliases on case-insensitive volumes
		// (EvalSymlinks does not canonicalize the spelling of directory entries).
		forbidden, err := directoryIdentity(resolved)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil {
			for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
				identity, err := directoryIdentity(ancestor)
				if err != nil {
					return "", err
				}
				if identity == forbidden {
					inside = true
					break
				}
				if ancestor == filepath.Dir(ancestor) {
					break
				}
			}
		}
		if inside {
			return "", fmt.Errorf("promotion destination %s must be outside %s", target, resolved)
		}
	}
	if err := s.guardIndependentLocation(target, ""); err != nil {
		return "", err
	}
	return target, nil
}

func (s *store) promote(name, target string) (project, error) {
	p, err := s.info(name)
	if err != nil {
		return p, err
	}
	if err := guardUnpromoted(p); err != nil {
		return p, err
	}
	if err := s.guardIndependentLocation(p.Location, name); err != nil {
		return p, err
	}
	identity, err := directoryIdentity(p.Location)
	if err != nil {
		return p, fmt.Errorf("promotion source %s: %w", p.Location, err)
	}
	target, err = s.promotionTarget(p.Location, target)
	if err != nil {
		return p, err
	}
	parentID, err := directoryIdentity(filepath.Dir(target))
	if err != nil {
		return p, err
	}
	if identity.device != parentID.device {
		return p, fmt.Errorf("cross-filesystem promotion is not supported: %s -> %s", p.Location, target)
	}
	if _, err := s.db.Exec(`INSERT INTO pending_promotion(singleton,name,source,target,device,inode) VALUES(1,?,?,?,?,?)`, name, p.Location, target, identity.device, identity.inode); err != nil {
		return p, err
	}
	interrupt("before-promotion-move")
	if err := moveDirectory(p.Location, target); err != nil {
		// Clear intent only when recovery can prove the original directory remains
		// untouched and the destination is absent. Otherwise retain the evidence.
		if recoveryErr := s.reconcilePromotion(); recoveryErr != nil {
			return p, fmt.Errorf("promotion move: %w; %v", err, recoveryErr)
		}
		return p, fmt.Errorf("promotion move %s -> %s (same filesystem required): %w", p.Location, target, err)
	}
	interrupt("after-promotion-move")
	if err := s.completePromotion(name, p.Location, target); err != nil {
		return p, err
	}
	interrupt("after-promotion-registry")
	p.Status, p.Location = "promoted", target
	return p, nil
}
