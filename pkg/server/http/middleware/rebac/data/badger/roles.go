package badger

import (
	"errors"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.Role], error) {
	var roles []data.Role

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

	if err := b.db.Find(&roles, badgerHoldQueryLimited); err != nil {
		return nil, err
	}

	count, err := b.db.Count(data.Role{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	return &data.Response[[]data.Role]{
		Meta: data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: roles,
	}, nil
}

func (b *Badger) GetRole(id string) (*data.Role, error) {
	var role data.Role

	if err := b.db.Get(id, &role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	return &role, nil
}

func (b *Badger) CreateRole(role data.Role) error {
	if err := b.db.Insert(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("role with name %s already exists; %w", role.Name, data.ErrConflict)
		}

		return err
	}

	return nil
}

func (b *Badger) PutRole(role data.Role) error {
	if err := b.db.Update(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("role with id %s not found; %w", role.ID, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) DeleteRole(id string) error {
	if err := b.db.Delete(id, data.Role{}); err != nil {
		return err
	}

	return nil
}
