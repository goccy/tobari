package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
)

func (c *CLI) runExtractCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "", "output file path for the tar.gz archive (required)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before the binary path\nUsage: tobari extract -o <output.tar.gz> <binary>")
		}
	}

	if *output == "" {
		return fmt.Errorf("missing required flag: -o <output.tar.gz>\nUsage: tobari extract -o <output.tar.gz> <binary>")
	}

	binArgs := fs.Args()
	if len(binArgs) == 0 {
		return fmt.Errorf("missing binary path\nUsage: tobari extract -o <output.tar.gz> <binary>")
	}
	binPath := binArgs[0]

	if err := extractSourcesFromBinary(ctx, binPath, *output); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(c.stdout, "extracted to %s\n", *output); err != nil {
		return err
	}
	return nil
}
