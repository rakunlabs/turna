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

func sysProcAttr(_ string) (*syscall.SysProcAttr, error) {
	return &syscall.SysProcAttr{}, nil
}
