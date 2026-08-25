package runner

import (
	"fmt"
	"os/user"
	"strconv"
	"strings"
)

type User struct {
	UID uint32
	GID uint32
}

// UserParser parse user string to uid and gid.
func UserParser(u string) (User, error) {
	v := strings.Split(u, ":")

	switch len(v) {
	case 1:
		uid, err := UToID(v[0], false)
		if err != nil {
			return User{}, err
		}

		// resolve the target user's primary group, not the current process's group
		var gid uint32
		if userInfo, lookupErr := user.Lookup(v[0]); lookupErr == nil {
			gid, err = ParseID(userInfo.Gid)
		} else if userInfo, lookupErr := user.LookupId(strconv.FormatUint(uint64(uid), 10)); lookupErr == nil {
			gid, err = ParseID(userInfo.Gid)
		} else {
			// numeric uid without a passwd entry; fall back to gid = uid
			gid = uid
		}

		if err != nil {
			return User{}, err
		}

		return User{
			UID: uid,
			GID: gid,
		}, nil
	case 2:
		uid, err := UToID(v[0], false)
		if err != nil {
			return User{}, err
		}

		gid, err := UToID(v[1], true)
		if err != nil {
			return User{}, err
		}

		return User{
			UID: uid,
			GID: gid,
		}, nil
	default:
		return User{}, fmt.Errorf("invalid user format: %s", u)
	}
}

func UToID(u string, group bool) (uint32, error) {
	uID, err := strconv.ParseUint(u, 10, 32)
	if err != nil {
		// ask system
		if group {
			gName, err := user.LookupGroup(u)
			if err != nil {
				return 0, err
			}

			return ParseID(gName.Gid)
		}

		uName, err := user.Lookup(u)
		if err != nil {
			return 0, err
		}

		return ParseID(uName.Uid)
	}

	return uint32(uID), nil
}

func ParseID(u string) (uint32, error) {
	uID, err := strconv.ParseUint(u, 10, 32)
	if err != nil {
		return 0, err
	}

	return uint32(uID), nil
}
