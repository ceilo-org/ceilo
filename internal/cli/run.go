package cli

import (
	"context"
	"os"
	"path/filepath"
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
	script, _ := resolveScript(args[0])
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

// resolveScript resolves name to an existing script file path, checking the
// direct path (with .luau appended if missing) and falling back to scripts/.
func resolveScript(name string) (string, bool) {
	hasExt := strings.HasSuffix(name, ".luau")
	withExt := name
	if !hasExt {
		withExt = name + ".luau"
	}

	if isRegularFile(withExt) {
		return withExt, true
	}
	if !hasExt && isRegularFile(name) {
		return name, true
	}

	if canCheckScriptsDir(name) {
		inScriptsWithExt := filepath.Join("scripts", withExt)
		if isRegularFile(inScriptsWithExt) {
			return inScriptsWithExt, true
		}
		if !hasExt {
			inScripts := filepath.Join("scripts", name)
			if isRegularFile(inScripts) {
				return inScripts, true
			}
		}
	}

	return withExt, false
}

func canCheckScriptsDir(name string) bool {
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return false
	}
	if name == "." || name == ".." {
		return false
	}
	for _, prefix := range []string{"./", "../", `.\`, `..\`} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
