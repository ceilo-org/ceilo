// task.go
package task

import (
	"container/heap"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Leoj030/go-luau/luau"
)

type Options struct {
	OnError    func(error)
	BeforeIdle func()
}

type Scheduler struct {
	opts           Options
	q              queue
	ready          []*task
	rhead          int
	seq            uint64
	failed         bool
	mu             sync.Mutex
	inbox          []command
	spare          []command
	hasInbox       atomic.Bool
	holds          int
	stopped        bool
	waitingModules int
	parkedWaiters  []*task
	current        *task
	state          *luau.State
	wake           chan struct{}
	nonce          string
	idle           []*luau.Thread
}

type task struct {
	th            *luau.Thread
	control       *taskControl
	isMain        bool
	started       bool
	nargs         int
	wake          time.Time
	yieldedAt     time.Time
	yieldedValues int
	waitScheduled bool
	seq           uint64
	moduleResume  bool
	moduleErr     error
	moduleFailed  bool
	start         func(*luau.Thread) (int, error)
	done          func(*luau.Thread, error)
	pooled        bool
	completed     bool
}

type taskControl struct {
	cancelled bool
}

type command struct {
	callback    luau.Ref
	hasCallback bool
	submit      *submission
	post        func()
}

type submission struct {
	state *luau.State
	start func(*luau.Thread) (int, error)
	done  func(*luau.Thread, error)
}

const maxIdleThreads = 256

func New(opts Options) *Scheduler {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return &Scheduler{
		opts:  opts,
		nonce: hex.EncodeToString(b),
		wake:  make(chan struct{}, 1),
	}
}

func (s *Scheduler) BindState(state *luau.State) {
	s.state = state
}

func (s *Scheduler) QueueCallback(callback luau.Ref) {
	_ = s.enqueue(command{callback: callback, hasCallback: true})
}

func (s *Scheduler) Post(fn func()) error {
	if fn == nil {
		return errors.New("task scheduler: cannot post a nil function")
	}
	return s.enqueue(command{post: fn})
}

func (s *Scheduler) Submit(state *luau.State, start func(*luau.Thread) (int, error), done func(*luau.Thread, error)) error {
	if state == nil {
		return errors.New("task scheduler: cannot submit a task without a state")
	}
	if start == nil {
		return errors.New("task scheduler: cannot submit a task without a start function")
	}
	if done == nil {
		return errors.New("task scheduler: cannot submit a task without a completion function")
	}
	return s.enqueue(command{submit: &submission{state: state, start: start, done: done}})
}

func (s *Scheduler) Hold() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return errors.New("task scheduler: cannot hold a stopped scheduler")
	}
	s.holds++
	return nil
}

func (s *Scheduler) Release() error {
	s.mu.Lock()
	if s.holds == 0 {
		s.mu.Unlock()
		return errors.New("task scheduler: release without a hold")
	}
	s.holds--
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *Scheduler) enqueue(c command) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errors.New("task scheduler: scheduler has stopped")
	}
	s.inbox = append(s.inbox, c)
	s.hasInbox.Store(true)
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Start(th *luau.Thread, nargs int) {
	s.schedule(&task{th: th, control: &taskControl{}, isMain: true, nargs: nargs}, time.Time{})
}

func (s *Scheduler) Module() map[string]luau.Func {
	return map[string]luau.Func{
		"wait":   s.wait,
		"spawn":  s.spawn,
		"defer":  s.deferTask,
		"delay":  s.delay,
		"cancel": s.cancel,
	}
}

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
	t := &task{th: waiter, control: control, started: true, moduleResume: true, moduleErr: err}
	if s.current != nil {
		t.isMain = s.current.isMain
		t.done = s.current.done
		s.current.done = nil
	}
	s.schedule(t, time.Time{})
}

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
	if !(sec > 0) {
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
	th, err := c.Thread()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	var control *taskControl
	var done func(*luau.Thread, error)
	isMain := false
	if s.current != nil {
		control = s.current.control
		done = s.current.done
		s.current.done = nil
		isMain = s.current.isMain
	}
	var at time.Time
	if sec > 0 {
		at = now.Add(time.Duration(sec * float64(time.Second)))
	}
	s.schedule(&task{
		th:            th,
		control:       control,
		isMain:        isMain,
		done:          done,
		started:       true,
		yieldedAt:     now,
		yieldedValues: 2,
	}, at)
	if s.current != nil {
		s.current.waitScheduled = true
	}
	return c.Yield(2)
}

func (s *Scheduler) spawn(c *luau.State) (int, error) {
	if c.TypeName(1) != "function" {
		return 0, errors.New("task.spawn: expected a function")
	}
	return s.scheduleTask(c, 1, time.Time{})
}

func (s *Scheduler) deferTask(c *luau.State) (int, error) {
	if c.TypeName(1) != "function" {
		return 0, errors.New("task.defer: expected a function")
	}
	return s.scheduleTask(c, 1, time.Time{})
}

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

func (s *Scheduler) Run() bool {
	defer s.finish()

	stats := os.Getenv("CEILO_SCHED_STATS") != ""
	var idle time.Duration
	if stats {
		began := time.Now()
		defer func() {
			total := time.Since(began)
			fmt.Fprintf(os.Stderr, "scheduler: busy %.0f%% of %v\n",
				100*(1-idle.Seconds()/total.Seconds()), total.Round(time.Millisecond))
		}()
	}

	for {
		s.processCommands()

		if len(s.q) > 0 {
			now := time.Now()
			for len(s.q) > 0 && !s.q[0].wake.After(now) {
				s.ready = append(s.ready, heap.Pop(&s.q).(*task))
			}
		}

		var t *task
		switch {
		case s.rhead < len(s.ready):
			t = s.popReady()

		case len(s.q) > 0:
			t = heap.Pop(&s.q).(*task)
			if t.control != nil && t.control.cancelled {
				s.finishTask(t, errors.New("task cancelled"))
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

		default:
			s.mu.Lock()
			if s.hasInbox.Load() {
				s.mu.Unlock()
				continue
			}
			if s.holds == 0 {
				s.mu.Unlock()
				return !s.failed
			}
			s.mu.Unlock()
			if s.opts.BeforeIdle != nil {
				s.opts.BeforeIdle()
			}
			var slept time.Time
			if stats {
				slept = time.Now()
			}
			<-s.wake
			if stats {
				idle += time.Since(slept)
			}
			continue
		}

		if t.control != nil && t.control.cancelled {
			s.finishTask(t, errors.New("task cancelled"))
			continue
		}
		finished, err := s.step(t)
		if t.control != nil && t.control.cancelled {
			finished = true
		}
		if err != nil && t.moduleFailed {
			err = nil
		}
		if err != nil {
			s.failed = true
			if s.opts.OnError != nil {
				s.opts.OnError(err)
			}
		}
		if finished {
			s.finishTask(t, err)
		}
		if err != nil && t.isMain {
			for _, rest := range s.q {
				s.finishTask(rest, err)
			}
			for _, rest := range s.ready[s.rhead:] {
				s.finishTask(rest, err)
			}
			s.closeParkedWaiters()
			return false
		}
	}
}

func (s *Scheduler) processCommands() {
	for s.hasInbox.Load() {
		s.mu.Lock()
		pending := s.inbox
		s.inbox = s.spare[:0]
		s.hasInbox.Store(false)
		s.mu.Unlock()

		for _, c := range pending {
			switch {
			case c.post != nil:
				c.post()
			case c.submit != nil:
				s.scheduleSubmission(c.submit)
			case c.hasCallback:
				s.scheduleCallback(c.callback)
			}
		}
		for i := range pending {
			pending[i] = command{}
		}
		s.spare = pending[:0]
	}
}

func (s *Scheduler) scheduleCallback(callback luau.Ref) {
	if s.state == nil {
		s.report(errors.New("task scheduler has no state for an external callback"))
		return
	}
	th, err := s.state.NewThread()
	if err == nil {
		err = th.PushRef(callback)
	}
	if err != nil {
		if th != nil {
			th.Close()
		}
		s.report(err)
		return
	}
	s.schedule(&task{th: th}, time.Time{})
}

func (s *Scheduler) scheduleSubmission(sub *submission) {
	th, err := s.thread(sub.state)
	if err != nil {
		sub.done(nil, err)
		s.report(err)
		return
	}
	s.schedule(&task{
		th:     th,
		start:  sub.start,
		done:   sub.done,
		pooled: sub.state == s.state && s.state != nil,
	}, time.Time{})
}

func (s *Scheduler) thread(state *luau.State) (*luau.Thread, error) {
	if state == s.state && s.state != nil {
		if n := len(s.idle); n > 0 {
			th := s.idle[n-1]
			s.idle[n-1] = nil
			s.idle = s.idle[:n-1]
			return th, nil
		}
	}
	return state.NewThread()
}

func (s *Scheduler) finishTask(t *task, err error) {
	if t.done != nil {
		t.done(t.th, err)
	}
	if t.pooled && t.completed && err == nil && len(s.idle) < maxIdleThreads && t.th.Reset() == nil {
		s.idle = append(s.idle, t.th)
		return
	}
	t.th.Close()
}

func (s *Scheduler) report(err error) {
	s.failed = true
	if s.opts.OnError != nil {
		s.opts.OnError(err)
	}
}

func (s *Scheduler) finish() {
	s.mu.Lock()
	s.stopped = true
	s.inbox = nil
	s.hasInbox.Store(false)
	s.mu.Unlock()

	for _, th := range s.idle {
		th.Close()
	}
	s.idle = nil
}

func (s *Scheduler) step(t *task) (finished bool, err error) {
	nargs := t.nargs
	var n int
	var done bool
	if t.start != nil {
		nargs, err = t.start(t.th)
		t.start = nil
		if err != nil {
			return true, err
		}
		s.current = t
		n, done, err = t.th.ResumeN(nargs)
		s.current = nil
	} else if t.moduleResume {
		t.moduleResume = false
		s.current = t
		if t.moduleErr != nil {
			n, done, err = t.th.ResumeError(t.moduleErr)
		} else {
			n, done, err = t.th.ResumeN(1)
		}
		s.current = nil
	} else {
		if t.started {
			if t.yieldedValues > 0 {
				t.th.Pop(t.yieldedValues)
				t.yieldedValues = 0
			}
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
		t.completed = true
		if t.done == nil {
			t.th.Pop(n)
		}
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
		t.pooled = false
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
	t.seq = s.seq
	if wake.IsZero() {
		s.ready = append(s.ready, t)
		return
	}
	t.wake = wake
	heap.Push(&s.q, t)
}

func (s *Scheduler) popReady() *task {
	t := s.ready[s.rhead]
	s.ready[s.rhead] = nil
	s.rhead++
	if s.rhead == len(s.ready) {
		s.ready, s.rhead = s.ready[:0], 0
	} else if s.rhead >= 1024 && s.rhead*2 >= len(s.ready) {
		n := copy(s.ready, s.ready[s.rhead:])
		for i := n; i < len(s.ready); i++ {
			s.ready[i] = nil
		}
		s.ready, s.rhead = s.ready[:n], 0
	}
	return t
}

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
