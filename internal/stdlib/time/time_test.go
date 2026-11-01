package time

import (
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
	"github.com/Leoj030/go-luau/vm"
)

func registerTimeModules(t *testing.T, v *vm.VM) {
	t.Helper()
	if err := v.Module("@std/time", Module(v)); err != nil {
		t.Fatal(err)
	}
	if err := v.Module("@std/time/duration", DurationModule()); err != nil {
		t.Fatal(err)
	}
}

func TestTimeAndDurationModules(t *testing.T) {
	v, err := vm.New()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.State().OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	registerTimeModules(t, v)

	script := `
		local time = require("@std/time")
		local duration = time.duration
		local requiredDuration = require("@std/time/duration")

		local oneSecond = duration.seconds(1)
		local quarterSecond = requiredDuration.create(0, 250000000)
		local combined = oneSecond + quarterSecond
		assert(combined:toNanoseconds() == 1250000000)
		assert(combined:toSeconds() == 1.25)
		assert(combined:subsecnanos() == 250000000)
		assert(combined:subsecmicros() == 250000)
		assert(combined:subsecmillis() == 250)
		assert((combined - quarterSecond):toSeconds() == 1)
		assert((oneSecond * 2):toSeconds() == 2)
		assert((oneSecond / 4):toSeconds() == 0.25)
		assert(oneSecond < combined and oneSecond <= combined)
		assert(oneSecond == requiredDuration.seconds(1))
		assert(duration.weeks(1):toDays() == 7)
		assert(duration.microseconds(1):toNanoseconds() == 1000)
		assert(duration.milliseconds(1):toNanoseconds() == 1000000)
		assert(duration.minutes(1):toSeconds() == 60)
		assert(duration.hours(1):toMinutes() == 60)
		assert(duration.days(1):toHours() == 24)

		local first = time.now()
		assert(first:elapsed() >= 0)
		assert(time.since(first) >= 0)
		local second = time.now()
		assert(second >= first and first <= second)
		local elapsed = second - first
		assert(elapsed:toNanoseconds() >= 0)
		assert(getmetatable(first) == "The metatable is locked!")
		assert(getmetatable(oneSecond) == "The metatable is locked!")
	`
	if err := v.Do(script); err != nil {
		t.Fatal(err)
	}
}

func TestDurationRejectsInvalidOperations(t *testing.T) {
	v, err := vm.New()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.State().OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	registerTimeModules(t, v)

	for _, script := range []string{
		`require("@std/time").duration.seconds(0 / 0)`,
		`require("@std/time/duration").seconds(1) / 0`,
		`require("@std/time").duration.seconds(1) + 1`,
	} {
		if err := v.Do(script); err == nil {
			t.Errorf("DoString(%q) succeeded, want an error", script)
		}
	}
}
