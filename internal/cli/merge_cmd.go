package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	flagSet := flag.NewFlagSet("merge json", flag.ContinueOnError)
	flagSet.SetOutput(c.stderr)
	output := flagSet.String("o", "merged.json", "output merged tobari.json file path")

	if err := flagSet.Parse(args); err != nil {
		return err
	}

	if flagSet.NArg() == 0 {
		return fmt.Errorf("missing input\nUsage: tobari merge json [-o merged.json] <file.json|./...> [...]")
	}
	for _, arg := range flagSet.Args() {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before input files\nUsage: tobari merge json [-o merged.json] <file.json|./...> [...]")
		}
	}

	inputFiles, err := expandMergeInputs(flagSet.Args())
	if err != nil {
		return err
	}

	reports := make([]*tobari.CoverReport, 0, len(inputFiles))
	for _, inputFile := range inputFiles {
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
	flagSet := flag.NewFlagSet("merge source", flag.ContinueOnError)
	flagSet.SetOutput(c.stderr)
	output := flagSet.String("o", "merged.tar.gz", "output merged source archive path")

	if err := flagSet.Parse(args); err != nil {
		return err
	}

	if flagSet.NArg() < 2 {
		return fmt.Errorf("at least two input files required\nUsage: tobari merge source [-o merged.tar.gz] <a.tar.gz> <b.tar.gz> [...]")
	}
	for _, arg := range flagSet.Args() {
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

	inputs := make([]io.Reader, 0, flagSet.NArg())
	for _, inputFile := range flagSet.Args() {
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

	if _, err := fmt.Fprintf(c.stdout, "Merged %d source archives into %s\n", flagSet.NArg(), *output); err != nil {
		return err
	}
	return nil
}

// classifyMergeArg classifies a single CLI argument for `tobari merge json`.
// kind is "file" or "pattern". For "pattern" kind, base is the walk root.
func classifyMergeArg(arg string) (kind string, base string, err error) {
	if strings.HasPrefix(arg, "-") {
		return "", "", fmt.Errorf("unexpected flag-like argument: %s", arg)
	}
	if arg == "..." {
		return "", "", fmt.Errorf("bare '...' is not allowed; use './...' to walk from current directory")
	}
	slashed := filepath.ToSlash(arg)
	if strings.HasSuffix(slashed, "/...") {
		trimmed := strings.TrimSuffix(slashed, "/...")
		if trimmed == "" {
			return "", "", fmt.Errorf("pattern base must not be filesystem root: %s", arg)
		}
		return "pattern", filepath.Clean(filepath.FromSlash(trimmed)), nil
	}
	return "file", "", nil
}

// isTobariJSONPath reports whether p ends with the canonical
// "tobari/tobari.json" path tobari writes per package.
func isTobariJSONPath(p string) bool {
	sp := filepath.ToSlash(p)
	return sp == "tobari/tobari.json" || strings.HasSuffix(sp, "/tobari/tobari.json")
}

// shouldSkipMergeWalkDir reports whether a directory found during pattern
// expansion should be skipped. The walk root itself is never skipped (the
// caller passes path != root).
func shouldSkipMergeWalkDir(name string) bool {
	if name == "vendor" {
		return true
	}
	if len(name) > 0 && (name[0] == '.' || name[0] == '_') {
		return true
	}
	return false
}

// walkTobariJSON walks root recursively and returns paths to all
// "tobari/tobari.json" files found, skipping vendor/ and dot/underscore-prefixed
// directories. Symbolic links are not followed.
func walkTobariJSON(root string) ([]string, error) {
	var found []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			// Ignore permission errors etc. on subentries.
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipMergeWalkDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if isTobariJSONPath(path) {
			found = append(found, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return found, nil
}

// expandMergeInputs converts a raw argument list (mix of literal file paths
// and "./..."-style patterns) into a deduplicated list of input file paths,
// preserving discovery order.
func expandMergeInputs(args []string) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string
	hasPattern := false
	var patterns []string

	add := func(p string) error {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path for %s: %w", p, err)
		}
		if _, dup := seen[abs]; dup {
			return nil
		}
		seen[abs] = struct{}{}
		out = append(out, p)
		return nil
	}

	for _, a := range args {
		kind, base, err := classifyMergeArg(a)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "file":
			if err := add(a); err != nil {
				return nil, err
			}
		case "pattern":
			hasPattern = true
			patterns = append(patterns, a)
			files, err := walkTobariJSON(base)
			if err != nil {
				return nil, fmt.Errorf("failed to walk %s: %w", a, err)
			}
			for _, f := range files {
				if err := add(f); err != nil {
					return nil, err
				}
			}
		}
	}

	if hasPattern && len(out) == 0 {
		return nil, fmt.Errorf("no tobari/tobari.json files found under patterns: %v", patterns)
	}
	return out, nil
}
