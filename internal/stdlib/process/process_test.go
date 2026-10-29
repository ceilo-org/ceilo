//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Leoj030/go-luau/luau"
	stdtask "github.com/ceilo-org/ceilo/internal/stdlib/task"
)

func newTestRuntime(t *testing.T, args []string) (*luau.State, *stdtask.Scheduler) {
	return newTestRuntimeWithContext(t, context.Background(), args)
}

func newTestRuntimeWithContext(t *testing.T, ctx context.Context, args []string) (*luau.State, *stdtask.Scheduler) {
	t.Helper()
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	scheduler := stdtask.New(stdtask.Options{Context: ctx, OnError: func(err error) {
		if ctx.Err() != nil {
			return
		}
		t.Errorf("scheduled callback: %v", err)
	}})
	scheduler.BindState(state)
	if err := state.RegisterModule("@std/task", scheduler.Module()); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithContext(ctx, scheduler, state, "@std/process", args, scheduler.QueueCallback); err != nil {
		t.Fatal(err)
	}
	return state, scheduler
}

func runScript(t *testing.T, state *luau.State, scheduler *stdtask.Scheduler, source string) {
	t.Helper()
	done := startScript(t, state, scheduler, source)
	if !<-done {
		t.Fatal("scheduler reported failure")
	}
}

func startScript(t *testing.T, state *luau.State, scheduler *stdtask.Scheduler, source string) <-chan bool {
	t.Helper()
	bytecode, err := luau.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := state.NewThread()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(thread.Close)
	if err := thread.Load(bytecode, "=process-test"); err != nil {
		t.Fatal(err)
	}
	scheduler.Start(thread, 0)
	done := make(chan bool, 1)
	go func() { done <- scheduler.Run() }()
	return done
}

func TestProcessModuleRunAndOptions(t *testing.T) {
	state, scheduler := newTestRuntime(t, []string{"first", "second"})
	runScript(t, state, scheduler, `
		local process = require("@std/process")
		assert(process.args[1] == "first" and process.args[2] == "second")
		assert(process.cwd() ~= "" and process.homedir() ~= "")
		assert(process.execPath() ~= "" and process.pid() > 0)

		local result = process.run({
			"/bin/sh", "-c", "printf '%s:%s' \"$PROCESS_TEST\" \"$PWD\"; printf err >&2; exit 7"
		}, {
			env = {PROCESS_TEST = "value"},
			cwd = "/",
		})
		assert(result.stdout == "value:/")
		assert(result.stderr == "err")
		assert(result.ok == false and result.exitcode == 7)
		assert(result.signal == nil and result.timedOut == false)
		local noTimeout = process.run({"/bin/sh", "-c", "printf ok"}, {timeout = nil, env = nil})
		assert(noTimeout.ok and noTimeout.stdout == "ok")
		local commandOk = pcall(function()
			process.run({"/ceilo/test/missing-command"})
		end)
		assert(commandOk == false)

		local discarded = process.run({"/bin/sh", "-c", "printf hidden; printf hidden >&2"}, {stdio = "none"})
		assert(discarded.ok and discarded.stdout == "" and discarded.stderr == "")

		local inherited = process.system("printf shell", {system = "/bin/sh"})
		assert(inherited.ok and inherited.stdout == "shell")
	`)
}

func TestProcessTypedefCompiles(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "typedefs", "std", "process.luau"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := luau.Compile(string(source)); err != nil {
		t.Fatalf("process typedef does not compile: %v", err)
	}
}

func TestProcessRunTimeoutKillsProcessGroup(t *testing.T) {
	state, scheduler := newTestRuntime(t, nil)
	runScript(t, state, scheduler, `
		local result = require("@std/process").run(
			{"/bin/sh", "-c", "sleep 10 & wait"},
			{timeout = 0.05}
		)
		assert(result.timedOut == true)
		assert(result.ok == false)
		assert(result.signal == "SIGKILL", tostring(result.signal) .. ":" .. tostring(result.exitcode) .. ":" .. tostring(result.timedOut))
	`)
}

func TestProcessRunAndSystemStopOnContextCancellation(t *testing.T) {
	tests := map[string]string{
		"run":    `process.run({"/bin/sh", "-c", "sleep 10"})`,
		"system": `process.system("sleep 10", {system = "/bin/sh"})`,
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			state, scheduler := newTestRuntimeWithContext(t, ctx, nil)
			done := startScript(t, state, scheduler, `
				local process = require("@std/process")
				`+call+`
			`)

			timer := time.AfterFunc(50*time.Millisecond, cancel)
			defer timer.Stop()
			select {
			case ok := <-done:
				if ok {
					t.Fatal("scheduler succeeded after process cancellation")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("process call did not stop after context cancellation")
			}
		})
	}
}

func TestTaskCancelStopsProcessRunInsideSpawnedTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, scheduler := newTestRuntimeWithContext(t, ctx, nil)
	done := startScript(t, state, scheduler, `
		local process = require("@std/process")
		local task = require("@std/task")
		local handle = task.spawn(function()
			process.run({"/bin/sh", "-c", "sleep 10"})
			childFinished = true
		end)
		task.delay(0.05, function()
			task.cancel(handle)
			cancelled = true
		end)
		task.wait(0.1)
		assert(cancelled == true)
		assert(childFinished == nil)
	`)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("scheduler reported failure")
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("task.cancel did not stop the subprocess task")
	}
}

func TestProcessOnSignalQueuesCallbackOnScheduler(t *testing.T) {
	state, scheduler := newTestRuntime(t, nil)
	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	}()
	runScript(t, state, scheduler, `
		local process = require("@std/process")
		local task = require("@std/task")
		local handle = process.onSignal("SIGUSR1", function()
			signalReceived = true
		end)
		task.wait(0.15)
		handle:close()
		assert(signalReceived == true)
	`)
}
