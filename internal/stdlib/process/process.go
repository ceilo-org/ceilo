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
	"github.com/Leoj030/go-luau/vm"
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

// Module returns the members of the "@std/process" module and registers the
// SignalHandle type in v, so call it before registering any module.
func Module(v *vm.VM, ctx context.Context, scheduler *stdtask.Scheduler, args []string, enqueue func(luau.Ref)) map[string]any {
	vm.MustRegisterType[*signalHandle](v, vm.TypeSpec{
		Name: "SignalHandle",
		Methods: map[string]any{
			"close": func(h *signalHandle) { h.close() },
		},
	})

	return map[string]any{
		// data
		"args": args,
		"env":  environment(),

		// plain functions
		"pid": os.Getpid,
		"homedir": func() (string, error) {
			dir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("process.homedir: %w", err)
			}
			return dir, nil
		},
		"cwd": func() (string, error) {
			dir, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("process.cwd: %w", err)
			}
			return dir, nil
		},
		"execPath": func() (string, error) {
			path, err := os.Executable()
			if err != nil {
				return "", fmt.Errorf("process.execPath: %w", err)
			}
			return path, nil
		},
		"exit": func(code float64) error {
			if math.IsNaN(code) || math.IsInf(code, 0) || math.Trunc(code) != code || code < 0 || code > 255 {
				return errors.New("process.exit: exit code must be an integer between 0 and 255")
			}
			os.Exit(int(code))
			return nil
		},

		// these wait for the child process, so they yield and stay low-level
		"run":    runWithContext(ctx, scheduler),
		"system": systemWithContext(ctx, scheduler),

		"onSignal": func(name string, callback *vm.Value) (*signalHandle, error) {
			if callback == nil || callback.Type() != "function" {
				return nil, errors.New("process.onSignal: argument 2 must be a function")
			}
			if enqueue == nil {
				return nil, errors.New("process.onSignal: signal callback dispatcher is unavailable")
			}
			sig, supported, err := signalValue(name)
			if err != nil {
				return nil, err
			}
			handle := &signalHandle{
				stop:    make(chan struct{}),
				signals: make(chan os.Signal, 1),
			}
			if supported {
				kept, err := callback.Retain() // an argument is released when this function returns
				if err != nil {
					return nil, err
				}
				osSignal.Notify(handle.signals, sig)
				go handle.forward(ctx, kept.Ref(), enqueue)
			}
			return handle, nil
		},
	}
}

// forward queues the callback each time the signal arrives, until the handle
// is closed or ctx ends.
func (h *signalHandle) forward(ctx context.Context, callback luau.Ref, enqueue func(luau.Ref)) {
	for {
		select {
		case <-ctx.Done():
			h.close()
			return
		case <-h.stop:
			return
		case _, ok := <-h.signals:
			if !ok {
				return
			}
			enqueue(callback)
		}
	}
}

// Finalize ensures a signal subscription is released if its handle is collected.
func (h *signalHandle) Finalize() { h.close() }

func (h *signalHandle) close() {
	h.stopOnce.Do(func() {
		osSignal.Stop(h.signals)
		close(h.stop)
	})
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
