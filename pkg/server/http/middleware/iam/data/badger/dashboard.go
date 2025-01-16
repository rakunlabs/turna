package badger

import (
	"github.com/dgraph-io/badger/v4"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) Dashboard() (*data.Dashboard, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var extendRoles []data.RoleExtended
	var totalRoles uint64
	var totalUsers uint64
	var totalPermissions uint64
	var totalServiceAccounts uint64

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error

		totalRoles, err = b.db.TxCount(txn, data.Role{}, nil)
		if err != nil {
			return err
		}

		var roles []data.Role

		if err := b.db.TxFind(txn, &roles, nil); err != nil {
			return err
		}

		extendRoles = make([]data.RoleExtended, 0, len(roles))
		for _, role := range roles {
			extendRole, err := b.ExtendRole(txn, true, true, true, &role)
			if err != nil {
				return err
			}

			extendRoles = append(extendRoles, extendRole)
		}

		totalUsers, err = b.db.TxCount(txn, data.User{}, badgerhold.Where("ServiceAccount").Eq(false))
		if err != nil {
			return err
		}

		totalPermissions, err = b.db.TxCount(txn, data.Permission{}, nil)
		if err != nil {
			return err
		}

		totalServiceAccounts, err = b.db.TxCount(txn, data.User{}, badgerhold.Where("ServiceAccount").Eq(true))
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &data.Dashboard{
		Roles: extendRoles,

		TotalRoles:           totalRoles,
		TotalUsers:           totalUsers,
		TotalPermissions:     totalPermissions,
		TotalServiceAccounts: totalServiceAccounts,
	}, nil
}
