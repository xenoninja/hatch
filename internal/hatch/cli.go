package hatch

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

const help = `Hatch — a home for your next experiment.

Commands:
  hatch new <name>                    Create an empty project folder
  hatch list                         List projects, newest first
  hatch info <name>                   Show a project's details and location
  hatch status <name> <status>        Update progress without changing files
  hatch promote <name> <target-path>  Move a project out of experiments
  hatch remove <name> [--force]       Move to trash and stop tracking
  hatch help                         Show this help

Examples:
  hatch new tiny-search
  hatch status tiny-search completed
  hatch promote tiny-search ~/work/tiny-search

Statuses:
  active      In progress (default for new projects)
  completed   Finished
  abandoned   Set aside

  Switch freely between these. Promoted projects stay listed, but cannot
  be changed or removed through Hatch.

More details: https://github.com/xenoninja/hatch/blob/main/docs/reference.md
`

// Execute runs a Hatch command with the given command-line arguments.
func Execute(args []string) error {
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
