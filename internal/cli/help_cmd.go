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
    version     Show version information
    help        Show this help message

Flags:
    -v, --version    Show version information
    -h, --help       Show this help message

Flags Command Options:
    --embed-code, -E    Embed original source code into the instrumented binary

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

    # Show version
    tobari version
    tobari -v

For more information, visit: https://github.com/goccy/tobari
`

func (c *CLI) showHelp() error {
	_, err := fmt.Fprint(c.stdout, helpText)
	return err
}
