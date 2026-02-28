package version

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

// ver is set at build time via ldflags:
//
//	-X github.com/goccy/tobari/internal/version.ver=v0.1.0
var ver string

type Version struct {
	Ver       string
	LocalPath string
}

func (v *Version) ID() string {
	if v.LocalPath != "" {
		sha := sha256.Sum256([]byte(v.LocalPath))
		hash := hex.EncodeToString(sha[:])
		return string(hash[:7])
	}
	return v.Ver
}

// Get determines the version of tobari binary being used.
func Get() (*Version, error) {
	root := repoRoot()
	if _, err := os.Stat(root); err == nil {
		return &Version{LocalPath: root}, nil
	}

	if ver != "" {
		return &Version{Ver: ver}, nil
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, fmt.Errorf("failed to read build info")
	}
	if strings.Contains(buildInfo.Main.Version, "devel") {
		return &Version{Ver: "main"}, nil
	}

	if buildInfo.Main.Version != "" {
		return &Version{Ver: buildInfo.Main.Version}, nil
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			// Use short commit hash for development builds
			return &Version{Ver: setting.Value[:7]}, nil
		}
	}
	return nil, errors.New("failed to get tobari version from binary")
}

func repoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	p := filepath.Join(filepath.Dir(filename), "..", "..")
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return ""
		}
		return abs
	}
	return p
}
