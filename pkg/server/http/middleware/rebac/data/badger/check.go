package badger

import (
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/timshannon/badgerhold/v4"
)

var ErrFuncExit = errors.New("function exit")

func (b *Badger) Check(req data.CheckRequest) (*data.CheckResponse, error) {
	var query *badgerhold.Query
	if req.ID != "" {
		query = badgerhold.Where("ID").Eq(req.ID)
	} else if req.Alias != "" {
		query = badgerhold.Where("Alias").Contains(req.Alias)
	}

	var user data.User
	if err := b.db.FindOne(&user, query); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, nil
		}

		return nil, err
	}

	// get permissions based on roles
	var roles []data.Role
	query = badgerhold.Where("ID").In(toInterfaceSlice(user.Roles)...)
	if err := b.db.Find(&roles, query); err != nil {
		return nil, err
	}

	permissionNames := make([]string, 0)
	for _, role := range roles {
		permissionNames = append(permissionNames, role.Permissions...)
	}

	query = badgerhold.Where("ID").In(toInterfaceSlice(permissionNames)...)

	access := false
	err := b.db.ForEach(query, func(perm *data.Permission) error {
		if CheckAccess(perm, req.Path, req.Method) {
			access = true

			return ErrFuncExit
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrFuncExit) {
			return nil, err
		}
	}

	return &data.CheckResponse{
		Allowed: access,
	}, nil
}

func CheckAccess(perm *data.Permission, pathRequest, method string) bool {
	for _, req := range perm.Requests {
		if !slices.ContainsFunc(req.Methods, func(v string) bool {
			if v == "*" {
				return true
			}

			return strings.EqualFold(v, method)
		}) {
			continue
		}

		if v, _ := path.Match(req.Path, pathRequest); v {
			return true
		}
	}

	return false
}
