package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/goccy/tobari"
)

func (c *CLI) runMergeCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand\nUsage: tobari merge <json|source> [...]")
	}
	switch args[0] {
	case "json":
		return c.runMergeJSONCmd(ctx, args[1:])
	case "source":
		return c.runMergeSourceCmd(ctx, args[1:])
	default:
		return fmt.Errorf("unknown merge subcommand: %s\nUsage: tobari merge <json|source> [...]", args[0])
	}
}

func (c *CLI) runMergeJSONCmd(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("merge json", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "merged.json", "output merged tobari.json file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 2 {
		return fmt.Errorf("at least two input files required\nUsage: tobari merge json [-o merged.json] <file1.json> <file2.json> [...]")
	}
	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before input files\nUsage: tobari merge json [-o merged.json] <file1.json> <file2.json> [...]")
		}
	}

	reports := make([]*tobari.CoverReport, 0, fs.NArg())
	for _, inputFile := range fs.Args() {
		data, err := os.ReadFile(inputFile)
		if err != nil {
			return fmt.Errorf("failed to read input file %s: %w", inputFile, err)
		}
		report, err := parseTobariJSON(data)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", inputFile, err)
		}
		reports = append(reports, report)
	}

	merged, err := tobari.MergeCoverReports(reports)
	if err != nil {
		return fmt.Errorf("failed to merge reports: %w", err)
	}

	mergedData, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("failed to marshal merged report: %w", err)
	}

	if err := os.WriteFile(*output, mergedData, 0o644); err != nil {
		return fmt.Errorf("failed to write output file %s: %w", *output, err)
	}

	if _, err := fmt.Fprintf(c.stdout, "Merged %d reports into %s\n", len(reports), *output); err != nil {
		return err
	}
	return nil
}

func (c *CLI) runMergeSourceCmd(_ context.Context, args []string) (e error) {
	fs := flag.NewFlagSet("merge source", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "merged.tar.gz", "output merged source archive path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 2 {
		return fmt.Errorf("at least two input files required\nUsage: tobari merge source [-o merged.tar.gz] <a.tar.gz> <b.tar.gz> [...]")
	}
	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before input files\nUsage: tobari merge source [-o merged.tar.gz] <a.tar.gz> <b.tar.gz> [...]")
		}
	}

	var openFiles []*os.File
	defer func() {
		for _, f := range openFiles {
			e = errors.Join(e, f.Close())
		}
	}()

	inputs := make([]io.Reader, 0, fs.NArg())
	for _, inputFile := range fs.Args() {
		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file %s: %w", inputFile, err)
		}
		openFiles = append(openFiles, f)
		inputs = append(inputs, f)
	}

	outFile, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", *output, err)
	}
	defer func() { e = errors.Join(e, outFile.Close()) }()

	if err := tobari.MergeCoverArchivedFiles(inputs, outFile); err != nil {
		return fmt.Errorf("failed to merge source archives: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdout, "Merged %d source archives into %s\n", fs.NArg(), *output); err != nil {
		return err
	}
	return nil
}
