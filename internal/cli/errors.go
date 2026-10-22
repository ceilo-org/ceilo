package cli

import "fmt"

// UsageError means the command line was wrong. The program prints it with a
// hint to read the help, and exits with code 2.
type UsageError struct {
	Msg     string
	Command string // the command to point the user at; empty for the general help
}

func (e *UsageError) Error() string { return e.Msg }

func usageErrorf(command, format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...), Command: command}
}

// ExitError ends the program with a specific exit code. If Err is not nil it
// is printed first. A script that calls process.exit(3) would end this way.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }
