local duration = {}
local duration_ops = {}

--- Creates a `Duration` from `seconds` and a fractional `subsecnanos` nanosecond component.
function duration.create(seconds: number, subsecnanos: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of nanoseconds.
function duration.nanoseconds(nanoseconds: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of microseconds.
function duration.microseconds(microseconds: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of milliseconds.
function duration.milliseconds(milliseconds: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of seconds.
function duration.seconds(seconds: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of minutes.
function duration.minutes(minutes: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of hours.
function duration.hours(hours: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of days.
function duration.days(days: number): Duration
	error("not implemented")
end

--- Creates a `Duration` from a number of weeks.
function duration.weeks(weeks: number): Duration
	error("not implemented")
end

function duration_ops.toNanoseconds(self: Duration): number
	error("not implemented")
end

function duration_ops.toMicroseconds(self: Duration): number
	error("not implemented")
end

function duration_ops.toMilliseconds(self: Duration): number
	error("not implemented")
end

function duration_ops.toSeconds(self: Duration): number
	error("not implemented")
end

function duration_ops.toMinutes(self: Duration): number
	error("not implemented")
end

function duration_ops.toHours(self: Duration): number
	error("not implemented")
end

function duration_ops.toDays(self: Duration): number
	error("not implemented")
end

function duration_ops.toWeeks(self: Duration): number
	error("not implemented")
end

function duration_ops.subsecnanos(self: Duration): number
	error("not implemented")
end

function duration_ops.subsecmicros(self: Duration): number
	error("not implemented")
end

type Frozen = typeof(table.freeze({}))
--- A time duration with nanosecond precision. Supports arithmetic via `+`, `-`, `*`, `/`, and comparison operators.
export type Duration = setmetatable<Frozen, {
	read __metatable: "The metatable is locked!",
	read __index: typeof(table.freeze(duration_ops)),
	read __add: (Duration, Duration) -> Duration,
	read __sub: (Duration, Duration) -> Duration,
	read __mul: (Duration, number) -> Duration,
	read __div: (Duration, number) -> Duration,
	read __eq: (Duration, Duration) -> boolean,
	read __lt: (Duration, Duration) -> boolean,
	read __le: (Duration, Duration) -> boolean,
}>

return duration
