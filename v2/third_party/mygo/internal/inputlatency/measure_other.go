//go:build !darwin

package main

import "errors"

func measure(mode string, events, warmup, x, y, boxes int) error {
	return errors.New("measures macOS only")
}
