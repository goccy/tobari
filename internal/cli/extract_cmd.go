package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func (c *CLI) runExtractCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "", "output file path for the tar.gz archive (required)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *output == "" {
		return fmt.Errorf("missing required flag: -o <output.tar.gz>\nUsage: tobari extract -o <output.tar.gz> <binary>")
	}

	binArgs := fs.Args()
	if len(binArgs) == 0 {
		return fmt.Errorf("missing binary path\nUsage: tobari extract -o <output.tar.gz> <binary>")
	}
	binPath := binArgs[0]

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "TOBARI_EXTRACT_SOURCES="+*output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run %s: %w", binPath, err)
	}

	if _, err := os.Stat(*output); err != nil {
		return fmt.Errorf("output file was not created: %s", *output)
	}

	if _, err := fmt.Fprintf(c.stdout, "extracted to %s\n", *output); err != nil {
		return err
	}
	return nil
}
