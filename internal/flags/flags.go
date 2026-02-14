package flags

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func Run(ctx context.Context, tobariBinPath string, embedCode bool) (string, error) {
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
		toolexecValue = path + " --embed-code"
	}
	toolexecFlag := "-toolexec=" + toolexecValue
	if strings.Contains(toolexecValue, " ") {
		// Quote for GOFLAGS which uses SplitQuotedFields
		toolexecFlag = "'-toolexec=" + toolexecValue + "'"
	}
	return strings.Join([]string{
		"-cover",
		toolexecFlag,
	}, " "), nil
}
