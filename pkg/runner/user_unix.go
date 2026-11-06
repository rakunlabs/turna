//go:build !windows

package runner

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// Credential returns the credential to run as u. As root the supplementary
// groups are replaced with the groups of u, otherwise the child keeps the
// groups of this process, e.g. gid 0. Without root setgroups is not allowed.
func (u User) Credential() *syscall.Credential {
	c := &syscall.Credential{Uid: u.UID, Gid: u.GID}

	if os.Geteuid() != 0 {
		c.NoSetGroups = true

		return c
	}

	c.Groups = []uint32{}
	if info, err := user.LookupId(strconv.FormatUint(uint64(u.UID), 10)); err == nil {
		if ids, err := info.GroupIds(); err == nil {
			for _, id := range ids {
				if gid, err := ParseID(id); err == nil && gid != u.GID {
					c.Groups = append(c.Groups, gid)
				}
			}
		}
	}

	return c
}
