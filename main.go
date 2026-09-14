package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var now = time.Now
var interrupt = func(string) {}

const help = `Hatch manages experimental projects.

Usage:
  hatch new <name>   Create an active experimental project
  hatch info <name>  Show persisted project information
  hatch help        Show this help

Names: lowercase ASCII letters or digits separated by single hyphens.
Defaults: ~/experiments/YYYY-MM-DD-<name>, ~/.local/share/hatch/hatch.db
Configuration and XDG overrides are not yet supported.
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
	if len(args) == 2 && (args[0] == "new" || args[0] == "info") && (args[1] == "--help" || args[1] == "-h") {
		fmt.Print(help)
		return nil
	}
	if len(args) != 2 || (args[0] != "new" && args[0] != "info") {
		return fmt.Errorf("expected new <name> or info <name>; use hatch --help")
	}
	name := args[1]
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name %q: use lowercase ASCII letters or digits separated by single hyphens", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return err
	}
	store, err := openStore(home, args[0] == "new")
	if err != nil {
		return err
	}
	if store == nil {
		return fmt.Errorf("unknown experimental project %q", name)
	}
	defer store.close()
	if err := store.reconcile(); err != nil {
		return err
	}
	var p project
	if args[0] == "new" {
		p, err = store.create(name, now().In(time.Local).Format("2006-01-02"))
	} else {
		p, err = store.info(name)
	}
	if err != nil {
		return err
	}
	fmt.Printf("Name: %s\nCreated: %s\nStatus: %s\nLocation: %s\n", p.Name, p.Date, p.Status, p.Location)
	if info, err := os.Stat(p.Location); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "warning: project location unavailable or not a directory: %s\n", p.Location)
	}
	return nil
}
