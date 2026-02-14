package tool

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/cover"
)

func handleCover(ctx context.Context, toolPath string, args []string, embedCode bool) error {
	if !isCoverVOption(args) && !containsSelfPackageFile(args) {
		return cover.Run(ctx, args, embedCode)
	}
	runCommand(toolPath, args)
	return nil
}

func isCoverVOption(args []string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], "-V")
}

func containsSelfPackageFile(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/tobari/") && strings.HasSuffix(arg, ".go") {
			if strings.Contains(arg, "/tobari/internal") {
				return true
			}
			if filepath.Base(arg) == "tobari.go" {
				return true
			}
		}
	}
	return false
}
