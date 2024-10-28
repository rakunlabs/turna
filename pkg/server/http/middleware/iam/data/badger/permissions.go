package badger

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dgraph-io/badger/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetPermissions(req data.GetPermissionRequest) (*data.Response[[]data.Permission], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var permissions []data.Permission
	var count uint64

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error

		badgerHoldQuery := &badgerhold.Query{}

		if req.ID != "" {
			badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
		} else {
			var badgerHoldQueryInternal *badgerhold.Query
			if req.Name != "" {
				badgerHoldQueryInternal = badgerhold.Where("Name").MatchFunc(matchAll(req.Name))
			}

			if req.Method != "" {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("Resources").MatchFunc(matchRequestMethod(req.Method))
				} else {
					badgerHoldQueryInternal = badgerhold.Where("Resources").MatchFunc(matchRequestMethod(req.Method))
				}
			}

			if req.Path != "" {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("Resources").MatchFunc(matchRequestPath(req.Path))
				} else {
					badgerHoldQueryInternal = badgerhold.Where("Resources").MatchFunc(matchRequestPath(req.Path))
				}
			}

			if req.Description != "" {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("Description").MatchFunc(matchAll(req.Description))
				} else {
					badgerHoldQueryInternal = badgerhold.Where("Description").MatchFunc(matchAll(req.Description))
				}
			}

			if len(req.Data) > 0 {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("Data").MatchFunc(matchData(req.Data))
				} else {
					badgerHoldQueryInternal = badgerhold.Where("Data").MatchFunc(matchData(req.Data))
				}
			}

			if badgerHoldQueryInternal != nil {
				badgerHoldQuery = badgerHoldQueryInternal
			}
		}

		count, err = b.db.TxCount(txn, data.Permission{}, badgerHoldQuery)
		if err != nil {
			return err
		}

		if req.Offset > 0 {
			badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
		}
		if req.Limit > 0 {
			badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
		}

		if err := b.db.TxFind(txn, &permissions, badgerHoldQuery); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &data.Response[[]data.Permission]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: permissions,
	}, nil
}

func (b *Badger) GetPermission(id string) (*data.Permission, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var permission data.Permission

	if err := b.db.Get(id, &permission); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("permission with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	return &permission, nil
}

func (b *Badger) CreatePermission(permission data.Permission) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		// Check if permission with the same name exists
		ff := &data.Permission{}
		if err := b.db.TxFindOne(txn, ff, badgerhold.Where("Name").Eq(permission.Name).Index("Name")); err != nil {
			if !errors.Is(err, badgerhold.ErrNotFound) {
				return err
			}
		} else {
			return fmt.Errorf("permission with name %s already exists %s; %w", permission.Name, ff.ID, data.ErrConflict)
		}

		permission.ID = ulid.Make().String()

		if err := b.db.TxInsert(txn, permission.ID, permission); err != nil {
			if errors.Is(err, badgerhold.ErrKeyExists) {
				return fmt.Errorf("permission with ID %s already exists; %w", permission.ID, data.ErrConflict)
			}
		}

		return nil
	}); err != nil {
		return "", err
	}

	return permission.ID, nil
}

func (b *Badger) CreatePermissions(permission []data.Permission) ([]string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	ids := make([]string, 0, len(permission))

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		for i := range permission {
			// Check if permission with the same name exists
			if err := b.db.TxFindOne(txn, &data.Permission{}, badgerhold.Where("Name").Eq(permission[i].Name).Index("Name")); err != nil {
				if !errors.Is(err, badgerhold.ErrNotFound) {
					return err
				}
			} else {
				continue
			}

			permission[i].ID = ulid.Make().String()

			if err := b.db.TxInsert(txn, permission[i].ID, permission[i]); err != nil {
				if errors.Is(err, badgerhold.ErrKeyExists) {
					continue
				}

				return err
			}

			ids = append(ids, permission[i].ID)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return ids, nil
}

func (b *Badger) editPermission(id string, fn func(*badger.Txn, *data.Permission) error) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		var foundPermission data.Permission
		if err := b.db.TxFindOne(txn, &foundPermission, badgerhold.Where("ID").Eq(id).Index("ID")); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("permission with id %s not found; %w", id, data.ErrNotFound)
			}

			return err
		}

		if err := fn(txn, &foundPermission); err != nil {
			return err
		}

		if err := b.db.TxUpdate(txn, foundPermission.ID, foundPermission); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (b *Badger) PatchPermission(id string, patch data.PermissionPatch) error {
	return b.editPermission(id, func(txn *badger.Txn, foundPermission *data.Permission) error {
		if patch.Name != nil && *patch.Name != "" && *patch.Name != foundPermission.Name {
			// Check if permission with the same name exists
			ff := &data.Permission{}
			if err := b.db.TxFindOne(txn, ff, badgerhold.Where("Name").Eq(patch.Name).Index("Name")); err != nil {
				if !errors.Is(err, badgerhold.ErrNotFound) {
					return err
				}
			} else {
				return fmt.Errorf("permission with name %s already exists with %s; %w", *patch.Name, ff.ID, data.ErrConflict)
			}

			foundPermission.Name = *patch.Name
		}

		if patch.Description != nil {
			foundPermission.Description = *patch.Description
		}

		if patch.Resources != nil {
			foundPermission.Resources = *patch.Resources
		}

		if patch.Data != nil {
			foundPermission.Data = patch.Data
		}

		return nil
	})
}

func (b *Badger) PutPermission(permission data.Permission) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Update(permission.ID, permission); err != nil {
		return err
	}

	return nil
}

func (b *Badger) DeletePermission(id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		// Delete the permission
		if err := b.db.TxDelete(txn, id, data.Permission{}); err != nil {
			return err
		}

		// Delete the permission from all roles
		if err := b.db.TxForEach(txn, badgerhold.Where("PermissionIDs").Contains(id), func(role *data.Role) error {
			role.PermissionIDs = slices.DeleteFunc(role.PermissionIDs, func(cmp string) bool {
				return cmp == id
			})

			if err := b.db.TxUpdate(txn, role.ID, role); err != nil {
				return err
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to delete permission from roles; %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (b *Badger) getPermissionIDs(txn *badger.Txn, method, path string) ([]string, error) {
	var permissionIDs []string

	var query *badgerhold.Query
	if method != "" {
		query = badgerhold.Where("Resources").MatchFunc(matchRequestMethod(method))
	}

	if path != "" {
		if query != nil {
			query = query.And("Resources").MatchFunc(matchRequestPath(path))
		} else {
			query = badgerhold.Where("Resources").MatchFunc(matchRequestPath(path))
		}
	}

	if err := b.db.TxForEach(txn, query, func(perm *data.Permission) error {
		permissionIDs = append(permissionIDs, perm.ID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to get permission IDs; %w", err)
	}

	return permissionIDs, nil
}
