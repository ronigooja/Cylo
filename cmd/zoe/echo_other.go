//go:build !linux && !darwin

package main

import (
	"errors"
	"os"
)

func disableTerminalEcho(*os.File) (func(), bool, error) {
	return nil, true, errors.New("hidden input is not supported on this platform; use --token-file")
}
