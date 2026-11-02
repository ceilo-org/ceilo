// Package engine owns the Luau State and runs scripts.
package engine

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Leoj030/go-luau/luau"
	"github.com/Leoj030/go-luau/vm"

	ceilobcrypt "github.com/ceilo-org/ceilo/internal/ceilolib/bcrypt"
	ceilonet "github.com/ceilo-org/ceilo/internal/ceilolib/net"

	stdfs "github.com/ceilo-org/ceilo/internal/stdlib/fs"
	stdio "github.com/ceilo-org/ceilo/internal/stdlib/io"
	stdjson "github.com/ceilo-org/ceilo/internal/stdlib/json"
	stdprocess "github.com/ceilo-org/ceilo/internal/stdlib/process"
	stdtask "github.com/ceilo-org/ceilo/internal/stdlib/task"
	stdtime "github.com/ceilo-org/ceilo/internal/stdlib/time"
)

type Options struct {
	Script    string
	Args      []string
	Timeout   time.Duration
	MemoryMiB int
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Runtime   struct {
		Name    string
		Version string
	}
}

var LibPrefix = "@std/"
var CeiloPrefix = "@ceilo/"

//go:embed prelude.luau
var prelude string

// RunFile runs a script file.
func RunFile(ctx context.Context, opts Options) error {
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	fail := func(format string, a ...any) int {
		out.Flush()
		fmt.Fprintf(os.Stderr, format+"\n", a...)
		return 1
	}
	report := func(err error) {
		if ctx.Err() != nil {
			return
		}
		if le, ok := errors.AsType[*luau.Error](err); ok {
			fail("%s\n%s", le.Error(), le.Traceback)
		} else {
			fail("error: %v", err)
		}
	}

	src, err := os.ReadFile(opts.Script)
	if err != nil {
		return fmt.Errorf("cannot read script: %w", err)
	}

	var vmOptions []vm.Option
	if opts.MemoryMiB > 0 {
		maxInt := int(^uint(0) >> 1)
		if opts.MemoryMiB > maxInt/(1<<20) {
			return fmt.Errorf("error: memory limit too large")
		}
		vmOptions = append(vmOptions, vm.WithMemoryLimit(opts.MemoryMiB<<20))
	}
	machine, err := vm.New(vmOptions...)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	defer machine.Close()
	L := machine.State()
	stopWatching := L.WatchContext(ctx)
	defer stopWatching()

	newTask := stdtask.New(stdtask.Options{OnError: report, BeforeIdle: func() { out.Flush() }, Context: ctx})
	newTask.BindState(L)

	disk, err := NewDiskRequirer(newTask.ModuleFinished)
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}

	if err := machine.EnableRequireWith(disk); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	serverModule := ceilonet.ServerModule(ctx, newTask, L, report)
	clientModule := ceilonet.ClientModule(ctx)
	modules := map[string]map[string]any{
		LibPrefix + "task":          newTask.Module(),
		LibPrefix + "fs":            stdfs.ModuleWithContext(ctx, newTask),
		LibPrefix + "io":            stdio.Module(),
		LibPrefix + "json":          stdjson.Module(),
		LibPrefix + "time":          stdtime.Module(machine),
		LibPrefix + "time/duration": stdtime.DurationModule(),
		LibPrefix + "process":       stdprocess.Module(machine, ctx, newTask, opts.Args, newTask.QueueCallback),

		CeiloPrefix + "bcrypt":     ceilobcrypt.Module(),
		CeiloPrefix + "net/client": clientModule,
		CeiloPrefix + "net/server": {"__call": serverModule["server"]},
		CeiloPrefix + "net": {
			"server": serverModule["server"],
			"client": map[string]any{"request": clientModule["request"]},
		},
	}

	for path, members := range modules {
		if err := machine.Module(path, members); err != nil {
			return fmt.Errorf("module %s: %w", path, err)
		}
	}

	if err := machine.Do(prelude); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	if err := machine.Do(fmt.Sprintf("_G._VERSION = '%s %s'", opts.Runtime.Name, opts.Runtime.Version)); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	bc, err := luau.Compile(string(src))

	if err != nil {
		if le, ok := errors.AsType[*luau.Error](err); ok {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		os.Exit(1)
	}

	return ctx.Err()
}
