package cli

import "fmt"

const helpText = `tobari - Go scoped coverage measurement tool

Usage:
    tobari <command>
    tobari [flags]

Commands:
    flags       Output flags for go build/test with coverage
    version     Show version information
    help        Show this help message

Flags:
    -v, --version    Show version information
    -h, --help       Show this help message

Examples:
    # Get flags for go build
    go build $(tobari flags) ./...

    # Show version
    tobari version
    tobari -v

For more information, visit: https://github.com/goccy/tobari
`

func (c *CLI) showHelp() error {
	_, err := fmt.Fprint(c.stdout, helpText)
	return err
}
