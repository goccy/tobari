package flags

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func Run(ctx context.Context, tobariBinPath string, embedCode bool, tags, excludeAnalysis string) (string, error) {
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
	toolexecValue := path
	if embedCode {
		toolexecValue += " --embed-code"
	}
	if tags != "" {
		toolexecValue += " --build-tags=" + tags
	}
	if excludeAnalysis != "" {
		toolexecValue += " --exclude-analysis=" + excludeAnalysis
	}
	toolexecFlag := "-toolexec=" + toolexecValue
	if strings.Contains(toolexecValue, " ") {
		// Quote for GOFLAGS which uses SplitQuotedFields
		toolexecFlag = "'-toolexec=" + toolexecValue + "'"
	}
	parts := []string{"-cover"}
	if tags != "" {
		parts = append(parts, "-tags="+tags)
	}
	parts = append(parts, toolexecFlag)
	return strings.Join(parts, " "), nil
}
