// Package time implements the "@std/time" Luau library.
package time

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Leoj030/go-luau/luau"
)

type duration struct {
	nanos int64
}

type instant struct {
	time time.Time
}

const (
	nanosPerMicro  = 1e3
	nanosPerMilli  = 1e6
	nanosPerSecond = 1e9
	maxInt64Float  = 9223372036854775808.0
)

// RegisterModules registers the time module and its duration submodule.
func RegisterModules(state *luau.State, path string) error {
	lib := &library{}
	durationFuncs := lib.durationModule()

	if err := pushFunctionTable(state, durationFuncs); err != nil {
		return err
	}
	if err := state.RegisterModuleTable(path + "/duration"); err != nil {
		return err
	}

	if err := state.NewTable(0, 3); err != nil {
		return err
	}
	if err := state.PushFunction("now", lib.now); err != nil {
		return err
	}
	if err := state.SetField(-2, "now"); err != nil {
		return err
	}
	if err := state.PushFunction("since", since); err != nil {
		return err
	}
	if err := state.SetField(-2, "since"); err != nil {
		return err
	}
	if err := pushFunctionTable(state, durationFuncs); err != nil {
		return err
	}
	if err := state.Freeze(-1); err != nil {
		return err
	}
	if err := state.SetField(-2, "duration"); err != nil {
		return err
	}
	return state.RegisterModuleTable(path)
}

func pushFunctionTable(state *luau.State, funcs map[string]luau.Func) error {
	if err := state.NewTable(0, len(funcs)); err != nil {
		return err
	}
	table := state.Top()
	for name, fn := range funcs {
		if err := state.PushFunction(name, fn); err != nil {
			return err
		}
		if err := state.SetField(table, name); err != nil {
			return err
		}
	}
	return nil
}

func (lib *library) durationModule() map[string]luau.Func {
	constructors := map[string]float64{
		"nanoseconds":  1,
		"microseconds": nanosPerMicro,
		"milliseconds": nanosPerMilli,
		"seconds":      nanosPerSecond,
		"minutes":      60 * nanosPerSecond,
		"hours":        60 * 60 * nanosPerSecond,
		"days":         24 * 60 * 60 * nanosPerSecond,
		"weeks":        7 * 24 * 60 * 60 * nanosPerSecond,
	}
	module := make(map[string]luau.Func, len(constructors)+1)
	for name, scale := range constructors {
		module[name] = lib.durationConstructor(name, scale)
	}
	module["create"] = lib.createDuration
	return module
}

type library struct {
	durationMeta luau.Ref
	instantMeta  luau.Ref
}

func (lib *library) ensureDurationMeta(state *luau.State) error {
	if lib.durationMeta != 0 {
		return nil
	}
	meta, err := buildDurationMetatable(state)
	if err != nil {
		return err
	}
	lib.durationMeta = meta
	return nil
}

func (lib *library) ensureInstantMeta(state *luau.State) error {
	if lib.instantMeta != 0 {
		return nil
	}
	if err := lib.ensureDurationMeta(state); err != nil {
		return err
	}
	meta, err := buildInstantMetatable(state, lib.durationMeta)
	if err != nil {
		return err
	}
	lib.instantMeta = meta
	return nil
}

func (lib *library) now(state *luau.State) (int, error) {
	if err := lib.ensureInstantMeta(state); err != nil {
		return 0, err
	}
	return 1, pushInstant(state, instant{time: time.Now()}, lib.instantMeta)
}

func (lib *library) durationConstructor(name string, scale float64) luau.Func {
	return func(state *luau.State) (int, error) {
		value, err := numberArg(state, name, 1)
		if err != nil {
			return 0, err
		}
		nanos, err := durationNanos(value * scale)
		if err != nil {
			return 0, fmt.Errorf("time.duration.%s: %w", name, err)
		}
		if err := lib.ensureDurationMeta(state); err != nil {
			return 0, err
		}
		return 1, pushDuration(state, duration{nanos: nanos}, lib.durationMeta)
	}
}

func (lib *library) createDuration(state *luau.State) (int, error) {
	seconds, err := numberArg(state, "create", 1)
	if err != nil {
		return 0, err
	}
	subseconds, err := numberArg(state, "create", 2)
	if err != nil {
		return 0, err
	}
	nanos, err := durationNanos(seconds*nanosPerSecond + subseconds)
	if err != nil {
		return 0, fmt.Errorf("time.duration.create: %w", err)
	}
	if err := lib.ensureDurationMeta(state); err != nil {
		return 0, err
	}
	return 1, pushDuration(state, duration{nanos: nanos}, lib.durationMeta)
}

func buildDurationMetatable(state *luau.State) (luau.Ref, error) {
	var meta luau.Ref
	if err := state.NewTable(0, 8); err != nil {
		return 0, err
	}
	if err := state.NewTable(0, 11); err != nil {
		return 0, err
	}
	methods := map[string]luau.Func{
		"toNanoseconds":  durationValue("toNanoseconds", func(d duration) float64 { return float64(d.nanos) }),
		"toMicroseconds": durationValue("toMicroseconds", func(d duration) float64 { return float64(d.nanos) / nanosPerMicro }),
		"toMilliseconds": durationValue("toMilliseconds", func(d duration) float64 { return float64(d.nanos) / nanosPerMilli }),
		"toSeconds":      durationValue("toSeconds", func(d duration) float64 { return float64(d.nanos) / nanosPerSecond }),
		"toMinutes":      durationValue("toMinutes", func(d duration) float64 { return float64(d.nanos) / (60 * nanosPerSecond) }),
		"toHours":        durationValue("toHours", func(d duration) float64 { return float64(d.nanos) / (60 * 60 * nanosPerSecond) }),
		"toDays":         durationValue("toDays", func(d duration) float64 { return float64(d.nanos) / (24 * 60 * 60 * nanosPerSecond) }),
		"toWeeks":        durationValue("toWeeks", func(d duration) float64 { return float64(d.nanos) / (7 * 24 * 60 * 60 * nanosPerSecond) }),
		"subsecnanos":    durationValue("subsecnanos", func(d duration) float64 { return float64(d.nanos % int64(nanosPerSecond)) }),
		"subsecmicros":   durationValue("subsecmicros", func(d duration) float64 { return float64(d.nanos%int64(nanosPerSecond)) / nanosPerMicro }),
		"subsecmillis":   durationValue("subsecmillis", func(d duration) float64 { return float64(d.nanos%int64(nanosPerSecond)) / nanosPerMilli }),
	}
	for name, fn := range methods {
		if err := state.PushFunction(name, fn); err != nil {
			return 0, err
		}
		if err := state.SetField(-2, name); err != nil {
			return 0, err
		}
	}
	if err := state.SetField(-2, "__index"); err != nil {
		return 0, err
	}
	operators := map[string]luau.Func{
		"__add": durationBinary("__add", func(a, b duration) (duration, error) {
			if b.nanos > 0 && a.nanos > math.MaxInt64-b.nanos || b.nanos < 0 && a.nanos < math.MinInt64-b.nanos {
				return duration{}, errors.New("duration is outside the supported range")
			}
			return duration{nanos: a.nanos + b.nanos}, nil
		}, func() luau.Ref { return meta }),
		"__sub": durationBinary("__sub", func(a, b duration) (duration, error) {
			if b.nanos < 0 && a.nanos > math.MaxInt64+b.nanos || b.nanos > 0 && a.nanos < math.MinInt64+b.nanos {
				return duration{}, errors.New("duration is outside the supported range")
			}
			return duration{nanos: a.nanos - b.nanos}, nil
		}, func() luau.Ref { return meta }),
		"__mul": durationScale(false, func() luau.Ref { return meta }),
		"__div": durationScale(true, func() luau.Ref { return meta }),
		"__eq":  durationCompare(func(a, b duration) bool { return a.nanos == b.nanos }),
		"__lt":  durationCompare(func(a, b duration) bool { return a.nanos < b.nanos }),
		"__le":  durationCompare(func(a, b duration) bool { return a.nanos <= b.nanos }),
	}
	for name, fn := range operators {
		if err := state.PushFunction(name, fn); err != nil {
			return 0, err
		}
		if err := state.SetField(-2, name); err != nil {
			return 0, err
		}
	}
	if err := state.Push("The metatable is locked!"); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "__metatable"); err != nil {
		return 0, err
	}
	var err error
	meta, err = state.Ref(-1)
	if err != nil {
		return 0, err
	}
	if err := state.Pop(1); err != nil {
		return 0, err
	}
	return meta, nil
}

func buildInstantMetatable(state *luau.State, durationMeta luau.Ref) (luau.Ref, error) {
	if err := state.NewTable(0, 6); err != nil {
		return 0, err
	}
	if err := state.NewTable(0, 1); err != nil {
		return 0, err
	}
	if err := state.PushFunction("elapsed", instantElapsed); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "elapsed"); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "__index"); err != nil {
		return 0, err
	}
	operators := map[string]luau.Func{
		"__sub": instantSubtract(durationMeta),
		"__eq":  instantCompare(func(a, b instant) bool { return a.time.Equal(b.time) }),
		"__lt":  instantCompare(func(a, b instant) bool { return a.time.Before(b.time) }),
		"__le":  instantCompare(func(a, b instant) bool { return !a.time.After(b.time) }),
	}
	for name, fn := range operators {
		if err := state.PushFunction(name, fn); err != nil {
			return 0, err
		}
		if err := state.SetField(-2, name); err != nil {
			return 0, err
		}
	}
	if err := state.Push("The metatable is locked!"); err != nil {
		return 0, err
	}
	if err := state.SetField(-2, "__metatable"); err != nil {
		return 0, err
	}
	ref, err := state.Ref(-1)
	if err != nil {
		return 0, err
	}
	if err := state.Pop(1); err != nil {
		return 0, err
	}
	return ref, nil
}

func pushDuration(state *luau.State, d duration, meta luau.Ref) error {
	if err := state.PushUserdata(d); err != nil {
		return err
	}
	if err := state.PushRef(meta); err != nil {
		return err
	}
	return state.SetMetatable(-2)
}

func pushInstant(state *luau.State, i instant, meta luau.Ref) error {
	if err := state.PushUserdata(i); err != nil {
		return err
	}
	if err := state.PushRef(meta); err != nil {
		return err
	}
	return state.SetMetatable(-2)
}

func since(state *luau.State) (int, error) {
	i, err := instantArg(state, "since", 1)
	if err != nil {
		return 0, err
	}
	return 1, state.Push(time.Since(i.time).Seconds())
}

func instantElapsed(state *luau.State) (int, error) {
	i, err := instantArg(state, "elapsed", 1)
	if err != nil {
		return 0, err
	}
	return 1, state.Push(time.Since(i.time).Seconds())
}

func durationValue(name string, value func(duration) float64) luau.Func {
	return func(state *luau.State) (int, error) {
		d, err := durationArg(state, name, 1)
		if err != nil {
			return 0, err
		}
		return 1, state.Push(value(d))
	}
}

func durationBinary(name string, op func(duration, duration) (duration, error), meta func() luau.Ref) luau.Func {
	return func(state *luau.State) (int, error) {
		a, err := durationArg(state, name, 1)
		if err != nil {
			return 0, err
		}
		b, err := durationArg(state, name, 2)
		if err != nil {
			return 0, err
		}
		result, err := op(a, b)
		if err != nil {
			return 0, err
		}
		return 1, pushDuration(state, result, meta())
	}
}

func durationScale(divide bool, meta func() luau.Ref) luau.Func {
	return func(state *luau.State) (int, error) {
		d, err := durationArg(state, "scale", 1)
		if err != nil {
			return 0, err
		}
		factor, err := numberArg(state, "scale", 2)
		if err != nil {
			return 0, err
		}
		if divide && factor == 0 {
			return 0, errors.New("duration division by zero")
		}
		scaled := float64(d.nanos) * factor
		if divide {
			scaled = float64(d.nanos) / factor
		}
		nanos, err := durationNanos(scaled)
		if err != nil {
			return 0, err
		}
		return 1, pushDuration(state, duration{nanos: nanos}, meta())
	}
}

func durationCompare(compare func(duration, duration) bool) luau.Func {
	return func(state *luau.State) (int, error) {
		a, err := durationArg(state, "compare", 1)
		if err != nil {
			return 0, err
		}
		b, err := durationArg(state, "compare", 2)
		if err != nil {
			return 0, err
		}
		return 1, state.Push(compare(a, b))
	}
}

func instantSubtract(meta luau.Ref) luau.Func {
	return func(state *luau.State) (int, error) {
		a, err := instantArg(state, "subtract", 1)
		if err != nil {
			return 0, err
		}
		b, err := instantArg(state, "subtract", 2)
		if err != nil {
			return 0, err
		}
		return 1, pushDuration(state, duration{nanos: int64(a.time.Sub(b.time))}, meta)
	}
}

func instantCompare(compare func(instant, instant) bool) luau.Func {
	return func(state *luau.State) (int, error) {
		a, err := instantArg(state, "compare", 1)
		if err != nil {
			return 0, err
		}
		b, err := instantArg(state, "compare", 2)
		if err != nil {
			return 0, err
		}
		return 1, state.Push(compare(a, b))
	}
}

func durationArg(state *luau.State, fn string, index int) (duration, error) {
	value, ok := state.ToUserdata(index)
	if !ok {
		return duration{}, fmt.Errorf("time.%s: argument %d must be a Duration", fn, index)
	}
	result, ok := value.(duration)
	if !ok {
		return duration{}, fmt.Errorf("time.%s: argument %d must be a Duration", fn, index)
	}
	return result, nil
}

func instantArg(state *luau.State, fn string, index int) (instant, error) {
	value, ok := state.ToUserdata(index)
	if !ok {
		return instant{}, fmt.Errorf("time.%s: argument %d must be an Instant", fn, index)
	}
	result, ok := value.(instant)
	if !ok {
		return instant{}, fmt.Errorf("time.%s: argument %d must be an Instant", fn, index)
	}
	return result, nil
}

func numberArg(state *luau.State, fn string, index int) (float64, error) {
	value, err := state.Get(index)
	number, ok := value.(float64)
	if err != nil || !ok {
		return 0, fmt.Errorf("time.duration.%s: argument %d must be a number", fn, index)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("time.duration.%s: argument %d must be finite", fn, index)
	}
	return number, nil
}

func durationNanos(nanos float64) (int64, error) {
	nanos = math.Round(nanos)
	if math.IsNaN(nanos) || math.IsInf(nanos, 0) || nanos >= maxInt64Float || nanos < -maxInt64Float {
		return 0, errors.New("duration is outside the supported range")
	}
	return int64(nanos), nil
}
