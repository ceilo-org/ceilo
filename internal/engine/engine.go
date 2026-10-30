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
	stopWatching := L.WatchContext(ctx)
	defer stopWatching()

	newTask := stdtask.New(stdtask.Options{OnError: report, BeforeIdle: func() { out.Flush() }, Context: ctx})
	newTask.BindState(L)

	disk, err := NewDiskRequirer(newTask.ModuleFinished)
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}

	if err := L.OpenRequire(disk); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	serverModule := ceilonet.ServerModule(ctx, newTask, L, report)
	clientModule := ceilonet.ClientModule(ctx)
	modules := map[string]map[string]luau.Func{
		LibPrefix + "task": newTask.Module(),
		LibPrefix + "fs":   stdfs.ModuleWithContext(ctx, newTask),
		LibPrefix + "io":   stdio.Module(),
		LibPrefix + "json": stdjson.Module(),

		CeiloPrefix + "bcrypt":     ceilobcrypt.Module(),
		CeiloPrefix + "net/client": clientModule,
	}

	for path, funcs := range modules {
		if err := L.RegisterModule(path, funcs); err != nil {
			return fmt.Errorf("error: %v", err)
		}
	}
	if err := registerNetModule(L, serverModule["server"], clientModule["request"]); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	if err := registerCallableModule(L, CeiloPrefix+"net/server", "server", serverModule["server"]); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	if err := stdprocess.RegisterWithContext(ctx, newTask, L, LibPrefix+"process", opts.Args, newTask.QueueCallback); err != nil {
		return fmt.Errorf("error: %v", err)
	}
	if err := stdtime.RegisterModules(L, LibPrefix+"time"); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	pre, err := luau.Compile(prelude)
	if err == nil {
		err = L.Load(pre, "=prelude")
	}
	if err == nil {
		_, err = L.Call()
	}
	if err != nil {
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

func registerCallableModule(state *luau.State, path, name string, fn luau.Func) error {
	if err := state.NewTable(0, 1); err != nil {
		return err
	}
	if err := state.PushFunction(name, fn); err != nil {
		return err
	}
	if err := state.SetField(-2, name); err != nil {
		return err
	}
	if err := state.NewTable(0, 1); err != nil {
		return err
	}
	if err := state.PushFunction("__call", fn); err != nil {
		return err
	}
	if err := state.SetField(-2, "__call"); err != nil {
		return err
	}
	if err := state.SetMetatable(-2); err != nil {
		return err
	}
	return state.RegisterModuleTable(path)
}

func registerNetModule(state *luau.State, server, clientRequest luau.Func) error {
	if err := state.NewTable(0, 2); err != nil {
		return err
	}
	if err := state.PushFunction("server", server); err != nil {
		return err
	}
	if err := state.SetField(-2, "server"); err != nil {
		return err
	}
	if err := state.NewTable(0, 1); err != nil {
		return err
	}
	if err := state.PushFunction("request", clientRequest); err != nil {
		return err
	}
	if err := state.SetField(-2, "request"); err != nil {
		return err
	}
	if err := state.SetField(-2, "client"); err != nil {
		return err
	}
	return state.RegisterModuleTable(CeiloPrefix + "net")
}
