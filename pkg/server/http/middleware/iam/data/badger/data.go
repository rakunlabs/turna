package badger

import (
	"context"

	"github.com/dgraph-io/badger/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) FixData(ctx context.Context) error {
	// all users with tmp roles or permissions
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		// find all user
		return b.db.TxForEach(txn, &badgerhold.Query{}, func(user *data.User) error {
			tmpRoleIDs := validTmpIDs(user.TmpRoleIDs)
			tmpPermissionIDs := validTmpIDs(user.TmpPermissionIDs)

			allRoleIDs := slicesUnique(user.RoleIDs, user.SyncRoleIDs)
			allRoleTmpIDs := totalID(WithTotalID(user.RoleIDs, user.SyncRoleIDs), WithTotalTmpID(tmpRoleIDs))
			allPermTmpIDs := totalID(WithTotalID(user.PermissionIDs), WithTotalTmpID(tmpPermissionIDs))

			if isDiffTmpID(user.TmpRoleIDs, tmpRoleIDs) ||
				isDiffTmpID(user.TmpPermissionIDs, tmpPermissionIDs) ||
				isDiffSliceString(user.AllRolePermanentIDs, allRoleIDs) ||
				isDiffMixID(user.AllRoleTmpIDs, allRoleTmpIDs) ||
				isDiffMixID(user.AllPermTmpIDs, allPermTmpIDs) {
				user.TmpRoleIDs = tmpRoleIDs
				user.TmpPermissionIDs = tmpPermissionIDs
				user.AllRoleTmpIDs = allRoleTmpIDs
				user.AllRolePermanentIDs = allRoleIDs
				user.AllPermTmpIDs = allPermTmpIDs

				return b.db.TxUpdate(txn, user.ID, user)
			}

			return nil
		})
	})
}

func isDiffSliceString(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}

	m := make(map[string]struct{}, len(a))
	for _, s := range a {
		m[s] = struct{}{}
	}

	for _, s := range b {
		if _, ok := m[s]; !ok {
			return true
		}
	}

	return false
}

func isDiffMixID(a, b []data.MixID) bool {
	if len(a) != len(b) {
		return true
	}

	m := make(map[string]data.MixID, len(a))
	for _, s := range a {
		m[s.ID] = s
	}

	for _, s := range b {
		v, ok := m[s.ID]
		if !ok {
			return true
		}

		if v.IsTmp != s.IsTmp {
			return true
		}

		if !v.ExpiresAt.Equal(s.ExpiresAt.Time) {
			return true
		}
	}

	return false
}

func isDiffTmpID(a, b []data.TmpID) bool {
	if len(a) != len(b) {
		return true
	}

	m := make(map[string]data.TmpID, len(a))
	for _, s := range a {
		m[s.ID] = s
	}

	for _, s := range b {
		v, ok := m[s.ID]
		if !ok {
			return true
		}

		if !v.ExpiresAt.Equal(s.ExpiresAt.Time) {
			return true
		}

		if !v.StartsAt.Equal(s.StartsAt.Time) {
			return true
		}
	}

	return false
}
