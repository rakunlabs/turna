package badger

import (
	"errors"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetPermissions(req data.GetPermissionRequest) (*data.Response[[]data.Permission], error) {
	var permissions []data.Permission

	badgerHoldQuery := &badgerhold.Query{}
	badgerHoldQueryLimited := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID)
		badgerHoldQueryLimited = badgerHoldQuery
	} else if req.Name != "" {
		badgerHoldQuery = badgerhold.Where("Name").Eq(req.Name)
		badgerHoldQueryLimited = badgerHoldQuery
	}

	if req.Offset > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Limit(int(req.Limit))
	}

	if err := b.db.Find(&permissions, badgerHoldQueryLimited); err != nil {
		return nil, err
	}

	count, err := b.db.Count(data.Permission{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	return &data.Response[[]data.Permission]{
		Meta: data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: permissions,
	}, nil
}

func (b *Badger) GetPermission(name string) (*data.Permission, error) {
	var permission data.Permission

	if err := b.db.Get(name, &permission); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("permission with name %s not found; %w", name, data.ErrNotFound)
		}

		return nil, err
	}

	return &permission, nil
}

func (b *Badger) CreatePermission(permission data.Permission) error {
	if err := b.db.Insert(permission.ID, permission); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("permission with name %s already exists; %w", permission.Name, data.ErrConflict)
		}
	}

	return nil
}

func (b *Badger) PutPermission(permission data.Permission) error {
	if err := b.db.Update(permission.Name, permission); err != nil {
		return err
	}

	return nil
}

func (b *Badger) DeletePermission(name string) error {
	if err := b.db.Delete(name, data.Permission{}); err != nil {
		return err
	}

	return nil
}
