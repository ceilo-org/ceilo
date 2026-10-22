package io

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Leoj030/go-luau/luau"
)

func Module() map[string]luau.Func {
	return map[string]luau.Func{
		"input": input,
		"write": write,
	}
}

// ---- arguments and errors ----

// isOptionalArg checks the arg if it is an optional arg or not.
// It returns true if it is, otherwise false.
func isOptionalArg(c *luau.State, i int) bool {
	switch c.TypeName(i) {
	case "no value", "nil":
		return true
	}
	return false
}

// getArg returns argument i, which must be the type of T.
func getArg[T any](c *luau.State, fn string, i int) (T, error) {
	var zero T

	v, err := c.Get(i)
	val, ok := v.(T)
	if err != nil || !ok {
		return zero, fmt.Errorf("fs.%s: argument %d must be a %T", fn, i, zero)
	}

	return val, nil
}

func readT(s *bufio.Scanner, input any) string {
	fmt.Print(input)
	if s.Scan() {
		input := strings.TrimSpace(s.Text())
		return input
	}
	return ""
}

func input(c *luau.State) (int, error) {
	var read string
	scanner := bufio.NewScanner(os.Stdin)

	if isOpt := isOptionalArg(c, 1); isOpt == true {
		read = readT(scanner, "")
	} else {
		i, err := getArg[any](c, "input", 1)
		if err != nil {
			return 0, err
		}
		read = readT(scanner, i)
	}
	return 1, c.Push(read)
}

func write(c *luau.State) (int, error) {
	var output strings.Builder
	for i := 1; i <= c.Top(); i++ {
		value, err := c.Get(i)
		if err != nil {
			return 0, fmt.Errorf("io.write: argument %d: %w", i, err)
		}
		if err := appendValue(&output, value); err != nil {
			return 0, fmt.Errorf("io.write: argument %d: %w", i, err)
		}
	}

	_, err := fmt.Print(output.String())
	return 0, err
}

func appendValue(output *strings.Builder, value any) error {
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if err := appendValue(output, item); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprint(output, value)
	return err
}
