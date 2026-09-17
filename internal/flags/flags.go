package flags

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrExcludeAnalysisWithPassedBlocksOnly reports a flag combination that can
// never take effect: with --passed-blocks-only no "places that should be
// passed" are derived, so there is no dependency analysis to exclude packages
// from.
var ErrExcludeAnalysisWithPassedBlocksOnly = errors.New("--exclude-analysis has no effect with --passed-blocks-only: specify only one of them")

// Options are the `tobari flags` settings that are forwarded to the toolexec
// invocation.
type Options struct {
	EmbedCode bool
	Tags      string
	// ExcludeAnalysis is the raw comma-separated prefix list.
	ExcludeAnalysis string
	// PassedBlocksOnly records only the blocks that were actually passed.
	PassedBlocksOnly bool
}

func Run(ctx context.Context, tobariBinPath string, opts Options) (string, error) {
	if opts.PassedBlocksOnly && opts.ExcludeAnalysis != "" {
		return "", ErrExcludeAnalysisWithPassedBlocksOnly
	}
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
	if opts.EmbedCode {
		toolexecValue += " --embed-code"
	}
	if opts.Tags != "" {
		toolexecValue += " --build-tags=" + opts.Tags
	}
	if opts.ExcludeAnalysis != "" {
		toolexecValue += " --exclude-analysis=" + opts.ExcludeAnalysis
	}
	if opts.PassedBlocksOnly {
		toolexecValue += " --passed-blocks-only"
	}
	toolexecFlag := "-toolexec=" + toolexecValue
	if strings.Contains(toolexecValue, " ") {
		// Quote for GOFLAGS which uses SplitQuotedFields
		toolexecFlag = "'-toolexec=" + toolexecValue + "'"
	}
	parts := []string{"-cover"}
	if opts.Tags != "" {
		parts = append(parts, "-tags="+opts.Tags)
	}
	parts = append(parts, toolexecFlag)
	return strings.Join(parts, " "), nil
}
