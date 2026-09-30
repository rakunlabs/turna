//go:build windows

package sshserver

import (
	"errors"
	"os/exec"
)

func startPTY(_ *exec.Cmd, _, _ uint32) (terminal, error) {
	return nil, errors.New("PTY is not supported on Windows")
}
