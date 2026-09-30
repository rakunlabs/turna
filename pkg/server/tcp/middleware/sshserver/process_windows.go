//go:build windows

package sshserver

import (
	"errors"
	"os/exec"
)

func defaultShell() string { return "cmd.exe" }

func setCommandUser(_ *exec.Cmd, username string) error {
	if username != "" {
		return errors.New("os_user is not supported on Windows")
	}
	return nil
}
