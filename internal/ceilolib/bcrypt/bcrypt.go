package bcrypt

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func Module() map[string]any {
	return map[string]any{
		"hash":    hash,
		"compare": compare,
	}
}

// hash takes a string password and returns its bcrypt hash.
func hash(pass string, cost *int) (string, error) {
	defCost := bcrypt.DefaultCost
	if cost != nil {
		if *cost < bcrypt.MinCost || *cost > bcrypt.MaxCost {
			return "", fmt.Errorf("ceilo/bcrypt.hash2: cost must be an integer between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
		}
		defCost = *cost
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(pass), defCost)
	if err != nil {
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return "", fmt.Errorf("ceilo/bcrypt.hash2: password exceeds the 72-byte bcrypt limit")
		}
		return "", fmt.Errorf("ceilo/bcrypt.hash2: failed to generate hash: %w", err)
	}
	return string(hashed), err
}

// compare compares a raw password against the stored bcrypt hash.
func compare(pass, hashedPass string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPass), []byte(pass))
	if err != nil && errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ceilo/bcrypt.compare: invalid hashed password")
	}
	return true, nil
}
