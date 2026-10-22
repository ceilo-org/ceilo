-- lute-lint-global-ignore(unused_variable)
local process = {}

--- How a child process's standard streams should be handled.
export type StdioKind = "default" | "inherit" | "none"

--- Options for `process.run`.
export type ProcessRunOptions = {
	cwd: string?,
	stdio: StdioKind?,
	env: { [string]: string }?,
	timeout: number?,
}

--- Options for `process.system`.
export type ProcessSystemOptions = {
	system: string?,
	cwd: string?,
	stdio: StdioKind?,
	env: { [string]: string }?,
	timeout: number?,
}

--- Result of a completed process.
export type ProcessResult = {
	stdout: string,
	stderr: string,
	ok: boolean,
	exitcode: number,
	signal: string?,
	timedOut: boolean?,
}

--- A handle returned by `process.onSignal`.
export type SignalHandle = {
	close: (self: SignalHandle) -> (),
}

--- An operating system signal name.
export type Signal =
	| "SIGINT"
	| "SIGTERM"
	| "SIGHUP"
	| "SIGQUIT"
	| "SIGUSR1"
	| "SIGUSR2"
	| "SIGWINCH"
	| "SIGPIPE"
	| "SIGBREAK"
	| "SIGALRM"

--- Command-line arguments passed to the current script.
process.args = {} :: { string }
--- Environment variables of the current process.
process.env = {} :: { [string]: string }

--- Returns the current user's home directory.
function process.homedir(): string
	error("not implemented")
end

--- Returns the current working directory.
function process.cwd(): string
	error("not implemented")
end

--- Runs a program and returns its exit status and captured output.
function process.run(args: { string }, options: ProcessRunOptions?): ProcessResult
	error("not implemented")
end

--- Runs a command through the system shell and returns its result.
function process.system(command: string, options: ProcessSystemOptions?): ProcessResult
	error("not implemented")
end

--- Exits the current process with the given exit code.
function process.exit(exitcode: number): never
	error("not implemented")
end

--- Returns the path of the currently running executable.
function process.execPath(): string
	error("not implemented")
end

--- Returns the current process ID.
function process.pid(): number
	error("not implemented")
end

--- Registers a callback for a process signal and returns a handle to deregister it.
function process.onSignal(signal: Signal, callback: () -> ()): SignalHandle
	error("not implemented")
end

return process
