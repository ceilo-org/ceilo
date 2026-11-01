// Package time implements the "@std/time" Luau library.
package time

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Leoj030/go-luau/vm"
)

const (
	nanosPerMicro  = 1e3
	nanosPerMilli  = 1e6
	nanosPerSecond = 1e9
	maxInt64Float  = 9223372036854775808.0
)

var errOutOfRange = errors.New("duration is outside the supported range")

// duration is a length of time. Scripts see it as userdata.
type duration struct{ nanos int64 }

// instant is a point in time. Scripts see it as userdata.
type instant struct{ t time.Time }

// Modules registers the Duration and Instant types in v and returns the
// modules to register: the library itself at path, and its duration
// submodule at path/duration. Call it before running any script.
func Module(v *vm.VM) map[string]any {
	if err := vm.RegisterType[duration](v, vm.TypeSpec{
		Name:    "Duration",
		Methods: durationMethods(),
		Meta:    durationOperators(),
	}); err != nil {
		panic(fmt.Errorf("time: %w", err))
	}
	if err := vm.RegisterType[instant](v, vm.TypeSpec{
		Name: "Instant",
		Methods: map[string]any{
			"elapsed": func(i instant) float64 { return time.Since(i.t).Seconds() },
		},
		Meta: map[string]any{
			"__sub": func(a, b instant) duration { return duration{int64(a.t.Sub(b.t))} },
			"__eq":  func(a, b instant) bool { return a.t.Equal(b.t) },
			"__lt":  func(a, b instant) bool { return a.t.Before(b.t) },
			"__le":  func(a, b instant) bool { return !a.t.After(b.t) },
		},
	}); err != nil {
		panic(fmt.Errorf("time: %w", err))
	}

	durations := durationMembers()
	return map[string]any{
		"now":      func() instant { return instant{t: time.Now()} },
		"since":    func(i instant) float64 { return time.Since(i.t).Seconds() },
		"duration": vm.Frozen(durations),
	}
}

func DurationModule() map[string]any {
	return durationMembers()
}

// durationMembers are the constructors: duration.seconds(5), and so on.
func durationMembers() map[string]any {
	units := map[string]float64{
		"nanoseconds":  1,
		"microseconds": nanosPerMicro,
		"milliseconds": nanosPerMilli,
		"seconds":      nanosPerSecond,
		"minutes":      60 * nanosPerSecond,
		"hours":        60 * 60 * nanosPerSecond,
		"days":         24 * 60 * 60 * nanosPerSecond,
		"weeks":        7 * 24 * 60 * 60 * nanosPerSecond,
	}
	members := make(map[string]any, len(units)+1)
	for name, scale := range units {
		members[name] = constructor(name, scale)
	}
	members["create"] = func(seconds, subseconds float64) (duration, error) {
		if !finite(seconds) || !finite(subseconds) {
			return duration{}, errors.New("time.duration.create: arguments must be finite")
		}
		nanos, err := toNanos(seconds*nanosPerSecond + subseconds)
		if err != nil {
			return duration{}, fmt.Errorf("time.duration.create: %w", err)
		}
		return duration{nanos}, nil
	}
	return members
}

func constructor(name string, scale float64) func(float64) (duration, error) {
	return func(n float64) (duration, error) {
		if !finite(n) {
			return duration{}, fmt.Errorf("time.duration.%s: argument 1 must be finite", name)
		}
		nanos, err := toNanos(n * scale)
		if err != nil {
			return duration{}, fmt.Errorf("time.duration.%s: %w", name, err)
		}
		return duration{nanos}, nil
	}
}

// durationMethods are what a script calls on a duration: d:toSeconds().
func durationMethods() map[string]any {
	in := func(unit float64) func(duration) float64 {
		return func(d duration) float64 { return float64(d.nanos) / unit }
	}
	subsec := func(unit float64) func(duration) float64 {
		return func(d duration) float64 { return float64(d.nanos%int64(nanosPerSecond)) / unit }
	}
	return map[string]any{
		"toNanoseconds":  in(1),
		"toMicroseconds": in(nanosPerMicro),
		"toMilliseconds": in(nanosPerMilli),
		"toSeconds":      in(nanosPerSecond),
		"toMinutes":      in(60 * nanosPerSecond),
		"toHours":        in(60 * 60 * nanosPerSecond),
		"toDays":         in(24 * 60 * 60 * nanosPerSecond),
		"toWeeks":        in(7 * 24 * 60 * 60 * nanosPerSecond),
		"subsecnanos":    subsec(1),
		"subsecmicros":   subsec(nanosPerMicro),
		"subsecmillis":   subsec(nanosPerMilli),
	}
}

// durationOperators are the operators: d + d, d - d, d * n, d / n, and the comparisons.
func durationOperators() map[string]any {
	return map[string]any{
		"__add": func(a, b duration) (duration, error) {
			if b.nanos > 0 && a.nanos > math.MaxInt64-b.nanos || b.nanos < 0 && a.nanos < math.MinInt64-b.nanos {
				return duration{}, errOutOfRange
			}
			return duration{a.nanos + b.nanos}, nil
		},
		"__sub": func(a, b duration) (duration, error) {
			if b.nanos < 0 && a.nanos > math.MaxInt64+b.nanos || b.nanos > 0 && a.nanos < math.MinInt64+b.nanos {
				return duration{}, errOutOfRange
			}
			return duration{a.nanos - b.nanos}, nil
		},
		"__mul": func(d duration, k float64) (duration, error) { return scaled(float64(d.nanos) * k) },
		"__div": func(d duration, k float64) (duration, error) {
			if k == 0 {
				return duration{}, errors.New("duration division by zero")
			}
			return scaled(float64(d.nanos) / k)
		},
		"__eq": func(a, b duration) bool { return a == b },
		"__lt": func(a, b duration) bool { return a.nanos < b.nanos },
		"__le": func(a, b duration) bool { return a.nanos <= b.nanos },
	}
}

func scaled(nanos float64) (duration, error) {
	n, err := toNanos(nanos)
	return duration{n}, err
}

func toNanos(nanos float64) (int64, error) {
	nanos = math.Round(nanos)
	if math.IsNaN(nanos) || math.IsInf(nanos, 0) || nanos >= maxInt64Float || nanos < -maxInt64Float {
		return 0, errOutOfRange
	}
	return int64(nanos), nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
