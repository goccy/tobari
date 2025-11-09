package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Create(ctx context.Context) (string, error) {
	src := []byte(`
package runtime

func GID()  uint64 { return getg().goid }
func PGID() uint64 { return getg().parentGoid }
`)

	root := filepath.Join(os.TempDir(), "tobari")
	ver, err := goVersion(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, ver), 0o755); err != nil {
		return "", err
	}
	runtimeFile := filepath.Join(root, ver, "runtime.go")
	if err := os.WriteFile(runtimeFile, src, 0o600); err != nil {
		return "", err
	}
	runtimePkgDir, err := pkgPath(ctx, "runtime")
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]interface{}{
		"Replace": map[string]string{
			filepath.Join(
				runtimePkgDir,
				fmt.Sprintf("rumtime_%d.go", time.Now().UnixNano()),
			): runtimeFile,
		},
	})
	if err != nil {
		return "", err
	}
	overlayPath := filepath.Join(root, ver, "overlay.json")
	if err := os.WriteFile(overlayPath, b, 0o600); err != nil {
		return "", err
	}
	return overlayPath, nil
}

func pkgPath(ctx context.Context, pkg string) (string, error) {
	root, err := goRoot(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "src", pkg), nil
}

func goRoot(ctx context.Context) (string, error) {
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.CommandContext(ctx, cmd, "env", "GOROOT").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func goVersion(ctx context.Context) (string, error) {
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.CommandContext(ctx, cmd, "env", "GOVERSION").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
