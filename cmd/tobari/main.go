package main

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/tobari/internal/flags"
	"github.com/goccy/tobari/internal/tool"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return nil
	}

	tobariBinPath := args[0]
	if len(args) >= 2 && args[1] == "flags" {
		out, err := flags.Run(ctx, tobariBinPath)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprint(os.Stdout, string(out)); err != nil {
			return err
		}
		return nil
	}

	return tool.Handle(ctx, args)
}
