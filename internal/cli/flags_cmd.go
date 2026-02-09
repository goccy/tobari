package cli

import (
	"context"
	"fmt"

	"github.com/goccy/tobari/internal/flags"
)

func (c *CLI) runFlagsCmd(ctx context.Context, tobariBinPath string) error {
	out, err := flags.Run(ctx, tobariBinPath)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(c.stdout, out)
	return err
}
