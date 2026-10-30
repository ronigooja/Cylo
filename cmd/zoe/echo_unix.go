//go:build linux || darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// disableTerminalEcho turns off echo on a terminal. It returns isTerminal=false
// for pipes and files so scripted input keeps working.
func disableTerminalEcho(file *os.File) (restore func(), isTerminal bool, err error) {
	fd := int(file.Fd())
	state, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, false, nil
	}
	hidden := *state
	hidden.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &hidden); err != nil {
		return nil, true, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlWriteTermios, state) }, true, nil
}
