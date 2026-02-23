package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/tobari"
)

func (c *CLI) runHTMLCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("html", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "cover.html", "output HTML file path")
	binary := fs.String("b", "", "path to tobari-built binary with embedded sources")
	sources := fs.String("s", "", "path to tar.gz archive of extracted sources")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("missing input file\nUsage: tobari html [-o output.html] [-b binary | -s sources.tar.gz] <coverprofile-or-tobari.json>")
	}
	for _, arg := range fs.Args()[1:] {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("flags must be specified before the input file\nUsage: tobari html [-o output.html] [-b binary | -s sources.tar.gz] <coverprofile-or-tobari.json>")
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

	isCoverprofile := bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("mode:"))

	// Resolve source directory for -b / -s flags
	sourceDir := ""
	if *binary != "" || *sources != "" {
		tmpDir, err := os.MkdirTemp("", "tobari-html-*")
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

	if isCoverprofile {
		return c.generateCoverprofileHTML(ctx, string(data), *output, sourceDir)
	}

	// tobari.json → interactive HTML report
	entriesMap, err := parseTobariJSON(data)
	if err != nil {
		return fmt.Errorf("failed to parse tobari.json: %w", err)
	}
	if err := generateTobarifmtReport(entriesMap, *output, sourceDir); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.stdout, "HTML coverage report written to %s\n", *output); err != nil {
		return err
	}
	return nil
}

func (c *CLI) generateCoverprofileHTML(ctx context.Context, coverprofileContent, output, sourceDir string) error {
	if sourceDir != "" {
		coverprofileContent = replacePathsInCoverprofile(coverprofileContent, sourceDir)
	}

	tmpCoverprofile, err := os.CreateTemp("", "tobari-coverprofile-*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp coverprofile: %w", err)
	}
	defer func() { _ = os.Remove(tmpCoverprofile.Name()) }()

	if _, err := tmpCoverprofile.WriteString(coverprofileContent); err != nil {
		_ = tmpCoverprofile.Close()
		return fmt.Errorf("failed to write temp coverprofile: %w", err)
	}
	if err := tmpCoverprofile.Close(); err != nil {
		return fmt.Errorf("failed to close temp coverprofile: %w", err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("failed to find go binary: %w", err)
	}
	coverCmd := exec.CommandContext(ctx, goBin, "tool", "cover", "-html="+tmpCoverprofile.Name(), "-o", output)
	coverCmd.Stdout = c.stdout
	coverCmd.Stderr = c.stderr
	if err := coverCmd.Run(); err != nil {
		return fmt.Errorf("failed to run go tool cover -html: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdout, "HTML coverage report written to %s\n", output); err != nil {
		return err
	}
	return nil
}

// generateTobarifmtReport generates the interactive HTML report from tobari.json data.
func generateTobarifmtReport(entriesMap map[string][]tobariJSONEntry, output string, sourceDir string) error {
	filePaths, fileIndexMap := buildFileIndex(entriesMap)
	shortNames := shortenFilePaths(filePaths)

	files := make([]*tobarifmtFile, len(filePaths))
	for i, p := range filePaths {
		actualPath := p
		if sourceDir != "" {
			actualPath = filepath.Join(sourceDir, p)
		}

		content, err := os.ReadFile(actualPath)
		if err != nil {
			files[i] = &tobarifmtFile{
				Path:  p,
				Short: shortNames[i],
				Lines: []string{"// Source not available: " + err.Error()},
			}
			continue
		}

		lines := strings.Split(string(content), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		files[i] = &tobarifmtFile{
			Path:  p,
			Short: shortNames[i],
			Lines: lines,
		}
	}

	td := buildTobarifmtData(entriesMap, files, fileIndexMap)

	outputFile, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", output, err)
	}
	defer func() { _ = outputFile.Close() }()

	if err := generateTobarifmtHTML(outputFile, td); err != nil {
		return fmt.Errorf("failed to generate HTML: %w", err)
	}
	return nil
}

type tobariJSONEntry struct {
	FileName       string        `json:"FileName"`
	Start          tobariEntryPos `json:"Start"`
	End            tobariEntryPos `json:"End"`
	StatementCount int           `json:"StatementCount"`
	Count          int           `json:"Count"`
}

type tobariEntryPos struct {
	Line   int `json:"Line"`
	Column int `json:"Column"`
}

func tobariJSONToCoverprofile(data []byte) (string, error) {
	// Try compact format first to use CoverReport.WriteCoverprofile.
	var report tobari.CoverReport
	if err := json.Unmarshal(data, &report); err == nil && report.Metadata.Files != nil {
		var b strings.Builder
		if err := report.WriteCoverprofile(&b); err != nil {
			return "", err
		}
		return b.String(), nil
	}

	// Legacy format: merge entries across tests.
	var entriesMap map[string][]tobariJSONEntry
	if err := json.Unmarshal(data, &entriesMap); err != nil {
		return "", fmt.Errorf("failed to decode tobari.json: %w", err)
	}
	return legacyEntriesToCoverprofile(entriesMap), nil
}

// legacyEntriesToCoverprofile merges legacy format entries across tests.
func legacyEntriesToCoverprofile(entriesMap map[string][]tobariJSONEntry) string {
	type mergedEntry struct {
		tobariJSONEntry
		key string
	}
	merged := make(map[string]*mergedEntry)
	var keys []string

	for _, entries := range entriesMap {
		for _, e := range entries {
			key := fmt.Sprintf("%s:%d.%d,%d.%d",
				e.FileName, e.Start.Line, e.Start.Column,
				e.End.Line, e.End.Column)
			if existing, ok := merged[key]; ok {
				existing.Count += e.Count
			} else {
				entry := e
				merged[key] = &mergedEntry{tobariJSONEntry: entry, key: key}
				keys = append(keys, key)
			}
		}
	}

	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("mode: set\n")
	for _, key := range keys {
		e := merged[key]
		fmt.Fprintf(&b, "%s %d %d\n", key, e.StatementCount, e.Count)
	}
	return b.String()
}

func replacePathsInCoverprofile(content, sourceDir string) string {
	var result strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "mode:") || line == "" {
			if line != "" {
				result.WriteString(line + "\n")
			}
			continue
		}
		idx := findCoverageColonIndex(line)
		if idx < 0 {
			result.WriteString(line + "\n")
			continue
		}
		origPath := line[:idx]
		rest := line[idx:]
		newPath := filepath.Join(sourceDir, origPath)
		result.WriteString(newPath + rest + "\n")
	}
	return result.String()
}

func findCoverageColonIndex(line string) int {
	for i := len(line) - 1; i >= 0; i-- {
		if line[i] == ':' && i+1 < len(line) && line[i+1] >= '0' && line[i+1] <= '9' {
			return i
		}
	}
	return -1
}

// parseTobariJSON auto-detects the tobari.json format (legacy or compact)
// and returns the entries as a map[string][]tobariJSONEntry.
func parseTobariJSON(data []byte) (map[string][]tobariJSONEntry, error) {
	var report tobari.CoverReport
	if err := json.Unmarshal(data, &report); err == nil && report.Metadata.Files != nil {
		return expandCoverReport(&report), nil
	}

	var entriesMap map[string][]tobariJSONEntry
	if err := json.Unmarshal(data, &entriesMap); err != nil {
		return nil, fmt.Errorf("failed to decode tobari.json: %w", err)
	}
	return entriesMap, nil
}

// expandCoverReport converts a compact CoverReport to the legacy entry map format.
func expandCoverReport(r *tobari.CoverReport) map[string][]tobariJSONEntry {
	result := make(map[string][]tobariJSONEntry, len(r.Counts))
	for _, c := range r.Counts {
		entries := make([]tobariJSONEntry, 0, len(c.Coverprofile))
		for _, cp := range c.Coverprofile {
			if len(cp) != 2 {
				continue
			}
			blockIdx := cp[0]
			count := cp[1]
			if blockIdx < 0 || blockIdx >= len(r.Metadata.All) {
				continue
			}
			block := r.Metadata.All[blockIdx]
			if len(block) != 6 {
				continue
			}
			fileIdx := block[0]
			fileName := ""
			if fileIdx >= 0 && fileIdx < len(r.Metadata.Files) {
				fileName = r.Metadata.Files[fileIdx]
			}
			entries = append(entries, tobariJSONEntry{
				FileName:       fileName,
				Start:          tobariEntryPos{Line: block[1], Column: block[2]},
				End:            tobariEntryPos{Line: block[3], Column: block[4]},
				StatementCount: block[5],
				Count:          count,
			})
		}
		result[c.Name] = entries
	}
	return result
}
