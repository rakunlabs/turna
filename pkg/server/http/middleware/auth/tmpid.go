package auth

import (
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func slicesUnique(ss ...[]string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)

	for _, v := range ss {
		for _, vv := range v {
			if _, ok := seen[vv]; !ok {
				seen[vv] = struct{}{}
				result = append(result, vv)
			}
		}
	}

	return result
}

// validTmpIDs keeps tmp ids that are not expired yet (only expiresAt check).
func validTmpIDs(ids []data.TmpID) []data.TmpID {
	valid := make([]data.TmpID, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) {
			valid = append(valid, id)
		}
	}

	return valid
}

// validIDs returns active tmp ids (startsAt and expiresAt window check).
func validIDs(ids []data.TmpID) []string {
	valid := make([]string, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) && now.After(id.StartsAt.Time) {
			valid = append(valid, id.ID)
		}
	}

	return valid
}

// validIDsWithTmpID returns active tmp records (startsAt and expiresAt window check).
func validIDsWithTmpID(ids []data.TmpID) []data.TmpID {
	valid := make([]data.TmpID, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) && now.After(id.StartsAt.Time) {
			valid = append(valid, id)
		}
	}

	return valid
}

func normalizeUser(u *data.User) {
	u.RoleIDs = slicesUnique(u.RoleIDs)
	u.SyncRoleIDs = slicesUnique(u.SyncRoleIDs)
	u.PermissionIDs = slicesUnique(u.PermissionIDs)
	u.TmpRoleIDs = validTmpIDs(u.TmpRoleIDs)
	u.TmpPermissionIDs = validTmpIDs(u.TmpPermissionIDs)
}
