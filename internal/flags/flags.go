package flags

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func Run(ctx context.Context, tobariBinPath string) (string, error) {
	path, err := exec.LookPath(tobariBinPath)
	if err != nil {
		return "", fmt.Errorf("failed to find tobari binary path from %s: %w", tobariBinPath, err)
	}
	if !filepath.IsAbs(path) {
		p, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to get abs path from %s: %w", tobariBinPath, err)
		}
		path = p
	}
	// Create overlay files (used by toolexec for file replacement during compilation)
	if _, err := overlay.Create(ctx); err != nil {
		return "", fmt.Errorf("failed to create overlay file: %w", err)
	}
	return strings.Join([]string{
		"-cover",
		"-toolexec=" + path,
	}, " "), nil
}
