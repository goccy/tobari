package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func (c *CLI) runHTMLCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("html", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	output := fs.String("o", "coverage.html", "output HTML file path")
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

	var coverprofileContent string
	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("mode:")) {
		coverprofileContent = string(data)
	} else {
		profile, err := tobariJSONToCoverprofile(data)
		if err != nil {
			return fmt.Errorf("failed to parse tobari.json: %w", err)
		}
		coverprofileContent = profile
	}

	if *binary != "" || *sources != "" {
		tmpDir, err := os.MkdirTemp("", "tobari-html-*")
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		tarGzPath := *sources
		if *binary != "" {
			tarGzPath = filepath.Join(tmpDir, "sources.tar.gz")
			cmd := exec.CommandContext(ctx, *binary)
			cmd.Env = append(os.Environ(), "TOBARI_EXTRACT_SOURCES="+tarGzPath)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to extract sources from binary %s: %w", *binary, err)
			}
		}

		if err := extractTarGz(tarGzPath, tmpDir); err != nil {
			return fmt.Errorf("failed to extract tar.gz: %w", err)
		}

		coverprofileContent = replacePathsInCoverprofile(coverprofileContent, tmpDir)
	}

	tmpCoverprofile, err := os.CreateTemp("", "tobari-coverprofile-*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp coverprofile: %w", err)
	}
	defer os.Remove(tmpCoverprofile.Name())

	if _, err := tmpCoverprofile.WriteString(coverprofileContent); err != nil {
		tmpCoverprofile.Close()
		return fmt.Errorf("failed to write temp coverprofile: %w", err)
	}
	if err := tmpCoverprofile.Close(); err != nil {
		return fmt.Errorf("failed to close temp coverprofile: %w", err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("failed to find go binary: %w", err)
	}
	coverCmd := exec.CommandContext(ctx, goBin, "tool", "cover", "-html="+tmpCoverprofile.Name(), "-o", *output)
	coverCmd.Stdout = c.stdout
	coverCmd.Stderr = c.stderr
	if err := coverCmd.Run(); err != nil {
		return fmt.Errorf("failed to run go tool cover -html: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdout, "HTML coverage report written to %s\n", *output); err != nil {
		return err
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
	var entriesMap map[string][]tobariJSONEntry
	if err := json.Unmarshal(data, &entriesMap); err != nil {
		return "", fmt.Errorf("failed to decode tobari.json: %w", err)
	}

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
		fmt.Fprintf(&b, "%s %d %d\n",
			key, e.StatementCount, e.Count)
	}
	return b.String(), nil
}

func extractTarGz(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", tarGzPath, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", target, err)
		}

		if err := func() error {
			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("failed to create %s: %w", target, err)
			}
			defer out.Close()

			if _, err := io.Copy(out, tr); err != nil {
				return fmt.Errorf("failed to write %s: %w", target, err)
			}
			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
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
