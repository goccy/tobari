package cli

import (
	"fmt"
	"runtime/debug"
	"strings"
)

func (c *CLI) showVersion() error {
	ver := "(devel)"

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if v := buildInfo.Main.Version; v != "" && strings.HasPrefix(v, "v") {
			ver = v
		}
	}

	fmt.Fprintf(c.stdout, "tobari version %s\n", ver)
	return nil
}
