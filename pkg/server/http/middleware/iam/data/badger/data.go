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
			user.TmpRoleIDs = validTmpIDs(user.TmpRoleIDs)
			user.TmpPermissionIDs = validTmpIDs(user.TmpPermissionIDs)

			user.AllRoleIDs = totalID(WithTotalID(user.RoleIDs, user.SyncRoleIDs), WithTotalTmpID(user.TmpRoleIDs))
			user.AllPermIDs = totalID(WithTotalID(user.PermissionIDs), WithTotalTmpID(user.TmpPermissionIDs))

			return b.db.TxUpdate(txn, user.ID, user)
		})
	})
}
