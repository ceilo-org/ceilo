package cli

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/ceilo-org/ceilo/internal/engine"
)

type runCmd struct {
	timeout time.Duration
	memory  int
}

func (*runCmd) Name() string     { return "run" }
func (*runCmd) Synopsis() string { return "Run a Luau script" }
func (*runCmd) Usage() string    { return "run [flags] <script> [arguments...]" }

func (c *runCmd) SetFlags(fs *flag.FlagSet) {
	fs.DurationVar(&c.timeout, "timeout", 0, "stop the script after this long, such as 30s (0 means no limit)")
	fs.IntVar(&c.memory, "memory", 0, "memory limit in MiB (0 means no limit)")
}

func (c *runCmd) Execute(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return usageErrorf("run", "run needs a script to run")
	}
	if c.timeout < 0 || c.memory < 0 {
		return usageErrorf("run", "-timeout and -memory can't be negative")
	}
	script := args[0]
	if !strings.HasSuffix(script, ".luau") {
		script += ".luau"
	}
	return engine.RunFile(ctx, engine.Options{
		Script:    script,
		Args:      args[1:],
		Timeout:   c.timeout,
		MemoryMiB: c.memory,
		Stdin:     env.Stdin,
		Stdout:    env.Stdout,
		Stderr:    env.Stderr,
	})
}
