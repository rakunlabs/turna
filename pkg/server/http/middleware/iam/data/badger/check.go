package badger

import (
	"errors"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/dgraph-io/badger/v4"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	"github.com/timshannon/badgerhold/v4"
)

var (
	ErrFuncExit = errors.New("function exit")
	ErrNotAllow = errors.New("not allowed")
)

func (b *Badger) Check(req data.CheckRequest) (*data.CheckResponse, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	access := false

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var user *data.User

		var query *badgerhold.Query
		if req.ID != "" {
			query = badgerhold.Where("ID").Eq(req.ID).Index("ID")
		} else if req.Alias != "" {
			query = badgerhold.Where("Alias").Contains(req.Alias)
			if userCached := b.getCachedID(txn, req.Alias); userCached != nil {
				user = userCached
			}
		}

		if user == nil {
			var userFind data.User

			if err := b.db.TxFindOne(txn, &userFind, query); err != nil {
				if errors.Is(err, badgerhold.ErrNotFound) {
					return ErrNotAllow
				}

				return err
			}

			user = &userFind

			b.SetCachedID(user.Alias, user.ID)
		}

		if user.Disabled {
			return ErrNotAllow
		}

		// get all roles of roles
		roleIDs, err := b.getVirtualRoleIDs(txn, slices.Concat(user.RoleIDs, user.SyncRoleIDs))
		if err != nil {
			return err
		}

		// get permissions based on roles
		var roles []data.Role
		query = badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...)
		if err := b.db.TxFind(txn, &roles, query); err != nil {
			return err
		}

		permissionMapIDs := make(map[string]struct{})
		for _, permID := range user.PermissionIDs {
			permissionMapIDs[permID] = struct{}{}
		}
		for _, role := range roles {
			for _, permID := range role.PermissionIDs {
				permissionMapIDs[permID] = struct{}{}
			}
		}

		query = badgerhold.Where("ID").In(toInterfaceSliceMap(permissionMapIDs)...)

		if err := b.db.TxForEach(txn, query, func(perm *data.Permission) error {
			if CheckAccess(perm, req.Path, req.Method) {
				access = true

				return ErrFuncExit
			}

			return nil
		}); err != nil {
			if !errors.Is(err, ErrFuncExit) {
				return err
			}
		}

		return nil
	}); err != nil {
		if errors.Is(err, ErrNotAllow) {
			return &data.CheckResponse{
				Allowed: false,
			}, nil
		}

		return nil, err
	}

	return &data.CheckResponse{
		Allowed: access,
	}, nil
}

func CheckAccess(perm *data.Permission, pathRequest, method string) bool {
	for _, req := range perm.Resources {
		if !checkMethod(req.Methods, method) {
			continue
		}

		if checkPath(req.Path, pathRequest) {
			return true
		}
	}

	return false
}

func checkMethod(methods []string, method string) bool {
	return slices.ContainsFunc(methods, func(v string) bool {
		if v == "*" {
			return true
		}

		return strings.EqualFold(v, method)
	})
}

func checkPath(pattern, pathRequest string) bool {
	v, _ := doublestar.Match(pattern, pathRequest)

	return v
}
