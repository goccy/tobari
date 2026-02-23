package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/tobari"
)

func (c *CLI) runConvertCmd(_ context.Context, args []string) (e error) {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "cover.out", "output coverprofile file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("missing input file\nUsage: tobari convert [-o output.cover] <tobari.json>")
	}
	for _, arg := range fs.Args()[1:] {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before the input file\nUsage: tobari convert [-o output.cover] <tobari.json>")
		}
	}

	inputFile := fs.Arg(0)

	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read input file %s: %w", inputFile, err)
	}

	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("mode:")) {
		return fmt.Errorf("input is already in coverprofile format")
	}

	var report tobari.CoverReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("failed to parse tobari.json: %w", err)
	}

	f, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", *output, err)
	}
	defer func() { e = errors.Join(e, f.Close()) }()

	if err := report.WriteCoverprofile(f); err != nil {
		return fmt.Errorf("failed to write coverprofile: %w", err)
	}
	if _, err := fmt.Fprintf(c.stdout, "Coverprofile written to %s\n", *output); err != nil {
		return err
	}
	return nil
}
