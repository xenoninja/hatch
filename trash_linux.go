package main

import "fmt"

func nativeTrash(string) (string, error) {
	return "", fmt.Errorf("trash unavailable on Linux in this release; files and tracking retained")
}
