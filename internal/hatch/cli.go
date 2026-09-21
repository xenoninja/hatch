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
  hatch list                          List projects, newest first
  hatch info <name>                   Show a project's details and location
  hatch path <name>                   Print only the project's absolute path
  hatch cd <name>                     Enter a project (requires shell integration)
  hatch shell-init <bash|zsh|fish>    Print shell initialization code
  hatch status <name> <status>        Update progress without changing files
  hatch promote <name> <target-path>  Move a project out of experiments
  hatch remove <name> [--force]       Move to trash and stop tracking
  hatch help                          Show this help
  hatch --version                     Show version (also: hatch version)

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

Path lookup fails with an error on stderr and no stdout if the name is
unknown, the location is unavailable, or recovery is uncertain.

More details: https://github.com/xenoninja/hatch/blob/main/docs/reference.md
`

// Execute runs a Hatch command with the given command-line arguments.
func Execute(args []string) error {
	if len(args) == 0 {
		args = []string{"help"}
	}
	var usage string
	var wantArgs int
	var listing, changingStatus, promoting, removing, force bool
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) != 1 {
			return fmt.Errorf("expected hatch %s; use hatch --help", args[0])
		}
		fmt.Printf("hatch %s\n\n%s", version(), help)
		return nil
	case "version", "--version":
		if len(args) != 1 {
			return fmt.Errorf("expected hatch %s; use hatch --help", args[0])
		}
		fmt.Printf("hatch %s\n", version())
		return nil
	case "cd", "shell-init":
		return shellCommand(args)
	case "new", "info", "path":
		usage, wantArgs = args[0]+" <name>", 2
	case "list":
		usage, wantArgs, listing = "list", 1, true
	case "status":
		usage, wantArgs, changingStatus = "status <name> <status>", 3, true
	case "promote":
		usage, wantArgs, promoting = "promote <name> <target-path>", 3, true
	case "remove":
		usage, wantArgs, removing = "remove <name> [--force]", 2, true
		if len(args) == 3 {
			if args[1] == "--force" {
				args = []string{"remove", args[2]}
				force = true
			} else if args[2] == "--force" {
				args = args[:2]
				force = true
			}
		}
	default:
		return fmt.Errorf("unknown command %q\nRun 'hatch help' for available commands and examples.", args[0])
	}
	if !force && len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Printf("hatch %s\n\n%s", version(), help)
		return nil
	}
	if len(args) != wantArgs {
		return fmt.Errorf("expected hatch %s; use hatch %s --help", usage, args[0])
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
	if args[0] == "path" {
		if info, err := os.Stat(p.Location); err != nil || !info.IsDir() {
			return fmt.Errorf("project location unavailable or not a directory: %s", p.Location)
		}
		_, err := fmt.Fprintln(os.Stdout, p.Location)
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
