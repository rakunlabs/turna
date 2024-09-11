package badger

import (
	"errors"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetLMaps(req data.GetLMapRequest) (*data.Response[[]data.LMap], error) {
	var lmaps []data.LMap

	badgerHoldQuery := &badgerhold.Query{}
	badgerHoldQueryLimited := &badgerhold.Query{}

	if req.Name != "" {
		badgerHoldQuery = badgerhold.Where("Name").Eq(req.Name)
		badgerHoldQueryLimited = badgerHoldQuery
	}

	if req.Offset > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQueryLimited = badgerHoldQueryLimited.Limit(int(req.Limit))
	}

	if err := b.db.Find(&lmaps, badgerHoldQueryLimited); err != nil {
		return nil, err
	}

	count, err := b.db.Count(data.LMap{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	return &data.Response[[]data.LMap]{
		Meta: data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: lmaps,
	}, nil
}

func (b *Badger) GetLMap(name string) (*data.LMap, error) {
	var lmap data.LMap

	if err := b.db.Get(name, &lmap); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
		}
		return nil, err
	}

	return &lmap, nil
}

func (b *Badger) CreateLMap(lmap data.LMap) error {
	if err := b.db.Insert(lmap.Name, lmap); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("lmap with name %s already exists; %w", lmap.Name, data.ErrConflict)
		}
	}

	return nil
}

func (b *Badger) PutLMap(lmap data.LMap) error {
	if err := b.db.Update(lmap.Name, lmap); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("lmap with name %s not found; %w", lmap.Name, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) DeleteLMap(name string) error {
	if err := b.db.Delete(name, data.LMap{}); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
		}

		return err
	}

	return nil
}
