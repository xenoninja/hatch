package main

import (
	"fmt"
	"os"

	"github.com/xenoninja/hatch/internal/hatch"
)

func main() {
	if err := hatch.Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hatch:", err)
		os.Exit(1)
	}
}
