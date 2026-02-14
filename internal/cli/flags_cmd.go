package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/goccy/tobari/internal/flags"
)

func (c *CLI) runFlagsCmd(ctx context.Context, tobariBinPath string, args []string) error {
	fs := flag.NewFlagSet("flags", flag.ContinueOnError)
	embedCode := fs.Bool("embed-code", false, "embed source code into the instrumented binary")
	fs.BoolVar(embedCode, "E", false, "embed source code into the instrumented binary (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before other arguments\nUsage: tobari flags [--embed-code | -E]")
		}
	}

	out, err := flags.Run(ctx, tobariBinPath, *embedCode)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(c.stdout, out)
	return err
}
