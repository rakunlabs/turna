package badger

import (
	"errors"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/timshannon/badgerhold/v4"
)

var ErrFuncExit = errors.New("function exit")

func (b *Badger) Check(req data.CheckRequest) (*data.CheckResponse, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var user *data.User

	var query *badgerhold.Query
	if req.ID != "" {
		query = badgerhold.Where("ID").Eq(req.ID).Index("ID")
	} else if req.Alias != "" {
		query = badgerhold.Where("Alias").Contains(req.Alias)
		if userCached := b.GetCachedID(req.Alias); userCached != nil {
			user = userCached
		}
	}

	if user == nil {
		var userFind data.User

		if err := b.db.FindOne(&userFind, query); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return &data.CheckResponse{
					Allowed: false,
				}, nil
			}

			return nil, err
		}

		user = &userFind

		b.SetCachedID(user.Alias, user.ID)
	}

	if user.Disabled {
		return &data.CheckResponse{
			Allowed: false,
		}, nil
	}

	// get all roles of roles
	roleIDs, err := b.getVirtualRoleIDs(slices.Concat(user.RoleIDs, user.SyncRoleIDs))
	if err != nil {
		return nil, err
	}

	// get permissions based on roles
	var roles []data.Role
	query = badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...)
	if err := b.db.Find(&roles, query); err != nil {
		return nil, err
	}

	permissionIDs := make([]string, 0)
	for _, role := range roles {
		permissionIDs = append(permissionIDs, role.PermissionIDs...)
	}

	query = badgerhold.Where("ID").In(toInterfaceSlice(permissionIDs)...)

	access := false
	if err := b.db.ForEach(query, func(perm *data.Permission) error {
		if CheckAccess(perm, req.Path, req.Method) {
			access = true

			return ErrFuncExit
		}

		return nil
	}); err != nil {
		if !errors.Is(err, ErrFuncExit) {
			return nil, err
		}
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
