package runner

import (
	"fmt"
	"syscall"
)

func terminateProcess(pid int) error {
	// Signal the process group (-pid), not just the process, so that the process
	// and all its children are signaled.
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func sysProcAttr(user string) (*syscall.SysProcAttr, error) {
	// Set process group ID so the cmd and all its children become a new
	// process group. This allows Stop to SIGTERM the cmd's process group
	// without killing this process.
	v := &syscall.SysProcAttr{Setpgid: true}

	if user != "" {
		// parse user
		user, err := UserParser(user)
		if err != nil {
			return nil, fmt.Errorf("cannot parse user: %w", err)
		}

		v.Credential = &syscall.Credential{
			Uid:         user.UID,
			Gid:         user.GID,
			NoSetGroups: true,
		}
	}

	return v, nil
}
