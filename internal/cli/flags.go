package cli

import (
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

var errFlagHelp = flag.ErrHelp

// Flags registers, parses, and formats a command's flags.
type Flags struct {
	set                *flag.FlagSet
	optionalValueFlags map[string]bool
}

func newFlags(name string) *Flags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Usage = func() {}
	return &Flags{set: set, optionalValueFlags: make(map[string]bool)}
}

// DurationVar registers a duration flag that requires a value when used.
// If the flag is omitted, target remains at its zero value. An empty alias
// disables the alias; otherwise, both names update target.
func (f *Flags) DurationVar(target *time.Duration, name, alias, usage string) {
	f.set.DurationVar(target, name, 0, usage)
	if alias != "" && alias != name {
		f.set.DurationVar(target, alias, 0, usage)
	}
}

// OptionalDurationVar registers a duration flag whose value may be omitted.
// A bare flag leaves target at defaultValue; provide an explicit value with
// --name=value. An empty alias disables the alias.
func (f *Flags) OptionalDurationVar(target *time.Duration, name, alias string, defaultValue time.Duration, usage string) {
	f.set.DurationVar(target, name, defaultValue, usage)
	if alias != "" && alias != name {
		f.set.DurationVar(target, alias, defaultValue, usage)
	}
	f.markOptionalValue(name, alias)
}

// IntVar registers an integer flag that requires a value when used.
// If the flag is omitted, target remains at its zero value. An empty alias
// disables the alias; otherwise, both names update target.
func (f *Flags) IntVar(target *int, name, alias, usage string) {
	f.set.IntVar(target, name, 0, usage)
	if alias != "" && alias != name {
		f.set.IntVar(target, alias, 0, usage)
	}
}

// OptionalIntVar registers an integer flag whose value may be omitted.
// A bare flag leaves target at defaultValue; provide an explicit value with
// --name=value. An empty alias disables the alias.
func (f *Flags) OptionalIntVar(target *int, name, alias string, defaultValue int, usage string) {
	f.set.IntVar(target, name, defaultValue, usage)
	if alias != "" && alias != name {
		f.set.IntVar(target, alias, defaultValue, usage)
	}
	f.markOptionalValue(name, alias)
}

// BoolVar registers a boolean flag. A bare flag sets target to true; use
// --name=false to explicitly set it to false. If omitted, target is
// defaultValue. An empty alias disables the alias.
func (f *Flags) BoolVar(target *bool, name, alias string, defaultValue bool, usage string) {
	f.set.BoolVar(target, name, defaultValue, usage)
	if alias != "" && alias != name {
		f.set.BoolVar(target, alias, defaultValue, usage)
	}
}

// StringVar registers a string flag that requires a value when used.
// If the flag is omitted, target remains empty. An empty alias disables the
// alias; otherwise, both names update target.
func (f *Flags) StringVar(target *string, name, alias, usage string) {
	f.set.StringVar(target, name, "", usage)
	if alias != "" && alias != name {
		f.set.StringVar(target, alias, "", usage)
	}
}

// OptionalStringVar registers a string flag whose value may be omitted.
// A bare flag leaves target at defaultValue; provide an explicit value with
// --name=value. An empty alias disables the alias.
func (f *Flags) OptionalStringVar(target *string, name, alias, defaultValue, usage string) {
	f.set.StringVar(target, name, defaultValue, usage)
	if alias != "" && alias != name {
		f.set.StringVar(target, alias, defaultValue, usage)
	}
	f.markOptionalValue(name, alias)
}

func (f *Flags) markOptionalValue(name, alias string) {
	f.optionalValueFlags[name] = true
	if alias != "" && alias != name {
		f.optionalValueFlags[alias] = true
	}
}

// Parse parses registered flag arguments. Bare optional-value flags leave
// their targets at the registered defaults. It returns an error for invalid
// flags or values.
func (f *Flags) Parse(args []string) error {
	parseArgs := make([]string, 0, len(args))
	for _, arg := range args {
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if !strings.Contains(arg, "=") && f.optionalValueFlags[name] {
			continue
		}
		parseArgs = append(parseArgs, arg)
	}
	return f.set.Parse(parseArgs)
}

// Args returns the non-flag arguments remaining after Parse.
func (f *Flags) Args() []string { return f.set.Args() }

// Lookup returns the registered flag with name, or nil if no such flag exists.
func (f *Flags) Lookup(name string) *flag.Flag { return f.set.Lookup(name) }

// splitArgs extracts registered flags wherever they occur before the command's
// -- separator, preserving the order of positional arguments.
func (f *Flags) splitArgs(args []string) (flags, commandArgs []string) {
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
			if f.Lookup(name) != nil || !positionalSeen {
				flags = append(flags, arg)
				continue
			}
		}
		positionalSeen = true
		commandArgs = append(commandArgs, arg)
	}
	return flags, commandArgs
}

// HasFlags reports whether any flags have been registered.
func (f *Flags) HasFlags() bool {
	hasFlags := false
	f.set.VisitAll(func(*flag.Flag) { hasFlags = true })
	return hasFlags
}

// Print writes the registered flags and their usage text to w. Optional
// values are shown in brackets.
func (f *Flags) Print(w io.Writer) {
	type group struct {
		usage    string
		typ      string
		optional bool
		names    []string
	}

	groups := make([]group, 0)
	groupIndexes := make(map[string]int)
	f.set.VisitAll(func(flag *flag.Flag) {
		valueName := flagValueName(flag)
		optional := f.optionalValueFlags[flag.Name]
		key := fmt.Sprintf("%s\x00%s\x00%t", flag.Usage, valueName, optional)
		index, ok := groupIndexes[key]
		if !ok {
			index = len(groups)
			groupIndexes[key] = index
			groups = append(groups, group{usage: flag.Usage, typ: valueName, optional: optional})
		}
		prefix := "--"
		if len(flag.Name) == 1 {
			prefix = "-"
		}
		groups[index].names = append(groups[index].names, prefix+flag.Name)
	})

	for _, group := range groups {
		fmt.Fprintf(w, "  %s", strings.Join(group.names, ", "))
		if group.typ != "" {
			if group.optional {
				fmt.Fprintf(w, " [%s]", group.typ)
			} else {
				fmt.Fprintf(w, " %s", group.typ)
			}
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
