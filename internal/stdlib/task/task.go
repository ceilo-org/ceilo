// Task scheduler runs Luau threads cooperatively on one goroutine. It
// provides the task library: task.wait, task.spawn, task.defer, and
// task.delay, including waits inside modules that are being loaded with require.
package task

import (
	"container/heap"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/Leoj030/go-luau/luau"
)

// Options configures a Scheduler.
type Options struct {
	// OnError is called for every task that fails.
	OnError func(error)
	// BeforeIdle is called right before the scheduler sleeps, so output
	// written so far shows up while it waits.
	BeforeIdle func()
}

type Scheduler struct {
	opts           Options
	q              queue
	seq            uint64
	failed         bool
	waitingModules int
	parkedWaiters  []*task
	current        *task
	state          *luau.State
	callbacks      chan luau.Ref
	wake           chan struct{}
	runDone        chan struct{}
	runOnce        sync.Once

	// task.wait yields (nonce, seconds). A yield that doesn't start with this
	// per-run secret did not come from task.wait.
	nonce string
}

type task struct {
	th            *luau.Thread
	control       *taskControl
	isMain        bool
	started       bool
	nargs         int       // arguments already on the stack for the first resume
	wake          time.Time // when to resume
	yieldedAt     time.Time
	yieldedValues int
	waitScheduled bool
	seq           uint64 // FIFO tie-break for equal wake times
	moduleResume  bool
	moduleErr     error // non-nil when resuming a failed module require
	moduleFailed  bool
}

type taskControl struct {
	cancelled bool
}

func New(opts Options) *Scheduler {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS random source is broken; nothing sensible to do
	}
	return &Scheduler{
		opts:      opts,
		nonce:     hex.EncodeToString(b),
		callbacks: make(chan luau.Ref, 64),
		wake:      make(chan struct{}, 1),
		runDone:   make(chan struct{}),
	}
}

// BindState attaches the owning state used to schedule callbacks from external
// event sources, such as operating system signals.
func (s *Scheduler) BindState(state *luau.State) {
	s.state = state
}

// QueueCallback schedules a retained Luau function reference on the scheduler's
// goroutine. It is safe to call from a goroutine that does not own the state.
func (s *Scheduler) QueueCallback(callback luau.Ref) {
	select {
	case s.callbacks <- callback:
		select {
		case s.wake <- struct{}{}:
		default:
		}
	case <-s.runDone:
	}
}

// Start queues th, with its function and nargs arguments already on its
// stack, as the main task. If it fails, everything else is abandoned.
func (s *Scheduler) Start(th *luau.Thread, nargs int) {
	s.schedule(&task{th: th, control: &taskControl{}, isMain: true, nargs: nargs}, time.Now())
}

// Module returns the functions of the task library, ready for
// luau.State.RegisterModule.
func (s *Scheduler) Module() map[string]luau.Func {
	return map[string]luau.Func{
		"wait":   s.wait,
		"spawn":  s.spawn,
		"defer":  s.deferTask,
		"delay":  s.delay,
		"cancel": s.cancel,
	}
}

// ModuleFinished queues a require caller to resume after its module finishes.
// The waiter is a separate pinned handle and must be closed after it finishes.
func (s *Scheduler) ModuleFinished(waiter *luau.Thread, err error) {
	if s.waitingModules > 0 {
		s.waitingModules--
	}
	if err != nil && s.current != nil {
		s.current.moduleFailed = true
	}
	var control *taskControl
	if s.current != nil {
		control = s.current.control
	}
	s.schedule(&task{th: waiter, control: control, started: true, moduleResume: true, moduleErr: err}, time.Now())
}

// task.wait([seconds]) -> seconds actually waited
func (s *Scheduler) wait(c *luau.State) (int, error) {
	sec := 0.0
	if t := c.TypeName(1); t != "nil" && t != "no value" {
		v, err := c.Get(1)
		f, ok := v.(float64)
		if err != nil || !ok {
			return 0, errors.New("task.wait: expected a number")
		}
		sec = f
	}
	if !(sec > 0) { // negative or NaN
		sec = 0
	}
	if sec > 1e9 {
		sec = 1e9
	}
	if err := c.Push(s.nonce); err != nil {
		return 0, err
	}
	if err := c.Push(sec); err != nil {
		return 0, err
	}
	// During require, the active scheduler task is the caller waiting on the
	// module. Pin the coroutine that actually yielded so the module can finish.
	th, err := c.Thread()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	var control *taskControl
	if s.current != nil {
		control = s.current.control
	}
	s.schedule(&task{
		th:            th,
		control:       control,
		started:       true,
		yieldedAt:     now,
		yieldedValues: 2,
	}, now.Add(time.Duration(sec*float64(time.Second))))
	if s.current != nil {
		s.current.waitScheduled = true
	}
	return c.Yield(2)
}

// task.spawn(fn, ...): run fn on its own thread, starting on the next
// scheduler turn (not immediately).
func (s *Scheduler) spawn(c *luau.State) (int, error) {
	if c.TypeName(1) != "function" {
		return 0, errors.New("task.spawn: expected a function")
	}
	return s.scheduleTask(c, 1, time.Now())
}

// task.defer(fn, ...): run fn at the next scheduler opportunity.
func (s *Scheduler) deferTask(c *luau.State) (int, error) {
	if c.TypeName(1) != "function" {
		return 0, errors.New("task.defer: expected a function")
	}
	return s.scheduleTask(c, 1, time.Now())
}

// task.cancel(handle): prevent a thread from running again.
func (s *Scheduler) cancel(c *luau.State) (int, error) {
	v, ok := c.ToUserdata(1)
	control, isTask := v.(*taskControl)
	if !ok || !isTask {
		return 0, errors.New("task.cancel: expected a thread")
	}
	control.cancelled = true
	return 0, nil
}

func (s *Scheduler) delay(c *luau.State) (int, error) {
	v, err := c.Get(1)
	seconds, isNumber := v.(float64)
	if err != nil || c.TypeName(1) != "number" || !isNumber {
		return 0, errors.New("task.delay: expected a number")
	}
	if c.TypeName(2) != "function" {
		return 0, errors.New("task.delay: expected a function")
	}
	if !(seconds > 0) {
		seconds = 0
	}
	if seconds > 1e9 {
		seconds = 1e9
	}
	return s.scheduleTask(c, 2, time.Now().Add(time.Duration(seconds*float64(time.Second))))
}

// scheduleTask queues a task and returns its cancellation handle.
func (s *Scheduler) scheduleTask(c *luau.State, functionIndex int, wake time.Time) (int, error) {
	t, err := newTask(c, functionIndex)
	if err != nil {
		return 0, err
	}
	if err := c.PushUserdata(t.control); err != nil {
		t.th.Close()
		return 0, err
	}
	s.schedule(t, wake)
	return 1, nil
}

// newTask copies a function and its arguments from c onto a new coroutine.
func newTask(c *luau.State, functionIndex int) (*task, error) {
	th, err := c.NewThread()
	if err != nil {
		return nil, err
	}
	for i := functionIndex; i <= c.Top(); i++ {
		ref, err := c.Ref(i)
		if err != nil {
			th.Close()
			return nil, err
		}
		err = th.PushRef(ref)
		c.Unref(ref)
		if err != nil {
			th.Close()
			return nil, err
		}
	}
	return &task{
		th:      th,
		control: &taskControl{},
		nargs:   c.Top() - functionIndex,
	}, nil
}

// Run drives tasks until none are left. An error in the main task stops
// everything; an error in a spawned task is reported and the rest carry on.
// It reports whether every task succeeded.
func (s *Scheduler) Run() bool {
	defer s.runOnce.Do(func() { close(s.runDone) })
	for {
		s.scheduleCallbacks()
		if len(s.q) == 0 {
			break
		}
		t := heap.Pop(&s.q).(*task)
		if t.control != nil && t.control.cancelled {
			t.th.Close()
			continue
		}
		if d := time.Until(t.wake); d > 0 {
			if s.opts.BeforeIdle != nil {
				s.opts.BeforeIdle()
			}
			timer := time.NewTimer(d)
			select {
			case <-timer.C:
			case <-s.wake:
				if !timer.Stop() {
					<-timer.C
				}
				heap.Push(&s.q, t)
				continue
			}
		}
		finished, err := s.step(t)
		if t.control != nil && t.control.cancelled {
			finished = true
		}
		if err != nil && t.moduleFailed {
			err = nil // module errors are delivered to require's caller
		}
		if err != nil {
			s.failed = true
			if s.opts.OnError != nil {
				s.opts.OnError(err)
			}
		}
		if finished {
			t.th.Close()
		}
		if err != nil && t.isMain {
			for _, rest := range s.q {
				rest.th.Close()
			}
			s.closeParkedWaiters()
			return false
		}
	}
	if s.waitingModules > 0 {
		err := errors.New("task yielded without task.wait or a yielding module")
		s.failed = true
		if s.opts.OnError != nil {
			s.opts.OnError(err)
		}
		for _, t := range s.parkedWaiters {
			if t.isMain {
				s.closeParkedWaiters()
				return false
			}
		}
	}
	s.closeParkedWaiters()
	return !s.failed
}

func (s *Scheduler) scheduleCallbacks() {
	for {
		select {
		case callback := <-s.callbacks:
			if s.state == nil {
				s.failed = true
				if s.opts.OnError != nil {
					s.opts.OnError(errors.New("task scheduler has no state for an external callback"))
				}
				continue
			}
			th, err := s.state.NewThread()
			if err == nil {
				err = th.PushRef(callback)
			}
			if err != nil {
				if th != nil {
					th.Close()
				}
				s.failed = true
				if s.opts.OnError != nil {
					s.opts.OnError(err)
				}
				continue
			}
			s.schedule(&task{th: th, control: &taskControl{}}, time.Now())
		default:
			return
		}
	}
}

// step resumes t once. It reports whether the task is finished.
func (s *Scheduler) step(t *task) (finished bool, err error) {
	nargs := t.nargs
	var n int
	var done bool
	if t.moduleResume {
		t.moduleResume = false
		s.current = t
		if t.moduleErr != nil {
			n, done, err = t.th.ResumeError(t.moduleErr)
		} else {
			// A successful module result was placed on the waiter's stack by
			// go-luau, so ResumeN consumes it directly.
			n, done, err = t.th.ResumeN(1)
		}
		s.current = nil
	} else {
		if t.started {
			if t.yieldedValues > 0 {
				t.th.Pop(t.yieldedValues)
				t.yieldedValues = 0
			}
			// Resuming after task.wait: its result is the time waited.
			if err := t.th.Push(time.Since(t.yieldedAt).Seconds()); err != nil {
				return true, err
			}
			nargs = 1
		}
		s.current = t
		n, done, err = t.th.ResumeN(nargs)
		s.current = nil
	}
	t.started = true

	if err != nil {
		return true, err
	}
	if done {
		t.th.Pop(n) // drop returned values
		return true, nil
	}

	wait, ok := s.parseWait(t.th, n)
	t.th.Pop(n)
	if !ok {
		t.waitScheduled = false
		if n == 0 {
			s.waitingModules++
			s.parkedWaiters = append(s.parkedWaiters, t)
			return false, nil
		}
		return true, errors.New("task yielded without task.wait or a yielding module")
	}
	if t.waitScheduled {
		t.waitScheduled = false
		return true, nil
	}
	t.yieldedAt = time.Now()
	s.schedule(t, t.yieldedAt.Add(wait))
	return false, nil
}

func (s *Scheduler) closeParkedWaiters() {
	for _, t := range s.parkedWaiters {
		t.th.Close()
	}
	s.parkedWaiters = nil
}

// parseWait recognises the (nonce, seconds) pair that task.wait yields.
func (s *Scheduler) parseWait(th *luau.Thread, n int) (time.Duration, bool) {
	if n != 2 {
		return 0, false
	}
	first, err := th.Get(1)
	if err != nil || first != s.nonce {
		return 0, false
	}
	second, err := th.Get(2)
	sec, ok := second.(float64)
	if err != nil || !ok {
		return 0, false
	}
	return time.Duration(sec * float64(time.Second)), true
}

func (s *Scheduler) schedule(t *task, wake time.Time) {
	s.seq++
	t.seq, t.wake = s.seq, wake
	heap.Push(&s.q, t)
}

// ---- min-heap ordered by wake time, then insertion order ----

type queue []*task

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].wake.Equal(q[j].wake) {
		return q[i].seq < q[j].seq
	}
	return q[i].wake.Before(q[j].wake)
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(*task)) }
func (q *queue) Pop() any {
	old := *q
	t := old[len(old)-1]
	*q = old[:len(old)-1]
	return t
}
