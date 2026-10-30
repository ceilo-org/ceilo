// Package cli parses the command line and runs the chosen subcommand.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Env is everything the commands need from the outside world. Commands use
// it instead of os.Stdout and friends, so tests can capture their output.
type Env struct {
	Name    string
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type Command interface {
	Name() string
	Synopsis() string // one line for the command list
	Usage() string    // the usage line, such as "run [flags] <script> [arguments...]"

	// SetFlags registers the command's flags on fs. Commands keep the
	// values in their own fields.
	SetFlags(fs *flag.FlagSet)

	// Execute runs the command. args are what is left after the flags.
	Execute(ctx context.Context, env Env, args []string) error
}

// App holds the registered commands.
type App struct {
	env      Env
	commands []Command
}

// Run runs the program with args (the command line without the program
// name) and returns the exit code.
func Run(ctx context.Context, env Env, args []string) int {
	return New(env).Run(ctx, args)
}

// New creates an App with all the commands from commands.go.
func New(env Env) *App {
	a := &App{env: env}
	a.register(commands(a)...)
	return a
}

func (a *App) register(cmds ...Command) { a.commands = append(a.commands, cmds...) }

func (a *App) lookup(name string) Command {
	for _, c := range a.commands {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// Run runs the program and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	return a.exitCode(a.dispatch(ctx, args))
}

func (a *App) dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.printHelp(a.env.Stderr)
		return &ExitError{Code: 2}
	}

	name, rest := args[0], args[1:]
	switch name {
	case "-h", "-help", "--help":
		name = "help"
	case "-v", "-version", "--version":
		return printVersion(a.env, rest)
	}

	cmd := a.lookup(name)
	if cmd == nil {
		run := a.lookup("run")
		if run == nil || !looksLikeScript(name) {
			return usageErrorf("", "unknown command %q", name)
		}
		cmd, rest = run, args
	}

	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	cmd.SetFlags(fs)
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.printCommandHelp(a.env.Stdout, cmd)
			return nil
		}
		return usageErrorf(cmd.Name(), "%v", err)
	}
	return cmd.Execute(ctx, a.env, fs.Args())
}

// looksLikeScript reports whether name is a script file rather than a
// mistyped command.
func looksLikeScript(name string) bool {
	if strings.HasSuffix(name, ".luau") || strings.HasSuffix(name, ".lua") {
		return true
	}
	st, err := os.Stat(name)
	if err == nil && st.Mode().IsRegular() {
		return true
	}
	st, err = os.Stat(name + ".luau")
	return err == nil && st.Mode().IsRegular()
}

// exitCode prints err (if there is one) and returns the exit code for it.
func (a *App) exitCode(err error) int {
	if err == nil {
		return 0
	}

	var exit *ExitError
	if errors.As(err, &exit) {
		if exit.Err != nil {
			fmt.Fprintf(a.env.Stderr, "%s: %v\n", a.env.Name, exit.Err)
		}
		return exit.Code
	}

	var usage *UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(a.env.Stderr, "%s: %v\n", a.env.Name, usage)
		if usage.Command != "" {
			fmt.Fprintf(a.env.Stderr, "Run '%s help %s' for usage.\n", a.env.Name, usage.Command)
		} else {
			fmt.Fprintf(a.env.Stderr, "Run '%s help' for usage.\n", a.env.Name)
		}
		return 2
	}

	if errors.Is(err, context.Canceled) {
		return 130
	}

	fmt.Fprintf(a.env.Stderr, "%s: %v\n", a.env.Name, err)
	return 1
}

func (a *App) printHelp(w io.Writer) {
	fmt.Fprintf(w, "%[1]s - A Luau runtime.\n\nUsage:\n  %[1]s <command> [flags] [arguments]\n  %[1]s <script.luau> [arguments]\n\nCommands:\n", a.env.Name)

	cmds := append([]Command(nil), a.commands...)
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
	width := 0
	for _, c := range cmds {
		if n := len(c.Name()); n > width {
			width = n
		}
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.Name(), c.Synopsis())
	}
	fmt.Fprintf(w, "\nFlags:\n  -v, --version  Print the version\n\nUse \"%s help <command>\" for more about a command.\n", a.env.Name)
}

func (a *App) printCommandHelp(w io.Writer, cmd Command) {
	fmt.Fprintf(w, "Usage:\n  %s %s\n\n%s\n", a.env.Name, cmd.Usage(), cmd.Synopsis())

	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	cmd.SetFlags(fs)
	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprint(w, "\nFlags:\n")
		fs.SetOutput(w)
		fs.PrintDefaults()
	}
}
