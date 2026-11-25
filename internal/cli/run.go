package cli

import (
	"context"
	"strings"

	"github.com/ceilo-org/ceilo/internal/engine"
)

type runCmd struct {
	memory int
}

func (*runCmd) Name() string { return "run" }
func (*runCmd) Synopsis() string {
	return "Run a Luau script"
}
func (*runCmd) Usage() string { return "run <script> [flags]" }

func (c *runCmd) SetFlags(fs *Flags) {
	fs.IntVar(&c.memory, "memory", "m", "memory limit in MiB (0 means no limit)")
}

func (c *runCmd) Execute(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return usageErrorf("run", "run needs a script to run")
	}
	script := args[0]
	if !strings.HasSuffix(script, ".luau") {
		script += ".luau"
	}
	return engine.RunFile(ctx, engine.Options{
		Script:    script,
		Args:      args[1:],
		MemoryMiB: c.memory,
		Stdin:     env.Stdin,
		Stdout:    env.Stdout,
		Stderr:    env.Stderr,
		Runtime: struct {
			Name    string
			Version string
		}{
			Name:    env.Name,
			Version: env.Version,
		},
	})
}
