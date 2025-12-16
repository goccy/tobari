package overlay

import (
	"context"
	"os"
	"path/filepath"

	"github.com/goccy/tobari/internal/version"
)

func OverlayPath(ctx context.Context) (string, error) {
	root, err := OverlayRootDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "overlay.json"), nil
}

func OverlayRootDir(ctx context.Context) (string, error) {
	goVer, err := goVersion(ctx)
	if err != nil {
		return "", err
	}
	ver, err := version.Get()
	if err != nil {
		return "", err
	}
	return filepath.Join(os.TempDir(), "tobari", "overlay", goVer, ver.ID()), nil
}
