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

	"golang.org/x/mod/module"
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
	if ver != "" {
		return &Version{Ver: ver}, nil
	}

	// Prefer tagged/release version when available ( e.g. `go install ...@vX.Y.Z` ),
	// even if compile-time source path still exists on disk ( module cache ).
	// Exclude pseudo-versions (e.g. v0.0.0-20260228130905-0bb47f48a0ec) which
	// Go 1.25+ stamps into Main.Version for VCS-tracked main-module builds.
	buildInfo, ok := debug.ReadBuildInfo()
	if ok && buildInfo.Main.Version != "" &&
		!strings.Contains(buildInfo.Main.Version, "devel") &&
		!module.IsPseudoVersion(strings.TrimSuffix(buildInfo.Main.Version, "+dirty")) {
		return &Version{Ver: buildInfo.Main.Version}, nil
	}

	root := repoRoot()
	if _, err := os.Stat(root); err == nil {
		return &Version{LocalPath: root}, nil
	}

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
