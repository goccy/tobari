package cli

import "fmt"

const helpText = `tobari - Go scoped coverage measurement tool

Usage:
    tobari <command>
    tobari [flags]

Commands:
    flags       Output flags for go build/test with coverage
    extract     Extract embedded source code from an instrumented binary
    html        Generate HTML coverage report from coverprofile or tobari.json
    view        Generate interactive HTML coverage visualization from tobari.json
    version     Show version information
    help        Show this help message

Flags:
    -v, --version    Show version information
    -h, --help       Show this help message

Flags Command Options:
    --embed-code, -E    Embed original source code into the instrumented binary

HTML Command Options:
    -o <file>           Output HTML file path (default: coverage.html)
    -b <binary>         Path to tobari-built binary with embedded sources
    -s <tar.gz>         Path to tar.gz archive of extracted sources

View Command Options:
    -o <file>           Output HTML file path (default: coverage-view.html)
    -b <binary>         Path to tobari-built binary with embedded sources
    -s <tar.gz>         Path to tar.gz archive of extracted sources

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

    # Generate HTML coverage report from coverprofile
    tobari html -o coverage.html profile.cover

    # Generate HTML from tobari.json
    tobari html -o coverage.html tobari.json

    # Generate HTML with sources from embedded binary
    tobari html -o coverage.html -b ./my-binary profile.cover

    # Generate HTML with sources from extracted tar.gz
    tobari html -o coverage.html -s sources.tar.gz profile.cover

    # Generate interactive coverage visualization
    tobari view -o report.html tobari.json

    # Show version
    tobari version
    tobari -v

For more information, visit: https://github.com/goccy/tobari
`

func (c *CLI) showHelp() error {
	_, err := fmt.Fprint(c.stdout, helpText)
	return err
}
