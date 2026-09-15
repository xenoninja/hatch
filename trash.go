package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// recordTrashPlan commits the Linux destination and exact restoration metadata
// before either can be created. macOS instead supplies a receipt after Foundation
// returns; an empty metadata body preserves that platform's recovery behavior.
type recordTrashPlan func(target, info string) error

func trashInfoPath(target string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(target)), "info", filepath.Base(target)+".trashinfo")
}

// Used both when selecting Linux trash and when verifying a persisted receipt.
// Recovery must not follow a trash directory replaced with a symlink.
func checkTrashDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe trash directory %s: must be a non-symlink directory owned by the current user with mode 0700", path)
	}
	return nil
}

func uncertainRemoval(name, source, target, info string) string {
	metadata := ""
	if info != "" {
		metadata = fmt.Sprintf(", trash metadata %q", trashInfoPath(target))
	}
	return fmt.Sprintf("uncertain interrupted removal of %q: source %s, trash receipt %q%s; tracking and pending evidence retained, mutations blocked; manual investigation required", name, source, target, metadata)
}

// A payload alone is not a restorable Linux trash entry. Check the exact durable
// metadata, rejecting symlinks and partial writes, before releasing tracking.
func syncTrashInfo(target, expected string) error {
	if expected == "" {
		return nil
	}
	path := trashInfoPath(target)
	for _, directory := range []string{filepath.Dir(filepath.Dir(target)), filepath.Dir(target), filepath.Dir(path)} {
		if err := checkTrashDirectory(directory); err != nil {
			return err
		}
	}
	stat, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("trash metadata is not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(len(expected))+1))
	if err != nil {
		return err
	}
	if string(data) != expected {
		return fmt.Errorf("trash metadata does not match durable receipt: %s", path)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
