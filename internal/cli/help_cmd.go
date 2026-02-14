package cli

import "fmt"

const helpText = `tobari - Go scoped coverage measurement tool

Usage:
    tobari <command>
    tobari [flags]

Commands:
    flags       Output flags for go build/test with coverage
    extract     Extract embedded source code from an instrumented binary
    version     Show version information
    help        Show this help message

Flags:
    -v, --version    Show version information
    -h, --help       Show this help message

Flags Command Options:
    --embed-code, -E    Embed original source code into the instrumented binary

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

    # Show version
    tobari version
    tobari -v

For more information, visit: https://github.com/goccy/tobari
`

func (c *CLI) showHelp() error {
	_, err := fmt.Fprint(c.stdout, helpText)
	return err
}
