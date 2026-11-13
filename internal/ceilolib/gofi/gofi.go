package gofi

import (
	"context"

	"github.com/ceilo-org/ceilo/internal/stdlib/task"
	"github.com/Leoj030/go-luau/luau"
	"github.com/ceilo-org/gofi/host"
)

func Module(s *task.Scheduler) map[string]any {
	return map[string]any{
		"open": func(path string) (map[string]any, error) {
			return host.OpenWith(path, host.Options{
				Wrap: func(f host.Func) any { return asyncFunc(s, f) },
			})
		},
	}
}

// pushValue pushes a value that may contain raw Go functions inside maps and
// slices; luau.State.Push only handles data.
func pushValue(s *luau.State, v any) error {
	switch t := v.(type) {
	case func(*luau.State) (int, error):
		return s.PushFunction("gofi", luau.Func(t))
	case map[string]any:
		if err := s.NewTable(0, len(t)); err != nil {
			return err
		}
		for k, e := range t {
			if err := pushValue(s, e); err != nil {
				return err
			}
			if err := s.SetField(-2, k); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if err := s.NewTable(len(t), 0); err != nil {
			return err
		}
		for i, e := range t {
			if err := pushValue(s, e); err != nil {
				return err
			}
			if err := s.SetIndex(-2, i+1); err != nil {
				return err
			}
		}
		return nil
	}
	return s.Push(v)
}

func asyncFunc(s *task.Scheduler, f host.Func) func(*luau.State) (int, error) {
	return func(c *luau.State) (int, error) {
		args := make([]any, 0, c.Top())
		for i := 1; i <= c.Top(); i++ {
			v, err := c.Get(i)
			if err != nil {
				return 0, err
			}
			args = append(args, v)
		}
		return s.Await(c, func(_ context.Context, resume func(func(*luau.Thread) (int, error), error)) {
			f.Call(args, func(results []any, err error) {
				if err != nil {
					resume(nil, err)
					return
				}
				resume(func(th *luau.Thread) (int, error) {
					for _, r := range results {
						if err := pushValue(th.State, r); err != nil {
							return 0, err
						}
					}
					return len(results), nil
				}, nil)
			})
		})
	}
}
