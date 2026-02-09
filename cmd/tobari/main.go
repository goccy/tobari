package main

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/tobari/internal/cli"
)

func main() {
	ctx := context.Background()
	c := cli.New()
	if err := c.Run(ctx, os.Args); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
}
