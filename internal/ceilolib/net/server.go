package net

import (
	"context"

	"github.com/Leoj030/go-luau/luau"
	"github.com/ceilo-org/ceilo/internal/stdlib/task"
)

func ServerModule(ctx context.Context, sched *task.Scheduler, state *luau.State, logErr func(error)) map[string]luau.Func {
	if ctx == nil {
		ctx = context.Background()
	}
	m := &module{sched: sched, L: state, ctx: ctx, logErr: logErr}
	return map[string]luau.Func{"server": m.serve}
}
