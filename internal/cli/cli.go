// Package cli parses the command line and runs the chosen subcommand.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
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
		if run != nil && looksLikeScript(name) {
			cmd, rest = run, args
		} else if strings.HasPrefix(name, "-") {
			commandIndex := 1
			for commandIndex < len(args) && strings.HasPrefix(args[commandIndex], "-") {
				commandIndex++
			}
			if commandIndex < len(args) {
				if candidate := a.lookup(args[commandIndex]); candidate != nil {
					cmd = candidate
					rest = append(append([]string(nil), args[:commandIndex]...), args[commandIndex+1:]...)
				} else if run != nil && looksLikeScript(args[commandIndex]) {
					cmd, rest = run, args
				}
			}
		}
		if cmd == nil {
			return usageErrorf("", "unknown command %q", name)
		}
	}

	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	cmd.SetFlags(fs)
	flagArgs, commandArgs := splitFlagArgs(fs, rest)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.printCommandHelp(a.env.Stdout, cmd)
			return nil
		}
		return usageErrorf(cmd.Name(), "%v", err)
	}
	return cmd.Execute(ctx, a.env, append(fs.Args(), commandArgs...))
}

// splitFlagArgs extracts registered flags wherever they occur before the
// command's -- separator, preserving the order of positional arguments.
func splitFlagArgs(fs *flag.FlagSet, args []string) (flags, commandArgs []string) {
	options := true
	positionalSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if options && arg == "--" {
			commandArgs = append(commandArgs, args[i+1:]...)
			break
		}
		if options && strings.HasPrefix(arg, "-") && arg != "-" {
			name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
			if fs.Lookup(name) != nil || !positionalSeen {
				flags = append(flags, arg)
				continue
			}
		}
		positionalSeen = true
		commandArgs = append(commandArgs, arg)
	}
	return flags, commandArgs
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
	fmt.Fprintf(w, "%[1]s - A Luau runtime.\n\nUsage:\n  %[1]s [flags] <command> [arguments]\n  %[1]s <command> [arguments] [flags]\n  %[1]s <script.luau> [arguments]\n\nCommands:\n", a.env.Name)

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
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprintln(w, "  -v, --version  Print the version")
	fmt.Fprintln(w, "  -h, --help  Print this help info")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Use \"%s help <command>\" for more about a command.\n", a.env.Name)
}

func (a *App) printCommandHelp(w io.Writer, cmd Command) {
	fmt.Fprintf(w, "Usage:\n  %s %s\n\n%s\n", a.env.Name, cmd.Usage(), cmd.Synopsis())

	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	cmd.SetFlags(fs)
	flags := make([]*flag.Flag, 0)
	fs.VisitAll(func(f *flag.Flag) { flags = append(flags, f) })
	if len(flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		fmt.Fprintln(w, "  -h, --help")
		fmt.Fprintln(w, "        Print this help info")
		printFlags(w, flags)
	}
}

func printFlags(w io.Writer, flags []*flag.Flag) {
	type group struct {
		usage string
		typ   string
		names []string
	}

	groups := make([]group, 0, len(flags))
	groupIndexes := make(map[string]int, len(flags))
	for _, f := range flags {
		valueName := flagValueName(f)
		key := f.Usage + "\x00" + valueName
		index, ok := groupIndexes[key]
		if !ok {
			index = len(groups)
			groupIndexes[key] = index
			groups = append(groups, group{usage: f.Usage, typ: valueName})
		}
		prefix := "--"
		if len(f.Name) == 1 {
			prefix = "-"
		}
		groups[index].names = append(groups[index].names, prefix+f.Name)
	}

	for _, group := range groups {
		fmt.Fprintf(w, "  %s", strings.Join(group.names, ", "))
		if group.typ != "" {
			fmt.Fprintf(w, " %s", group.typ)
		}
		fmt.Fprintf(w, "\n        %s\n", group.usage)
	}
}

func flagValueName(f *flag.Flag) string {
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return "value"
	}
	value := getter.Get()
	if _, ok := value.(bool); ok {
		return ""
	}
	if _, ok := value.(time.Duration); ok {
		return "duration"
	}
	if value == nil {
		return "value"
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Int:
		return "int"
	case reflect.Int8:
		return "int8"
	case reflect.Int16:
		return "int16"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Uint:
		return "uint"
	case reflect.Uint8:
		return "uint8"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Float32:
		return "float"
	case reflect.Float64:
		return "float"
	case reflect.String:
		return "string"
	default:
		return "value"
	}
}
