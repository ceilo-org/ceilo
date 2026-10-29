package bcrypt

import (
	"errors"
	"fmt"
	"math"

	"github.com/Leoj030/go-luau/luau"
	"golang.org/x/crypto/bcrypt"
)

func Module() map[string]luau.Func {
	return map[string]luau.Func{
		"hash":    hash,
		"compare": compare,
	}
}

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
		return zero, fmt.Errorf("ceilo/bcrypt.%s: argument %d must be a %T", fn, i, zero)
	}

	return val, nil
}

// hash takes a string password and returns its bcrypt hash.
func hash(c *luau.State) (int, error) {
	var defCost int

	pass, err := getArg[string](c, "hash", 1)
	if err != nil {
		return 0, err
	}

	if isOpt := isOptionalArg(c, 2); isOpt == true {
		defCost = bcrypt.DefaultCost
	} else {
		cost, err := getArg[float64](c, "hash", 2)
		if err != nil {
			return 0, err
		}
		if math.Trunc(cost) != cost || cost < float64(bcrypt.MinCost) || cost > float64(bcrypt.MaxCost) {
			return 0, fmt.Errorf("ceilo/bcrypt.hash: cost must be an integer between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
		}
		defCost = int(cost)
	}

	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), defCost)
	if err != nil {
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return 0, fmt.Errorf("ceilo/bcrypt.hash: password exceeds the 72-byte bcrypt limit")
		}
		return 0, fmt.Errorf("ceilo/bcrypt.hash: failed to generate hash: %w", err)
	}

	return 1, c.Push(string(bytes))
}

// compare compares a raw password against the stored bcrypt hash.
func compare(c *luau.State) (int, error) {
	rawPass, err := getArg[string](c, "compare", 1)
	if err != nil {
		return 0, err
	}

	hashedPass, err := getArg[string](c, "compare", 2)
	if err != nil {
		return 0, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPass), []byte(rawPass))
	if err != nil && errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return 1, c.Push(false)
	}
	if err != nil {
		return 0, fmt.Errorf("ceilo/bcrypt.compare: invalid hashed password")
	}
	return 1, c.Push(true)
}
