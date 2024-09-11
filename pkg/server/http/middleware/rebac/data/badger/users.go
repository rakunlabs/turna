package badger

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetUsers(req data.GetUserRequest) (*data.Response[[]data.User], error) {
	var users []data.User

	badgerHoldQuery := &badgerhold.Query{}
	badgerHoldQueryLimited := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID)
		badgerHoldQueryLimited = badgerHoldQuery
	} else if req.Alias != "" {
		badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
		badgerHoldQueryLimited = badgerHoldQuery
	}

	if req.Offset > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Limit(int(req.Limit))
	}

	if err := b.db.Find(&users, badgerHoldQueryLimited); err != nil {
		return nil, err
	}

	count, err := b.db.Count(data.User{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	return &data.Response[[]data.User]{
		Meta: data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: users,
	}, nil
}

func (b *Badger) GetUser(req data.GetUserRequest) (*data.User, error) {
	var user data.User

	badgerHoldQuery := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID)
	} else if req.Alias != "" {
		badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
	}

	if err := b.db.FindOne(&user, badgerHoldQuery); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("user with id %s not found; %w", req.ID, data.ErrNotFound)
		}

		return nil, err
	}

	return &user, nil
}

func (b *Badger) CreateUser(user data.User) error {
	var foundUser data.User
	alias := make([]interface{}, len(user.Alias))
	for i, a := range user.Alias {
		alias[i] = a
	}

	if err := b.db.FindOne(&foundUser, badgerhold.Where("Alias").ContainsAny(alias...)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			return err
		}
	}

	if foundUser.ID != "" {
		return fmt.Errorf("user with alias %v already exists; %w", user.Alias, data.ErrConflict)
	}

	return b.db.Insert(user.ID, user)
}

func (b *Badger) PatchUser(user data.User) error {
	var foundUser data.User
	if err := b.db.FindOne(&foundUser, badgerhold.Where("ID").Eq(user.ID)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", user.ID, badgerhold.ErrNotFound)
		}

		return err
	}

	for _, alias := range user.Alias {
		if !slices.Contains(foundUser.Alias, alias) {
			foundUser.Alias = append(foundUser.Alias, alias)
		}
	}

	for _, role := range user.Roles {
		if !slices.Contains(foundUser.Roles, role) {
			foundUser.Roles = append(foundUser.Roles, role)
		}
	}

	for k, v := range user.Details {
		foundUser.Details[k] = v
	}

	if err := b.db.Update(user.ID, foundUser); err != nil {
		return err
	}

	return nil
}

func (b *Badger) PutUser(user data.User) error {
	var foundUser data.User
	if err := b.db.FindOne(&foundUser, badgerhold.Where("ID").Eq(user.ID)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", user.ID, badgerhold.ErrNotFound)
		}

		return err
	}

	if err := b.db.Update(user.ID, user); err != nil {
		return err
	}

	return nil
}

func (b *Badger) DeleteUser(id string) error {
	return b.db.Delete(id, data.User{})
}
