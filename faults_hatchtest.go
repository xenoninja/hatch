//go:build hatchtest

package main

import (
	"os"
	"syscall"
	"time"
)

// Only compiled into the CLI test binary, never the distributed binary.
func init() {
	if os.Getenv("HATCH_TEST_RENAME_EXDEV") == "1" {
		moveDirectory = func(string, string) error { return syscall.EXDEV }
	}
	if value := os.Getenv("HATCH_TEST_TIME"); value != "" {
		instant, err := time.Parse(time.RFC3339, value)
		if err != nil {
			panic(err)
		}
		now = func() time.Time { return instant }
	}
	interrupt = func(point string) {
		if os.Getenv("HATCH_TEST_INTERRUPT") == point {
			os.Exit(86)
		}
	}
}
