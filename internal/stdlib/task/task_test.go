package task

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Leoj030/go-luau/luau"
)

type yieldingRequirer struct {
	luau.Requirer
	finished func(*luau.Thread, error)
}

func (r *yieldingRequirer) ModuleFinished(waiter *luau.Thread, err error) {
	r.finished(waiter, err)
}

func runWithYieldingModule(t *testing.T, main, module string, onError func(error)) (*luau.State, bool) {
	t.Helper()

	L, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(L.Close)

	scheduler := New(Options{OnError: onError})
	fsys := fstest.MapFS{
		"main.luau":   &fstest.MapFile{Data: []byte(main)},
		"module.luau": &fstest.MapFile{Data: []byte(module)},
	}
	requirer := &yieldingRequirer{
		Requirer: luau.FSRequirer(fsys),
		finished: scheduler.ModuleFinished,
	}
	if err := L.OpenRequire(requirer); err != nil {
		t.Fatal(err)
	}
	if err := L.RegisterModule("@std/task", scheduler.Module()); err != nil {
		t.Fatal(err)
	}

	bc, err := luau.Compile(main)
	if err != nil {
		t.Fatal(err)
	}
	th, err := L.NewThread()
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Load(bc, "@main.luau"); err != nil {
		th.Close()
		t.Fatal(err)
	}
	scheduler.Start(th, 0)
	return L, scheduler.Run()
}

func TestRequireModuleCanYieldOnTaskWait(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`result = require("./module").value`,
		`local task = require("@std/task"); task.wait(0.001); task.wait(0.001); return {value = 42}`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("result"); err != nil || got != 42.0 {
		t.Errorf("result = %v, %v; want 42", got, err)
	}
}

func TestTaskWaitResumesRegularAndSpawnedTasks(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task"); task.spawn(function() task.wait(0.001); spawned = true end); task.wait(0.002); resumed = true`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	for name := range map[string]bool{"resumed": true, "spawned": true} {
		if got, err := L.GetGlobal(name); err != nil || got != true {
			t.Errorf("%s = %v, %v; want true", name, got, err)
		}
	}
}

func TestTaskDeferRunsAtNextSchedulerOpportunityWithArguments(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task")
		local stage = "before yield"
		local handle = task.defer(function(a, b)
			seenStage = stage
			deferred = a + b
		end, 5, 7)
		handleReturned = handle ~= nil
		stage = "yielding"
		task.wait(0.001)
		stage = "resumed"`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("handleReturned"); err != nil || got != true {
		t.Errorf("handleReturned = %v, %v; want true", got, err)
	}
	if got, err := L.GetGlobal("seenStage"); err != nil || got != "yielding" {
		t.Errorf("seenStage = %v, %v; want yielding", got, err)
	}
	if got, err := L.GetGlobal("deferred"); err != nil || got != 12.0 {
		t.Errorf("deferred = %v, %v; want 12", got, err)
	}
}

func TestTaskCancelStopsDeferredTask(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task")
		local handle = task.defer(function()
			deferred = true
		end)
		task.cancel(handle)
		task.wait(0.001)`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("deferred"); err != nil || got != nil {
		t.Errorf("deferred = %v, %v; want nil", got, err)
	}
}

func TestTaskCancelStopsSpawnedTaskAfterWait(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task")
		local handle = task.spawn(function()
			task.wait(0.001)
			first = true
			task.wait(0.01)
			second = true
		end)
		task.wait(0.004)
		task.cancel(handle)`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("first"); err != nil || got != true {
		t.Errorf("first = %v, %v; want true", got, err)
	}
	if got, err := L.GetGlobal("second"); err != nil || got != nil {
		t.Errorf("second = %v, %v; want nil", got, err)
	}
}

func TestTaskDelayRunsFunctionWithArguments(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task")
		local handle = task.delay(0.001, function(a, b)
			delayed = a + b
		end, 20, 22)
		handleReturned = handle ~= nil
		task.wait(0.004)`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("handleReturned"); err != nil || got != true {
		t.Errorf("handleReturned = %v, %v; want true", got, err)
	}
	if got, err := L.GetGlobal("delayed"); err != nil || got != 42.0 {
		t.Errorf("delayed = %v, %v; want 42", got, err)
	}
}

func TestTaskCancelStopsDelayedTask(t *testing.T) {
	L, ok := runWithYieldingModule(
		t,
		`local task = require("@std/task")
		local handle = task.delay(0.02, function()
			delayed = true
		end)
		task.wait(0.001)
		task.cancel(handle)
		task.wait(0.025)`,
		`return true`,
		func(err error) { t.Errorf("unexpected task error: %v", err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure")
	}
	if got, err := L.GetGlobal("delayed"); err != nil || got != nil {
		t.Errorf("delayed = %v, %v; want nil", got, err)
	}
}

func TestYieldingModuleErrorCanBeCaughtByPcall(t *testing.T) {
	var errors []error
	L, ok := runWithYieldingModule(
		t,
		`local ok, err = pcall(function() return require("./module") end); result = ok; message = tostring(err)`,
		`require("@std/task").wait(0.001); error("module failed")`,
		func(err error) { errors = append(errors, err) },
	)
	if !ok {
		t.Fatal("scheduler reported failure for a caught module error")
	}
	if len(errors) != 0 {
		t.Errorf("reported errors = %v; want none", errors)
	}
	if got, err := L.GetGlobal("result"); err != nil || got != false {
		t.Errorf("result = %v, %v; want false", got, err)
	}
	if got, err := L.GetGlobal("message"); err != nil {
		t.Errorf("reading message: %v", err)
	} else if message, ok := got.(string); !ok || !strings.Contains(message, "module failed") {
		t.Errorf("message = %v; want the module error", got)
	}
}

func TestYieldingModuleErrorReachesScheduler(t *testing.T) {
	var reported error
	_, ok := runWithYieldingModule(
		t,
		`return require("./module")`,
		`require("@std/task").wait(0.001); error("module failed")`,
		func(err error) { reported = err },
	)
	if ok {
		t.Fatal("scheduler succeeded after an uncaught module error")
	}
	if reported == nil || !strings.Contains(reported.Error(), "module failed") {
		t.Errorf("reported error = %v; want the module error", reported)
	}
}

func TestPostKeepsSchedulerAliveUntilReleased(t *testing.T) {
	scheduler := New(Options{})
	if err := scheduler.Hold(); err != nil {
		t.Fatal(err)
	}

	runDone := make(chan bool, 1)
	go func() { runDone <- scheduler.Run() }()

	posted := make(chan struct{})
	if err := scheduler.Post(func() {
		close(posted)
		if err := scheduler.Release(); err != nil {
			t.Errorf("release scheduler: %v", err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-posted:
	case <-time.After(time.Second):
		t.Fatal("posted callback was not run")
	}
	select {
	case ok := <-runDone:
		if !ok {
			t.Fatal("scheduler reported failure")
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after release")
	}
}

func TestRunReturnsWhenNoTasksOrHoldsRemain(t *testing.T) {
	scheduler := New(Options{})
	if !scheduler.Run() {
		t.Fatal("scheduler reported failure with no tasks")
	}
}

func TestSubmitRunsYieldingTaskAndCompletesOnScheduler(t *testing.T) {
	L, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(L.Close)

	scheduler := New(Options{OnError: func(err error) { t.Errorf("unexpected task error: %v", err) }})
	scheduler.BindState(L)
	if err := L.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := L.RegisterModule("@std/task", scheduler.Module()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Hold(); err != nil {
		t.Fatal(err)
	}

	bc, err := luau.Compile(`require("@std/task").wait(0.001); submitted = true`)
	if err != nil {
		t.Fatal(err)
	}

	runDone := make(chan bool, 1)
	go func() { runDone <- scheduler.Run() }()

	type completion struct {
		value any
		err   error
	}
	completed := make(chan completion, 1)
	err = scheduler.Submit(L,
		func(th *luau.Thread) (int, error) {
			if err := th.Load(bc, "@submitted.luau"); err != nil {
				return 0, err
			}
			return 0, nil
		},
		func(_ *luau.Thread, taskErr error) {
			value, getErr := L.GetGlobal("submitted")
			if taskErr == nil {
				taskErr = getErr
			}
			completed <- completion{value: value, err: taskErr}
			if err := scheduler.Release(); err != nil {
				t.Errorf("release scheduler: %v", err)
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-completed:
		if result.err != nil {
			t.Fatalf("submitted task failed: %v", result.err)
		}
		if result.value != true {
			t.Errorf("submitted = %v; want true", result.value)
		}
	case <-time.After(time.Second):
		t.Fatal("submitted task did not complete")
	}
	select {
	case ok := <-runDone:
		if !ok {
			t.Fatal("scheduler reported failure")
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after submitted task completed")
	}
}
