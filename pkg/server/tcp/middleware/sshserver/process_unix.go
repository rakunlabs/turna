//go:build unix

package sshserver

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func defaultShell() string { return "/bin/sh" }

func setCommandUser(cmd *exec.Cmd, username string) error {
	if username == "" {
		return nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("lookup os user %q: %w", username, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse uid for %q: %w", username, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse gid for %q: %w", username, err)
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("list groups for %q: %w", username, err)
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, value := range groupIDs {
		groupID, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parse group id for %q: %w", username, err)
		}
		groups = append(groups, uint32(groupID))
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: uint32(uid), Gid: uint32(gid), Groups: groups,
	}}
	cmd.Dir = u.HomeDir
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
	return nil
}
