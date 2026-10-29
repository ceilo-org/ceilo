// Package process implements the "@std/process" Luau library.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	osSignal "os/signal"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Leoj030/go-luau/luau"
	stdtask "github.com/ceilo-org/ceilo/internal/stdlib/task"
)

type runOptions struct {
	cwd        string
	stdio      string
	env        map[string]string
	timeout    time.Duration
	timeoutSet bool
}

type signalHandle struct {
	stop     chan struct{}
	signals  chan os.Signal
	stopOnce sync.Once
}

// Finalize ensures a signal subscription is released if its handle is collected.
func (h *signalHandle) Finalize() { h.close() }

func (h *signalHandle) close() {
	h.stopOnce.Do(func() {
		osSignal.Stop(h.signals)
		close(h.stop)
	})
}

// Register registers the process module, including the current script arguments
// and environment, at path.
func Register(state *luau.State, path string, args []string, enqueue func(luau.Ref)) error {
	return RegisterWithContext(context.Background(), nil, state, path, args, enqueue)
}

// RegisterWithContext registers process operations with task-aware cancellation
// and signal subscriptions.
func RegisterWithContext(ctx context.Context, scheduler *stdtask.Scheduler, state *luau.State, path string, args []string, enqueue func(luau.Ref)) error {
	meta, err := buildSignalHandleMetatable(state)
	if err != nil {
		return err
	}
	functions := map[string]luau.Func{
		"homedir":  homedir,
		"cwd":      cwd,
		"run":      runWithContext(ctx, scheduler),
		"system":   systemWithContext(ctx, scheduler),
		"exit":     exit,
		"execPath": execPath,
		"pid":      pid,
		"onSignal": onSignal(ctx, enqueue, meta),
	}

	if err := state.NewTable(0, len(functions)+2); err != nil {
		return err
	}
	module := state.Top()
	for name, fn := range functions {
		if err := state.PushFunction(name, fn); err != nil {
			return err
		}
		if err := state.SetField(module, name); err != nil {
			return err
		}
	}
	if err := state.Push(args); err != nil {
		return err
	}
	if err := state.SetField(module, "args"); err != nil {
		return err
	}
	if err := state.Push(environment()); err != nil {
		return err
	}
	if err := state.SetField(module, "env"); err != nil {
		return err
	}
	return state.RegisterModuleTable(path)
}

func environment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func homedir(c *luau.State) (int, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("process.homedir: %w", err)
	}
	return 1, c.Push(dir)
}

func cwd(c *luau.State) (int, error) {
	dir, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("process.cwd: %w", err)
	}
	return 1, c.Push(dir)
}

func execPath(c *luau.State) (int, error) {
	path, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("process.execPath: %w", err)
	}
	return 1, c.Push(path)
}

func pid(c *luau.State) (int, error) {
	return 1, c.Push(os.Getpid())
}

func exit(c *luau.State) (int, error) {
	if c.Top() != 1 {
		return 0, errors.New("process.exit: expected 1 exit code")
	}
	value, err := c.Get(1)
	code, ok := value.(float64)
	if err != nil || !ok || math.IsNaN(code) || math.IsInf(code, 0) || math.Trunc(code) != code || code < 0 || code > 255 {
		return 0, errors.New("process.exit: exit code must be an integer between 0 and 255")
	}
	os.Exit(int(code))
	return 0, nil
}

func runWithContext(ctx context.Context, scheduler *stdtask.Scheduler) luau.Func {
	return func(c *luau.State) (int, error) {
		if c.Top() < 1 || c.Top() > 2 {
			return 0, fmt.Errorf("process.run: expected 1 or 2 arguments, got %d", c.Top())
		}
		args, err := stringArrayArg(c, "run", 1)
		if err != nil {
			return 0, err
		}
		if len(args) == 0 || args[0] == "" {
			return 0, errors.New("process.run: args must include a program")
		}
		options, err := parseOptions(c, 2, false)
		if err != nil {
			return 0, fmt.Errorf("process.run: %w", err)
		}
		if scheduler != nil {
			return scheduler.Await(c, func(taskCtx context.Context, resume func(func(*luau.Thread) (int, error), error)) {
				go func() {
					result, err := execute(taskCtx, args[0], args[1:], options.runOptions)
					if err != nil {
						resume(nil, fmt.Errorf("process.run: %w", err))
						return
					}
					resume(func(th *luau.Thread) (int, error) {
						return 1, pushResult(th, result)
					}, nil)
				}()
			})
		}
		result, err := execute(ctx, args[0], args[1:], options.runOptions)
		if err != nil {
			return 0, fmt.Errorf("process.run: %w", err)
		}
		return 1, pushResult(c, result)
	}
}

func systemWithContext(ctx context.Context, scheduler *stdtask.Scheduler) luau.Func {
	return func(c *luau.State) (int, error) {
		if c.Top() < 1 || c.Top() > 2 {
			return 0, fmt.Errorf("process.system: expected 1 or 2 arguments, got %d", c.Top())
		}
		command, err := stringArg(c, "system", 1)
		if err != nil {
			return 0, err
		}
		options, err := parseOptions(c, 2, true)
		if err != nil {
			return 0, fmt.Errorf("process.system: %w", err)
		}
		executable, args, err := shellCommand(options.system, command)
		if err != nil {
			return 0, fmt.Errorf("process.system: %w", err)
		}
		if scheduler != nil {
			return scheduler.Await(c, func(taskCtx context.Context, resume func(func(*luau.Thread) (int, error), error)) {
				go func() {
					result, err := execute(taskCtx, executable, args, options.runOptions)
					if err != nil {
						resume(nil, fmt.Errorf("process.system: %w", err))
						return
					}
					resume(func(th *luau.Thread) (int, error) {
						return 1, pushResult(th, result)
					}, nil)
				}()
			})
		}
		result, err := execute(ctx, executable, args, options.runOptions)
		if err != nil {
			return 0, fmt.Errorf("process.system: %w", err)
		}
		return 1, pushResult(c, result)
	}
}

type systemOptions struct {
	runOptions
	system string
}

func parseOptions(c *luau.State, index int, allowSystem bool) (systemOptions, error) {
	options := systemOptions{runOptions: runOptions{stdio: "default"}}
	if c.TypeName(index) == "no value" || c.TypeName(index) == "nil" {
		return options, nil
	}
	value, err := c.Get(index)
	if err != nil {
		return options, err
	}
	fields, ok := value.(map[string]any)
	if !ok {
		if empty, isEmptyArray := value.([]any); isEmptyArray && len(empty) == 0 {
			return options, nil
		}
		return options, errors.New("options must be a table")
	}
	for key := range fields {
		switch key {
		case "system":
			if !allowSystem {
				return options, errors.New(`unknown option "system"`)
			}
		case "cwd", "stdio", "env", "timeout":
		default:
			return options, fmt.Errorf("unknown option %q", key)
		}
	}
	if value, exists := fields["system"]; exists && value != nil {
		system, ok := value.(string)
		if !ok {
			return options, errors.New("system must be a string")
		}
		options.system = system
	}
	if value, exists := fields["cwd"]; exists && value != nil {
		cwd, ok := value.(string)
		if !ok {
			return options, errors.New("cwd must be a string")
		}
		options.cwd = cwd
	}
	if value, exists := fields["stdio"]; exists && value != nil {
		stdio, ok := value.(string)
		if !ok || (stdio != "default" && stdio != "inherit" && stdio != "none") {
			return options, errors.New(`stdio must be "default", "inherit", or "none"`)
		}
		options.stdio = stdio
	}
	if value, exists := fields["env"]; exists && value != nil {
		env, ok := value.(map[string]any)
		if values, emptyTable := value.([]any); emptyTable && len(values) == 0 {
			env, ok = map[string]any{}, true
		}
		if !ok {
			return options, errors.New("env must be a table of string values")
		}
		options.env = make(map[string]string, len(env))
		for key, value := range env {
			value, ok := value.(string)
			if !ok {
				return options, fmt.Errorf("env[%q] must be a string", key)
			}
			options.env[key] = value
		}
	}
	if value, exists := fields["timeout"]; exists && value != nil {
		seconds, ok := value.(float64)
		if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt64)/float64(time.Second) {
			return options, errors.New("timeout must be a non-negative finite number of seconds")
		}
		options.timeout = time.Duration(seconds * float64(time.Second))
		options.timeoutSet = true
	}
	return options, nil
}

func stringArrayArg(c *luau.State, fn string, index int) ([]string, error) {
	if c.TypeName(index) != "table" {
		return nil, fmt.Errorf("process.%s: argument %d must be an array of strings", fn, index)
	}
	value, err := c.Get(index)
	if err != nil {
		return nil, fmt.Errorf("process.%s: %w", fn, err)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("process.%s: argument %d must be an array of strings", fn, index)
	}
	args := make([]string, len(items))
	for i, item := range items {
		arg, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("process.%s: argument %d item %d must be a string", fn, index, i+1)
		}
		args[i] = arg
	}
	return args, nil
}

func stringArg(c *luau.State, fn string, index int) (string, error) {
	value, err := c.Get(index)
	if err != nil {
		return "", fmt.Errorf("process.%s: argument %d must be a string", fn, index)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("process.%s: argument %d must be a string", fn, index)
	}
	return text, nil
}

func onSignal(ctx context.Context, enqueue func(luau.Ref), meta luau.Ref) luau.Func {
	return func(c *luau.State) (int, error) {
		if enqueue == nil {
			return 0, errors.New("process.onSignal: signal callback dispatcher is unavailable")
		}
		if c.Top() != 2 {
			return 0, fmt.Errorf("process.onSignal: expected 2 arguments, got %d", c.Top())
		}
		name, err := stringArg(c, "onSignal", 1)
		if err != nil {
			return 0, err
		}
		if c.TypeName(2) != "function" {
			return 0, errors.New("process.onSignal: argument 2 must be a function")
		}
		sig, supported, err := signalValue(name)
		if err != nil {
			return 0, err
		}
		handle := &signalHandle{
			stop:    make(chan struct{}),
			signals: make(chan os.Signal, 1),
		}
		if supported {
			callback, err := c.Ref(2)
			if err != nil {
				return 0, err
			}
			osSignal.Notify(handle.signals, sig)
			go func() {
				for {
					select {
					case <-ctx.Done():
						handle.close()
						return
					case <-handle.stop:
						return
					case _, ok := <-handle.signals:
						if !ok {
							return
						}
						enqueue(callback)
					}
				}
			}()
		}
		if err := c.PushUserdata(handle); err != nil {
			handle.close()
			return 0, err
		}
		if err := c.PushRef(meta); err != nil {
			handle.close()
			return 0, err
		}
		if err := c.SetMetatable(-2); err != nil {
			handle.close()
			return 0, err
		}
		return 1, nil
	}
}

func buildSignalHandleMetatable(state *luau.State) (luau.Ref, error) {
	if err := state.NewTable(0, 2); err != nil {
		return 0, err
	}
	if err := state.NewTable(0, 1); err != nil {
		return 0, err
	}
	if err := state.PushFunction("close", closeSignal); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "close"); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "__index"); err != nil {
		return 0, err
	}
	if err := state.Push("The metatable is locked!"); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "__metatable"); err != nil {
		return 0, err
	}
	ref, err := state.Ref(-1)
	if err != nil {
		return 0, err
	}
	if err := state.Pop(1); err != nil {
		return 0, err
	}
	return ref, nil
}

func closeSignal(c *luau.State) (int, error) {
	value, ok := c.ToUserdata(1)
	handle, valid := value.(*signalHandle)
	if !ok || !valid {
		return 0, errors.New("process.signal.close: expected a signal handle")
	}
	handle.close()
	return 0, nil
}

func pushResult(c interface{ Push(any) error }, result processResult) error {
	return c.Push(map[string]any{
		"stdout":   result.stdout,
		"stderr":   result.stderr,
		"ok":       result.ok,
		"exitcode": result.exitcode,
		"signal":   result.signal,
		"timedOut": result.timedOut,
	})
}

type processResult struct {
	stdout   string
	stderr   string
	ok       bool
	exitcode int
	signal   any
	timedOut bool
}

func execute(parent context.Context, program string, args []string, options runOptions) (processResult, error) {
	ctx := parent
	cancel := func() {}
	if options.timeoutSet {
		ctx, cancel = context.WithTimeout(parent, options.timeout)
	}
	defer cancel()

	cmd := newCommand(ctx, program, args)
	cmd.Dir = options.cwd
	if options.env != nil {
		cmd.Env = make([]string, 0, len(options.env))
		for key, value := range options.env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}

	var stdout, stderr bytes.Buffer
	switch options.stdio {
	case "default":
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	case "inherit":
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	case "none":
		cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(""), io.Discard, io.Discard
	default:
		return processResult{}, fmt.Errorf("invalid stdio mode %q", options.stdio)
	}

	err := cmd.Run()
	if parent.Err() != nil {
		return processResult{}, luau.ErrInterrupted
	}
	result := processResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitcode: 0,
		timedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	if err == nil {
		result.ok = !result.timedOut
		return result, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		if result.timedOut && cmd.Process != nil {
			result.exitcode = timeoutExitCode()
			result.signal = timeoutSignalName()
			return result, nil
		}
		return processResult{}, err
	}
	result.exitcode = exitErr.ExitCode()
	result.signal = processSignalName(exitErr)
	if result.exitcode < 0 {
		if signal, ok := result.signal.(string); ok {
			result.exitcode = 128 + signalExitCode(signal)
		}
	}
	result.ok = result.exitcode == 0 && !result.timedOut
	return result, nil
}

func shellCommand(system, command string) (string, []string, error) {
	if system == "" {
		system = defaultShell()
	}
	if system == "" {
		return "", nil, errors.New("no system shell is configured")
	}
	if runtime.GOOS == "windows" {
		return system, []string{"/C", command}, nil
	}
	return system, []string{"-c", command}, nil
}
