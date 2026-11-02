package net

import (
	"context"

	"github.com/Leoj030/go-luau/luau"
	"github.com/ceilo-org/ceilo/internal/stdlib/task"
)

func ClientModule(ctx context.Context) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &clientModule{ctx: ctx}
	return map[string]any{"request": client.request}
}

// ServerModule returns the members of the net module that serve requests:
// the server function. The callable net/server module is built from it by
// the engine.
func ServerModule(ctx context.Context, sched *task.Scheduler, state *luau.State, logErr func(error)) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}
	m := &module{sched: sched, L: state, ctx: ctx, logErr: logErr}
	return map[string]any{"server": m.serve}
}
