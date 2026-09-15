package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"text/tabwriter"
	"time"
)

var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var now = time.Now
var interrupt = func(string) {}

const help = `Hatch manages experimental projects.

Usage:
  hatch new <name>              Create an active experimental project
  hatch info <name>             Show persisted project information
  hatch list                    List name, creation date, and status, newest first
  hatch status <name> <status>  Reclassify without moving or changing files
  hatch promote <name> <target-path>  Move to an exact external destination
  hatch remove <name> [--force]  Trash files and end tracking
  hatch help                   Show this help

Statuses: active (ongoing), completed (finished), abandoned (set aside).
Switch freely among these statuses; repeating the current status succeeds.
The promoted status is terminal and can only be set through promotion;
it cannot be assigned or changed with status.
Status preserves name, creation date, location, contents, and list order.
Promote accepts active, completed, or abandoned projects and retains identity.
Target-path is the exact final location (relative to the working directory or
absolute), not a containing directory. Its parent must exist and the target
must be absent. Symlink aliases are resolved; the destination must be outside
both the current experiments directory and the source project.
Promotion never merges, overwrites, creates parents, or copies across filesystems.
Cross-filesystem moves are unsupported; use a destination on the same filesystem.
Interrupted promotions recover on the next registry command; ambiguous states
preserve files and evidence, report both paths, and block mutations.
Remove rejects promoted projects, even when their files are missing.
Missing files allow record-only removal. Both forms require interactive consent
or --force (confirmation only); force never bypasses lifecycle or safety checks.
Removal uses native macOS trash or Linux freedesktop home trash, never permanent
deletion. Linux requires a safe home trash on the source filesystem; per-mount
trash and cross-filesystem copy/delete fallback are unsupported.
Trash failures retain tracking. Successful removal releases the name, but new
still refuses an existing dated destination. No restore command is provided.
Interrupted removals recover when evidence proves the outcome; ambiguous states
retain tracking and pending evidence and block registry commands.

Names: lowercase ASCII letters or digits separated by single hyphens.
Projects: ~/experiments/YYYY-MM-DD-<name> by default.
Config: $XDG_CONFIG_HOME/hatch/config.toml (default ~/.config/hatch/config.toml).
  experiments_dir = "~/experiments" (absolute path or leading ~/).
Registry: $XDG_DATA_HOME/hatch/hatch.db (default ~/.local/share/hatch/hatch.db).
Empty or relative XDG homes use defaults. Missing config uses defaults;
invalid config is an error. Changes affect new projects only; no files move.
List uses recorded creation order (latest first, including same-date ties).
Unavailable locations produce warnings; records and statuses are preserved.
An empty list succeeds with "No experimental projects tracked.".
Storage is created lazily; help and fresh info/list do not initialize it.
`

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hatch:", err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		fmt.Print(help)
		return nil
	}
	if len(args) == 2 && (args[0] == "new" || args[0] == "info" || args[0] == "list" || args[0] == "status" || args[0] == "promote" || args[0] == "remove") && (args[1] == "--help" || args[1] == "-h") {
		fmt.Print(help)
		return nil
	}
	listing := len(args) == 1 && args[0] == "list"
	changingStatus := len(args) == 3 && args[0] == "status"
	promoting := len(args) == 3 && args[0] == "promote"
	force := false
	if args[0] == "remove" && len(args) == 3 {
		if args[1] == "--force" {
			args = []string{"remove", args[2]}
			force = true
		} else if args[2] == "--force" {
			args = args[:2]
			force = true
		}
	}
	removing := len(args) == 2 && args[0] == "remove"
	if !listing && !changingStatus && !promoting && !removing && (len(args) != 2 || (args[0] != "new" && args[0] != "info")) {
		return fmt.Errorf("expected new <name>, info <name>, list, status <name> <status>, promote <name> <target-path>, or remove <name> [--force]; use hatch --help")
	}
	var name string
	if !listing {
		name = args[1]
		if !validName.MatchString(name) {
			return fmt.Errorf("invalid name %q: use lowercase ASCII letters or digits separated by single hyphens", name)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return err
	}
	experiments, err := experimentsDirectory(home)
	if err != nil {
		return err
	}
	root := filepath.Join(xdgHome("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "hatch")
	store, err := openStore(root, experiments, args[0] == "new")
	if err != nil {
		return err
	}
	if store == nil {
		if listing {
			return printList(nil)
		}
		return fmt.Errorf("unknown experimental project %q", name)
	}
	defer store.close()
	if err := store.reconcile(); err != nil {
		return err
	}
	if listing {
		projects, err := store.list()
		if err != nil {
			return err
		}
		return printList(projects)
	}
	if removing {
		return store.remove(name, force)
	}
	var p project
	if args[0] == "new" {
		p, err = store.create(name, now().In(time.Local).Format("2006-01-02"))
	} else if promoting {
		p, err = store.promote(name, args[2])
	} else if changingStatus {
		p, err = store.changeStatus(name, args[2])
	} else {
		p, err = store.info(name)
	}
	if err != nil {
		return err
	}
	fmt.Printf("Name: %s\nCreated: %s\nStatus: %s\nLocation: %s\n", p.Name, p.Date, p.Status, p.Location)
	warnLocation(p)
	return nil
}

func printList(projects []project) error {
	if len(projects) == 0 {
		_, err := fmt.Fprintln(os.Stdout, "No experimental projects tracked.")
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCREATED\tSTATUS")
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Date, p.Status)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, p := range projects {
		warnLocation(p)
	}
	return nil
}

func warnLocation(p project) {
	if info, err := os.Stat(p.Location); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "warning: project location unavailable or not a directory: %s\n", p.Location)
	}
}
