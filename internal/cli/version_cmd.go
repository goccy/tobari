package cli

import (
	"fmt"

	"github.com/goccy/tobari/internal/version"
)

func (c *CLI) showVersion() error {
	v := "(devel)"
	if ver, err := version.Get(); err == nil && ver.Ver != "" {
		v = ver.Ver
	}
	_, err := fmt.Fprintf(c.stdout, "tobari version %s\n", v)
	return err
}
