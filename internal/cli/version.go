package cli

import (
	"fmt"
	"runtime"
)

func printVersion(env Env, args []string) error {
	if len(args) > 0 {
		return usageErrorf("", "version flag takes no arguments")
	}
	fmt.Fprintf(env.Stdout, "%s %s (%s %s/%s)\n", env.Name, env.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
