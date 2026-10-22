local io = {}

--- Writes one or more strings to standard output without a trailing newline.
function io.write(...: any): ()
	error("unimplemented")
end

--- Reads a line from standard input. If `prompt` is provided, it is written to stdout before waiting for input.
--- User input can be provided via command line stdin, piped input, or redirection from a file. See `examples/user_input.luau`.
function io.input(prompt: string?): string
	error("unimplemented")
end

return io
