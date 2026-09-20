package hatch

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func syncTrashParents(source, target string) error {
	return syncMoveParents(source, target)
}

// Linux uses the freedesktop home trash, and never copies across filesystems.
// Per-mount trash discovery is intentionally unsupported: fail rather than
// placing files in a trash that a desktop cannot safely restore.
func nativeTrash(source string, record recordTrashPlan) (string, error) {
	root, err := linuxTrashRoot(source)
	if err != nil {
		return "", fmt.Errorf("Linux trash unavailable for %s: %w", source, err)
	}
	target, err := unusedTrashTarget(root, filepath.Base(source))
	if err != nil {
		return "", err
	}
	infoPath := trashInfoPath(target)
	metadata := "[Trash Info]\nPath=" + (&url.URL{Path: source}).EscapedPath() + "\nDeletionDate=" + now().Local().Format("2006-01-02T15:04:05") + "\n"
	if err := record(target, metadata); err != nil {
		return "", err
	}
	interrupt("linux-trash-planned")
	f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	interrupt("linux-trash-metadata-created")
	_, err = f.WriteString(metadata)
	interrupt("linux-trash-metadata-written")
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := syncDirectory(filepath.Dir(infoPath)); err != nil {
		return "", err
	}
	interrupt("linux-trash-metadata-synced")
	if err := renameExclusive(source, target); err != nil {
		return "", err
	}
	interrupt("linux-trash-payload-moved")
	return target, nil
}

func linuxTrashRoot(source string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data := xdgHome("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	if err := makeDurableDirectories(data, 0700); err != nil {
		return "", err
	}
	data, err = filepath.EvalSymlinks(data)
	if err != nil {
		return "", err
	}
	sourceID, err := directoryIdentity(source)
	if err != nil {
		return "", err
	}
	dataID, err := directoryIdentity(data)
	if err != nil {
		return "", err
	}
	if sourceID.device != dataID.device {
		return "", fmt.Errorf("home trash %s is on another filesystem; per-mount trash is unsupported (no copy/delete fallback)", data)
	}
	root := filepath.Join(data, "Trash")
	relative, err := filepath.Rel(source, root)
	if err != nil {
		return "", err
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))) {
		return "", fmt.Errorf("trash %s is inside source %s", root, source)
	}
	for _, path := range []string{root, filepath.Join(root, "files"), filepath.Join(root, "info")} {
		if err := privateTrashDirectory(path); err != nil {
			return "", err
		}
		id, err := directoryIdentity(path)
		if err != nil {
			return "", err
		}
		if id.device != sourceID.device {
			return "", fmt.Errorf("trash %s is on another filesystem", path)
		}
	}
	return root, nil
}

func privateTrashDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := checkTrashDirectory(path); err != nil {
		return err
	}
	if err := syncDirectory(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func unusedTrashTarget(root, name string) (string, error) {
	for {
		// Keep names short even when the original basename reaches NAME_MAX.
		// Original location is preserved in the metadata, not inferred from this name.
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		if len(name) > 100 {
			name = name[:100]
		}
		target := filepath.Join(root, "files", name+"-"+hex.EncodeToString(random[:]))
		available := true
		for _, path := range []string{target, trashInfoPath(target)} {
			_, err := os.Lstat(path)
			if err == nil {
				available = false
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if available {
			return target, nil
		}
	}
}
