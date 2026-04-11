package cli

import "fmt"

const helpText = `tobari - Go scoped coverage measurement tool

Usage:
    tobari <command>
    tobari [flags]

Commands:
    flags       Output flags for go build/test with coverage
    extract     Extract embedded source code from an instrumented binary
    html        Generate HTML coverage report
    convert     Convert tobari.json to coverprofile format
    merge       Merge multiple tobari.json or source archives
    version     Show version information
    help        Show this help message

Flags:
    -v, --version    Show version information
    -h, --help       Show this help message

Flags Command Options:
    --embed-code, -E    Embed original source code into the instrumented binary
    -tags=VALUE         Build tags (same as go build -tags)

HTML Command Options:
    -o <file>           Output HTML file path (default: cover.html)
    -b <binary>         Path to tobari-built binary with embedded sources
    -s <tar.gz>         Path to tar.gz archive of extracted sources

    When given a tobari.json file, generates an interactive HTML report with
    per-test coverage visualization, overlap analysis, and summary statistics.
    When given a coverprofile file, generates a standard HTML report using
    go tool cover.

Convert Command Options:
    -o <file>           Output coverprofile file path (default: cover.out)

Merge Command:
    tobari merge json [-o merged.json] <file.json|./...> [...]
    tobari merge source [-o merged.tar.gz] <a.tar.gz> <b.tar.gz> [...]

    merge json      Merge multiple tobari.json files into one.
                    Inputs may be literal file paths and/or Go-style "./..."
                    package patterns. Pattern arguments walk the filesystem
                    recursively and pick up every "tobari/tobari.json" file
                    they find, skipping vendor/ and dot/underscore-prefixed
                    directories. Symbolic links are not followed.
                    Note: avoid pointing -o to a path ending in
                    tobari/tobari.json, since the next run would pick it up.
    merge source    Merge multiple source tar.gz archives into one.
                    Duplicate archives (same SHA-256 hash) are skipped.
                    Conflicting files (same path, different content) cause an error.

Toolexec Options (used with -toolexec):
    --embed-code        Embed original source code into the instrumented binary

Examples:
    # Get flags for go build
    go build $(tobari flags) ./...

    # Get flags with embedded source code
    go build $(tobari flags -E) ./...

    # Use tobari directly as toolexec
    go build -cover -toolexec=tobari ./...

    # Use tobari directly as toolexec with source embedding
    go build -cover -toolexec='tobari --embed-code' ./...

    # Use tobari with build tags (e.g., timetzdata)
    GOFLAGS=$(tobari flags -tags=timetzdata) go build ./...

    # Extract embedded sources from an instrumented binary
    tobari extract -o sources.tar.gz ./my-binary

    # Generate interactive HTML from tobari.json
    tobari html -o cover.html tobari.json

    # Generate HTML with embedded sources
    tobari html -o cover.html -b ./my-binary tobari.json

    # Generate standard HTML from coverprofile
    tobari html -o cover.html profile.cover

    # Convert tobari.json to coverprofile
    tobari convert -o profile.cover tobari.json

    # Merge multiple tobari.json files
    tobari merge json -o merged.json a.json b.json

    # Merge every tobari/tobari.json under the current directory recursively
    tobari merge json -o merged.json ./...

    # Merge multiple source archives
    tobari merge source -o merged.tar.gz a.tar.gz b.tar.gz

    # Show version
    tobari version
    tobari -v

For more information, visit: https://github.com/goccy/tobari
`

func (c *CLI) showHelp() error {
	_, err := fmt.Fprint(c.stdout, helpText)
	return err
}
