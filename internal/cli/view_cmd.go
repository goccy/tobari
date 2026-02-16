package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (c *CLI) runViewCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "coverage-view.html", "output HTML file path")
	binary := fs.String("b", "", "path to tobari-built binary with embedded sources")
	sources := fs.String("s", "", "path to tar.gz archive of extracted sources")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("missing input file\nUsage: tobari view [-o output.html] [-b binary | -s sources.tar.gz] <tobari.json>")
	}
	for _, arg := range fs.Args()[1:] {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before the input file\nUsage: tobari view [-o output.html] [-b binary | -s sources.tar.gz] <tobari.json>")
		}
	}
	if *binary != "" && *sources != "" {
		return fmt.Errorf("-b and -s flags are mutually exclusive")
	}

	inputFile := fs.Arg(0)

	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read input file %s: %w", inputFile, err)
	}

	// Only accept tobari.json format (not coverprofile)
	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("mode:")) {
		return fmt.Errorf("tobari view requires tobari.json input (per-test coverage data)\nUse 'tobari html' for coverprofile files")
	}

	var entriesMap map[string][]tobariJSONEntry
	if err := json.Unmarshal(data, &entriesMap); err != nil {
		return fmt.Errorf("failed to parse tobari.json: %w", err)
	}

	// Source resolution
	sourceDir := ""
	if *binary != "" || *sources != "" {
		tmpDir, err := os.MkdirTemp("", "tobari-view-*")
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		tarGzPath := *sources
		if *binary != "" {
			tarGzPath = filepath.Join(tmpDir, "sources.tar.gz")
			if err := extractSourcesFromBinary(ctx, *binary, tarGzPath); err != nil {
				return fmt.Errorf("failed to extract sources from binary %s: %w", *binary, err)
			}
		}

		if err := extractTarGz(tarGzPath, tmpDir); err != nil {
			return fmt.Errorf("failed to extract tar.gz: %w", err)
		}
		sourceDir = tmpDir
	}

	// Collect unique file paths and build index
	filePaths, fileIndexMap := buildFileIndex(entriesMap)

	// Shorten file paths for display
	shortNames := shortenFilePaths(filePaths)

	// Read source files
	files := make([]*viewFile, len(filePaths))
	for i, p := range filePaths {
		actualPath := p
		if sourceDir != "" {
			actualPath = filepath.Join(sourceDir, p)
		}

		content, err := os.ReadFile(actualPath)
		if err != nil {
			files[i] = &viewFile{
				Path:  p,
				Short: shortNames[i],
				Lines: []string{"// Source not available: " + err.Error()},
			}
			continue
		}

		lines := strings.Split(string(content), "\n")
		// Remove trailing empty line from Split
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		files[i] = &viewFile{
			Path:  p,
			Short: shortNames[i],
			Lines: lines,
		}
	}

	// Build view data
	vd := buildViewData(entriesMap, files, fileIndexMap)

	// Generate HTML
	outputFile, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", *output, err)
	}
	defer func() { _ = outputFile.Close() }()

	if err := generateViewHTML(outputFile, vd); err != nil {
		return fmt.Errorf("failed to generate HTML: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdout, "Interactive coverage report written to %s\n", *output); err != nil {
		return err
	}
	return nil
}
