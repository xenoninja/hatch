// Command package builds deterministic release ZIPs using only the standard library.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	version := flag.String("version", "", "release version (vMAJOR.MINOR.PATCH, optionally with a prerelease suffix)")
	out := flag.String("out", "dist", "output directory")
	flag.Parse()
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(*version) || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/package -version vMAJOR.MINOR.PATCH [-out dist]")
		os.Exit(1)
	}
	if err := build(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(version, out string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "hatch-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	var sums strings.Builder
	for _, system := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			binary := filepath.Join(temp, "hatch")
			cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid= -X github.com/xenoninja/hatch/internal/hatch.releaseVersion="+version, "-o", binary, ".")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+system, "GOARCH="+arch,
				"GOAMD64=v1", "GOARM64=v8.0", "GOFLAGS=", "GOEXPERIMENT=")
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("build %s/%s: %w\n%s", system, arch, err, output)
			}
			name := fmt.Sprintf("hatch_%s_%s_%s.zip", version, system, arch)
			path := filepath.Join(out, name)
			if err := archive(path, binary); err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
			fmt.Println(path)
		}
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(sums.String()), 0644)
}

func archive(path, binary string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, entry := range []struct {
		source, name string
		mode         os.FileMode
	}{
		{binary, "hatch", 0755},
		{"README.md", "README.md", 0644},
	} {
		data, err := os.ReadFile(entry.source)
		if err != nil {
			return err
		}
		// Fixed ordering, zero timestamps and stored bytes avoid host metadata
		// and compression differences in the package.
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		h.SetMode(entry.mode)
		dst, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := dst.Write(data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}
