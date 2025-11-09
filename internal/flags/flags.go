package flags

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func Run(ctx context.Context, tobariBinPath string) (string, error) {
	if !filepath.IsAbs(tobariBinPath) {
		p, err := filepath.Abs(tobariBinPath)
		if err != nil {
			return "", fmt.Errorf("failed to get abs path from %s: %w", tobariBinPath, err)
		}
		tobariBinPath = p
	}
	f, err := overlay.Create(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create overlay file: %w", err)
	}
	return strings.Join([]string{
		"-cover",
		"-overlay=" + f,
		"-toolexec=" + tobariBinPath,
	}, " "), nil
}
