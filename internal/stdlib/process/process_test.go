//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
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
	t.Helper()
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	scheduler := stdtask.New(stdtask.Options{OnError: func(err error) { t.Errorf("scheduled callback: %v", err) }})
	scheduler.BindState(state)
	if err := state.RegisterModule("@ceilo/task", scheduler.Module()); err != nil {
		t.Fatal(err)
	}
	if err := Register(state, "@ceilo/process", args, scheduler.QueueCallback); err != nil {
		t.Fatal(err)
	}
	return state, scheduler
}

func runScript(t *testing.T, state *luau.State, scheduler *stdtask.Scheduler, source string) {
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
	if !scheduler.Run() {
		t.Fatal("scheduler reported failure")
	}
}

func TestProcessModuleRunAndOptions(t *testing.T) {
	state, scheduler := newTestRuntime(t, []string{"first", "second"})
	runScript(t, state, scheduler, `
		local process = require("@ceilo/process")
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

		local discarded = process.run({"/bin/sh", "-c", "printf hidden; printf hidden >&2"}, {stdio = "none"})
		assert(discarded.ok and discarded.stdout == "" and discarded.stderr == "")

		local inherited = process.system("printf shell", {system = "/bin/sh"})
		assert(inherited.ok and inherited.stdout == "shell")
	`)
}

func TestProcessTypedefCompiles(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "typedefs", "process.luau"))
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
		local result = require("@ceilo/process").run(
			{"/bin/sh", "-c", "sleep 10 & wait"},
			{timeout = 0.05}
		)
		assert(result.timedOut == true)
		assert(result.ok == false)
		assert(result.signal == "SIGKILL", tostring(result.signal) .. ":" .. tostring(result.exitcode) .. ":" .. tostring(result.timedOut))
	`)
}

func TestProcessOnSignalQueuesCallbackOnScheduler(t *testing.T) {
	state, scheduler := newTestRuntime(t, nil)
	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	}()
	runScript(t, state, scheduler, `
		local process = require("@ceilo/process")
		local task = require("@ceilo/task")
		local handle = process.onSignal("SIGUSR1", function()
			signalReceived = true
		end)
		task.wait(0.15)
		handle:close()
		assert(signalReceived == true)
	`)
}
