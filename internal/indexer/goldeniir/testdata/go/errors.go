package service

import (
	"errors"
	"fmt"
)

// ErrClosed is a sentinel returned when the store is closed.
var ErrClosed = errors.New("store closed")

// Load returns a sentinel error and a constructed error — both become failure
// modes. The propagated `err` return does not (it has no stable name).
func Load(id string) ([]byte, error) {
	if id == "" {
		return nil, errors.New("empty id")
	}
	data, err := read(id)
	if err != nil {
		return nil, err // propagated — intentionally not a named failure mode
	}
	if len(data) == 0 {
		return nil, ErrClosed
	}
	return data, nil
}

// Save wraps failures with fmt.Errorf and panics on a nil argument.
func Save(id string, data []byte) error {
	if data == nil {
		panic("nil data")
	}
	if id == "" {
		return fmt.Errorf("save %q: %w", id, ErrClosed)
	}
	return nil
}

// pure has no error return, so it contributes no failure modes.
func pure(x int) int {
	return x * 2
}

func read(id string) ([]byte, error) { return nil, nil }
