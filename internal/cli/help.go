package cli

import (
	"context"
	"flag"
)

type helpCmd struct{ app *App }

func (*helpCmd) Name() string              { return "help" }
func (*helpCmd) Synopsis() string          { return "Show help for a command" }
func (*helpCmd) Usage() string             { return "help [command]" }
func (*helpCmd) SetFlags(fs *flag.FlagSet) {}

func (c *helpCmd) Execute(ctx context.Context, env Env, args []string) error {
	switch len(args) {
	case 0:
		c.app.printHelp(env.Stdout)
		return nil
	case 1:
		cmd := c.app.lookup(args[0])
		if cmd == nil {
			return usageErrorf("help", "unknown command %q", args[0])
		}
		c.app.printCommandHelp(env.Stdout, cmd)
		return nil
	}
	return usageErrorf("help", "help takes at most one command")
}
