package hatch

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Foundation owns the destination-side trash operation. macOS can allow the
// native move and inspection of its receipt while denying open of the trash
// parent itself. Do not require that extra access to complete or recover removal.
func syncTrashParents(source, _ string) error {
	return syncDirectory(filepath.Dir(source))
}

// Foundation selects the native per-volume trash and a collision-free name.
// AppleScriptObjC supports Foundation's output parameters without cgo or Finder
// automation. A failed call may still have moved files: reconcile evidence.
func nativeTrash(source string, _ recordTrashPlan) (string, error) {
	const script = `use framework "Foundation"
on run argv
 set sourceURL to current application's NSURL's fileURLWithPath:(item 1 of argv)
 set {ok, trashedURL, trashError} to current application's NSFileManager's defaultManager()'s trashItemAtURL:sourceURL resultingItemURL:(reference) |error|:(reference)
 if not ok then error (trashError's localizedDescription() as text)
 return trashedURL's |path|() as text
end run`
	cmd := exec.Command("/usr/bin/osascript", "-l", "AppleScript", "-e", script, "--", source)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("macOS trash: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	target := strings.TrimSuffix(string(out), "\n")
	if !filepath.IsAbs(target) || target == source {
		return "", fmt.Errorf("macOS trash returned an invalid receipt")
	}
	return target, nil
}
