package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extractSourcesFromBinary runs a tobari-built binary with TOBARI_EXTRACT_SOURCES
// to extract embedded source files into a tar.gz archive at outputPath.
func extractSourcesFromBinary(ctx context.Context, binPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "TOBARI_EXTRACT_SOURCES="+outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run %s: %w", binPath, err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file was not created: %s", outputPath)
	}
	return nil
}

// extractTarGz extracts a tar.gz archive into destDir.
func extractTarGz(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", tarGzPath, err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

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
			defer func() { _ = out.Close() }()

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
