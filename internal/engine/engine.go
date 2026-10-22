// Package engine owns the Luau State and runs scripts.
package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Leoj030/go-luau/luau"
	stdfs "github.com/ceilo-org/ceilo/internal/stdlib/fs"
	stdio "github.com/ceilo-org/ceilo/internal/stdlib/io"
	stdjson "github.com/ceilo-org/ceilo/internal/stdlib/json"
	stdprocess "github.com/ceilo-org/ceilo/internal/stdlib/process"
	stdtask "github.com/ceilo-org/ceilo/internal/stdlib/task"
	stdtime "github.com/ceilo-org/ceilo/internal/stdlib/time"
)

// Options says what to run and with what limits.
type Options struct {
	Script    string
	Args      []string
	Timeout   time.Duration
	MemoryMiB int
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

var LibPrefix = "@ceilo/"

// RunFile runs a script file. This is a placeholder: it only reads the file
// and says what it would do. The next step replaces it with the real runtime.
func RunFile(ctx context.Context, opts Options) error {
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	fail := func(format string, a ...any) int {
		out.Flush()
		fmt.Fprintf(os.Stderr, format+"\n", a...)
		return 1
	}
	report := func(err error) {
		var le *luau.Error
		if errors.As(err, &le) {
			fail("%s\n%s", le.Error(), le.Traceback)
		} else {
			fail("error: %v", err)
		}
	}

	src, err := os.ReadFile(opts.Script)
	if err != nil {
		return fmt.Errorf("cannot read script: %w", err)
	}

	L, err := luau.NewState()

	if err != nil {
		panic(err)
	}

	defer L.Close()

	newTask := stdtask.New(stdtask.Options{OnError: report, BeforeIdle: func() { out.Flush() }})
	newTask.BindState(L)

	disk, err := NewDiskRequirer(newTask.ModuleFinished)
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}

	if err := L.OpenRequire(disk); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	modules := map[string]map[string]luau.Func{
		LibPrefix + "task": newTask.Module(),
		LibPrefix + "fs":   stdfs.Module(),
		LibPrefix + "io":   stdio.Module(),
		LibPrefix + "json": stdjson.Module(),
	}

	for path, funcs := range modules {
		if err := L.RegisterModule(path, funcs); err != nil {
			return fmt.Errorf("error: %v", err)
		}
	}
	if err := stdprocess.Register(L, LibPrefix+"process", opts.Args, newTask.QueueCallback); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	if err := stdtime.RegisterModules(L, LibPrefix+"time"); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	bc, err := luau.Compile(string(src))

	if err != nil {
		var le *luau.Error
		if errors.As(err, &le) {
			return fmt.Errorf("%s:%d:%d: %s", le.Chunk, le.Line, le.Column, le.Message)
		}
		return fmt.Errorf("error: %v", err)
	}

	th, err := L.NewThread()
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}
	defer th.Close()

	if err := th.Load(bc, disk.Chunkname(opts.Script)); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	for _, arg := range opts.Args {
		if err := th.Push(arg); err != nil {
			return fmt.Errorf("error: %v", err)
		}
	}

	newTask.Start(th, len(opts.Args))
	if !newTask.Run() {
		os.Exit(1)
	}

	return nil
}
