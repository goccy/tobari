package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/tool"
)

// CLI handles command-line interface processing
type CLI struct {
	stdout io.Writer
	stderr io.Writer
}

// New creates a new CLI instance
func New() *CLI {
	return &CLI{
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// Run processes command-line arguments
func (c *CLI) Run(ctx context.Context, args []string) error {
	if len(args) == 1 {
		return c.showHelp()
	}

	tobariBinPath := args[0]

	// Parse tobari-specific flags before the tool path.
	// When invoked as: tobari [--embed-code] [--build-tags=VALUE] /path/to/compile <args>
	embedCode := false
	buildTags := ""
	i := 1
	for i < len(args) {
		if args[i] == "--embed-code" {
			embedCode = true
			i++
		} else if strings.HasPrefix(args[i], "--build-tags=") {
			buildTags = strings.TrimPrefix(args[i], "--build-tags=")
			i++
		} else {
			break
		}
	}
	args = append(args[:1], args[i:]...) // remove consumed flags

	if len(args) < 2 {
		return c.showHelp()
	}

	arg1 := args[1]

	// Check if this is a toolexec call
	// toolexec passes Go tool's absolute path as args[1]
	if isToolexecCall(arg1) {
		return tool.Handle(ctx, args, tool.BuildOpts{
			EmbedCode: embedCode,
			BuildTags: buildTags,
		})
	}

	// Handle flags (starts with "-")
	if len(arg1) > 0 && arg1[0] == '-' {
		return c.handleFlags(ctx, args[1:])
	}

	// Handle subcommands
	return c.handleSubcommand(ctx, tobariBinPath, arg1, args[2:])
}

// isToolexecCall determines if the argument is a toolexec invocation
func isToolexecCall(arg string) bool {
	// toolexec passes Go tools as absolute paths
	// e.g., /usr/local/go/pkg/tool/darwin_arm64/compile
	return filepath.IsAbs(arg)
}

// handleFlags processes top-level flags
func (c *CLI) handleFlags(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tobari", flag.ContinueOnError)
	fs.SetOutput(c.stderr)

	showVersion := fs.Bool("v", false, "show version")
	showHelp := fs.Bool("h", false, "show help")

	// Also support --version and --help (flag treats -- as -)
	fs.BoolVar(showVersion, "version", false, "show version")
	fs.BoolVar(showHelp, "help", false, "show help")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		return c.showVersion()
	}
	if *showHelp {
		return c.showHelp()
	}

	return c.showHelp()
}

// handleSubcommand processes subcommands
func (c *CLI) handleSubcommand(ctx context.Context, tobariBinPath, cmd string, args []string) error {
	switch cmd {
	case "flags":
		return c.runFlagsCmd(ctx, tobariBinPath, args)
	case "extract":
		return c.runExtractCmd(ctx, args)
	case "html":
		return c.runHTMLCmd(ctx, args)
	case "convert":
		return c.runConvertCmd(ctx, args)
	case "merge":
		return c.runMergeCmd(ctx, args)
	case "version":
		return c.showVersion()
	case "help":
		return c.showHelp()
	default:
		return fmt.Errorf("unknown command: %s\nRun 'tobari help' for usage", cmd)
	}
}
