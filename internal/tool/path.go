package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readRuntimePkg() (string, error) {
	f, err := os.ReadFile(runtimePkgPath())
	if err != nil {
		return "", fmt.Errorf("failed to read runtime package archive file path from %s: %w", runtimePkgPath(), err)
	}
	return strings.TrimSpace(string(f)), nil
}

func writeRuntimePkg(b []byte) error {
	p := runtimePkgPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return fmt.Errorf("failed to save runtime package %s: %w", p, err)
	}
	return nil
}

func readTobariPkgs(id int) map[string]string {
	f, err := os.ReadFile(tobariPkgPathWithID(id))
	if err != nil {
		return nil
	}

	var res map[string]string
	if err := json.Unmarshal(f, &res); err != nil {
		return nil
	}
	return res
}

func writeTobariPkgs(pkgs map[string]string) error {
	data, err := json.Marshal(pkgs)
	if err != nil {
		return fmt.Errorf("failed to encode tobari_pkg.json: %w", err)
	}

	path := tobariPkgPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func tobariPkgPath() string {
	return tobariPkgPathWithID(buildID())
}

func tobariPkgPathWithID(id int) string {
	return filepath.Join(buildPathWithID(id), "tobari_pkg.json")
}

func buildPathWithID(id int) string {
	return filepath.Join(tobariRoot(), fmt.Sprintf("build%d", id))
}

func runtimePkgPath() string {
	return filepath.Join(buildPath(), "runtime_pkg.txt")
}

func buildPath() string {
	return buildPathWithID(buildID())
}

// buildID returns a unique ID for each "go build" execution.
func buildID() int {
	// The "go tool" command is invoked by "go build",
	// so its parent process ID becomes the ID of the "go build" process.
	// In other words, it can be used as a unique ID for each "go build" execution.
	return os.Getppid()
}

func tobariRoot() string {
	return filepath.Join(os.TempDir(), "tobari")
}
