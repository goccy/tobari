package overlay

import (
	"os"
	"path/filepath"

	"github.com/goccy/tobari/internal/utils"
	"github.com/goccy/tobari/internal/version"
)

func OverlayPath() (string, error) {
	root, err := OverlayRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "overlay.json"), nil
}

func OverlayRootDir() (string, error) {
	goVer, err := utils.GoVersion()
	if err != nil {
		return "", err
	}
	ver, err := version.Get()
	if err != nil {
		return "", err
	}
	return filepath.Join(os.TempDir(), "tobari", "overlay", goVer, ver.ID()), nil
}
