// Package json implements the "@std/json" Luau library.
package json

import (
	"encoding/json"
	"fmt"

	"github.com/Leoj030/go-luau/luau"
)

func Module() map[string]luau.Func {
	return map[string]luau.Func{
		"deserialize": deserialize,
		"serialize":   serialize,
	}
}

func deserialize(c *luau.State) (int, error) {
	input, err := stringArg(c, "decode")
	if err != nil {
		return 0, err
	}

	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		return 0, fmt.Errorf("json.decode: %w", err)
	}
	if err := c.Push(value); err != nil {
		return 0, fmt.Errorf("json.decode: %w", err)
	}
	return 1, nil
}

func serialize(c *luau.State) (int, error) {
	if c.Top() < 1 || c.Top() > 2 {
		return 0, fmt.Errorf("json.serialize: expected 1 or 2 arguments, got %d", c.Top())
	}
	value, err := c.Get(1)
	if err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}

	pretty := false
	if c.Top() == 2 {
		if c.TypeName(2) != "boolean" {
			return 0, fmt.Errorf("json.serialize: argument 2 must be a boolean")
		}
		formatValue, err := c.Get(2)
		if err != nil {
			return 0, fmt.Errorf("json.serialize: %w", err)
		}
		var ok bool
		pretty, ok = formatValue.(bool)
		if !ok {
			return 0, fmt.Errorf("json.serialize: argument 2 must be a boolean")
		}
	}

	var encoded []byte
	if pretty {
		encoded, err = json.MarshalIndent(value, "", "  ")
	} else {
		encoded, err = json.Marshal(value)
	}
	if err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}
	if err := c.Push(string(encoded)); err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}
	return 1, nil
}

func stringArg(c *luau.State, fn string) (string, error) {
	if c.Top() != 1 {
		return "", fmt.Errorf("json.%s: expected 1 argument, got %d", fn, c.Top())
	}
	value, err := c.Get(1)
	if err != nil {
		return "", fmt.Errorf("json.%s: %w", fn, err)
	}
	input, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("json.%s: argument 1 must be a string", fn)
	}
	return input, nil
}
