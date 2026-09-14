package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type promotionRaceResult struct {
	out string
	err error
}

func racePromotionCommands(home string, commands ...[]string) []promotionRaceResult {
	results := make([]promotionRaceResult, len(commands))
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(len(commands))
	done.Add(len(commands))
	for i, args := range commands {
		go func(i int, args []string) {
			defer done.Done()
			ready.Done()
			<-start
			results[i].out, results[i].err = invoke(home, nil, args...)
		}(i, args)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results
}

func promotionRaceHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func promotionRaceSource(t *testing.T, home, name string) string {
	t.Helper()
	run(t, home, true, nil, "new", name)
	source := filepath.Join(home, "experiments", "2026-09-14-"+name)
	if err := os.WriteFile(filepath.Join(source, "sentinel.txt"), []byte(name), 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

func checkPromotionRaceProject(t *testing.T, home, name, status, location string) {
	t.Helper()
	contains(t, run(t, home, true, nil, "info", name), "Name: "+name+"\n", "Created: 2026-09-14\n", "Status: "+status+"\n", "Location: "+location+"\n")
	data, err := os.ReadFile(filepath.Join(location, "sentinel.txt"))
	if err != nil || string(data) != name {
		t.Fatalf("%s sentinel: got %q, error %v; want %q", location, data, err, name)
	}
}

func promotionRaceWinner(t *testing.T, results []promotionRaceResult) int {
	t.Helper()
	winner := -1
	for i, result := range results {
		if result.err == nil {
			if winner != -1 {
				t.Fatalf("multiple successful promotions: %+v", results)
			}
			winner = i
		}
	}
	if winner == -1 {
		t.Fatalf("no successful promotion: %+v", results)
	}
	return winner
}

func TestPromotionConcurrencyDistinctProjectsSameTarget(t *testing.T) {
	home := promotionRaceHome(t)
	names := []string{"alpha", "beta"}
	sources := []string{promotionRaceSource(t, home, names[0]), promotionRaceSource(t, home, names[1])}
	target := filepath.Join(home, "exact-target")
	results := racePromotionCommands(home,
		[]string{"promote", names[0], target},
		[]string{"promote", names[1], target},
	)
	winner := promotionRaceWinner(t, results)
	loser := 1 - winner
	contains(t, results[loser].out, "destination already exists")
	checkPromotionRaceProject(t, home, names[winner], "promoted", target)
	checkPromotionRaceProject(t, home, names[loser], "active", sources[loser])
	absent(t, sources[winner])
}

func TestPromotionConcurrencySameProjectTwoTargets(t *testing.T) {
	home := promotionRaceHome(t)
	source := promotionRaceSource(t, home, "example")
	targets := []string{filepath.Join(home, "target-a"), filepath.Join(home, "target-b")}
	results := racePromotionCommands(home,
		[]string{"promote", "example", targets[0]},
		[]string{"promote", "example", targets[1]},
	)
	winner := promotionRaceWinner(t, results)
	contains(t, results[1-winner].out, "terminal")
	checkPromotionRaceProject(t, home, "example", "promoted", targets[winner])
	absent(t, source, targets[1-winner])
}

func TestPromotionConcurrencyStatusAndSameNameCreation(t *testing.T) {
	home := promotionRaceHome(t)
	source := promotionRaceSource(t, home, "example")
	target := filepath.Join(home, "target")
	results := racePromotionCommands(home,
		[]string{"promote", "example", target},
		[]string{"status", "example", "completed"},
		[]string{"new", "example"},
	)
	if results[0].err != nil {
		t.Fatalf("promotion: %s %v", results[0].out, results[0].err)
	}
	// Status may run before promotion, or be rejected after it becomes terminal.
	if results[1].err == nil {
		contains(t, results[1].out, "Status: completed\n")
	} else {
		contains(t, results[1].out, "terminal")
	}
	if results[2].err == nil {
		t.Fatalf("same-name creation succeeded: %s", results[2].out)
	}
	contains(t, results[2].out, "already tracked")
	checkPromotionRaceProject(t, home, "example", "promoted", target)
	absent(t, source)
	contains(t, run(t, home, false, nil, "status", "example", "active"), "terminal")
	// A later date must not allow another directory for this reserved name.
	contains(t, run(t, home, false, []string{"HATCH_TEST_TIME=2027-01-01T12:00:00Z"}, "new", "example"), "already tracked")
	entries, err := os.ReadDir(filepath.Join(home, "experiments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("duplicate project directories: %v, error %v", entries, err)
	}
	listing := run(t, home, true, nil, "list")
	if strings.Count(listing, "example") != 1 {
		t.Fatalf("expected one reserved identity: %s", listing)
	}
	contains(t, listing, "promoted")
}

func TestPromotionConcurrencyInterruptedRecovery(t *testing.T) {
	home := promotionRaceHome(t)
	source := promotionRaceSource(t, home, "example")
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "target")
	out, err := invoke(home, []string{"HATCH_TEST_INTERRUPT=after-promotion-move"}, "promote", "example", target)
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 86 {
		t.Fatalf("interrupt did not fire: %s %v", out, err)
	}
	absent(t, source)
	results := racePromotionCommands(home,
		[]string{"info", "example"},
		[]string{"list"},
		[]string{"new", "other"},
		[]string{"status", "example", "completed"},
		[]string{"new", "example"},
	)
	for i := 0; i < 3; i++ {
		if results[i].err != nil {
			t.Fatalf("recovery command %d: %s %v", i, results[i].out, results[i].err)
		}
	}
	contains(t, results[0].out, "Name: example\n", "Created: 2026-09-14\n", "Status: promoted\n", "Location: "+target+"\n")
	contains(t, results[1].out, "example", "promoted")
	contains(t, results[2].out, "Name: other\n", "Status: active\n")
	for i, message := range []string{"terminal", "already tracked"} {
		result := results[i+3]
		if result.err == nil {
			t.Fatalf("recovery allowed forbidden mutation: %s", result.out)
		}
		contains(t, result.out, message)
	}
	checkPromotionRaceProject(t, home, "example", "promoted", target)
	moved, err := os.Stat(target)
	if err != nil || !os.SameFile(original, moved) {
		t.Fatalf("recovery changed directory identity: %v", err)
	}
	absent(t, source)
	entries, err := os.ReadDir(filepath.Join(home, "experiments"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "2026-09-14-other" || !entries[0].IsDir() {
		t.Fatalf("unexpected project directories after recovery: %v, error %v", entries, err)
	}
}
