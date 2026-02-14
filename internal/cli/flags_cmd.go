package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/goccy/tobari/internal/flags"
)

func (c *CLI) runFlagsCmd(ctx context.Context, tobariBinPath string, args []string) error {
	fs := flag.NewFlagSet("flags", flag.ContinueOnError)
	embedCode := fs.Bool("embed-code", false, "embed source code into the instrumented binary")
	fs.BoolVar(embedCode, "E", false, "embed source code into the instrumented binary (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	out, err := flags.Run(ctx, tobariBinPath, *embedCode)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(c.stdout, out)
	return err
}
