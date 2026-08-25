package runner

import (
	"os"
	"syscall"
)

// Stop stops the command by sending its process group a SIGTERM signal.
func terminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return p.Kill()
}

// killProcess force kills the process; on Windows terminateProcess is already a
// hard kill, so this is the same operation.
func killProcess(pid int) error {
	return terminateProcess(pid)
}

func sysProcAttr(_ string) (*syscall.SysProcAttr, error) {
	return &syscall.SysProcAttr{}, nil
}
