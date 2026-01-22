package flags

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func Run(ctx context.Context, tobariBinPath string, fix bool) (string, error) {
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
	f, err := overlay.Create(ctx, fix)
	if err != nil {
		return "", fmt.Errorf("failed to create overlay file: %w", err)
	}
	return strings.Join([]string{
		"-cover",
		"-overlay=" + f,
		"-toolexec=" + path,
	}, " "), nil
}
