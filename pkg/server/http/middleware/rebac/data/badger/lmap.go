package badger

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetLMaps(req data.GetLMapRequest) (*data.Response[[]data.LMap], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var lmaps []data.LMap

	badgerHoldQuery := &badgerhold.Query{}

	if req.Name != "" {
		badgerHoldQuery = badgerhold.Where("Name").Eq(req.Name).Index("Name")
	} else if len(req.RoleIDs) > 0 {
		badgerHoldQuery = badgerhold.Where("RoleIDs").ContainsAny(toInterfaceSlice(req.RoleIDs)...)
	}

	count, err := b.db.Count(data.LMap{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	if req.Offset > 0 {
		badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
	}

	if err := b.db.Find(&lmaps, badgerHoldQuery); err != nil {
		return nil, err
	}

	return &data.Response[[]data.LMap]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: lmaps,
	}, nil
}

func (b *Badger) GetLMap(name string) (*data.LMap, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

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
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Insert(lmap.Name, lmap); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("lmap with name %s already exists; %w", lmap.Name, data.ErrConflict)
		}
	}

	return nil
}

func (b *Badger) CheckCreateLMap(names []data.LMapCheckCreate) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	for _, name := range names {
		var lmap data.LMap
		if err := b.db.Get(name, &lmap); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				// create role
				role := data.Role{
					ID:          ulid.Make().String(),
					Name:        name.Name,
					Description: name.Description,
				}

				if err := b.db.Insert(role.ID, role); err != nil {
					if errors.Is(err, badgerhold.ErrKeyExists) {
						slog.Warn("role already exists", slog.String("name", role.Name), slog.String("error", err.Error()))
					} else {
						slog.Error("failed to create role", slog.String("error", err.Error()))
					}
				}

				// create lmap
				lmap := data.LMap{
					Name:    name.Name,
					RoleIDs: []string{role.ID},
				}

				if err := b.db.Insert(lmap.Name, lmap); err != nil {
					if errors.Is(err, badgerhold.ErrKeyExists) {
						slog.Warn("lmap already exists", slog.String("name", lmap.Name), slog.String("error", err.Error()))
					} else {
						slog.Error("failed to create lmap", slog.String("error", err.Error()))
					}
				}
			}
		} else {
			slog.Warn("failed to get lmap", slog.String("name", name.Name), slog.String("error", err.Error()))
		}
	}
}

func (b *Badger) PutLMap(lmap data.LMap) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Update(lmap.Name, lmap); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("lmap with name %s not found; %w", lmap.Name, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) DeleteLMap(name string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Delete(name, data.LMap{}); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) LMapRoleIDs() data.LMapRoleIDs {
	return NewLMapCacheRoleIDs(b)
}

type LMapCacheRoleIDs struct {
	b *Badger

	cache map[string][]string
}

func NewLMapCacheRoleIDs(b *Badger) *LMapCacheRoleIDs {
	return &LMapCacheRoleIDs{
		b:     b,
		cache: make(map[string][]string),
	}
}

func (l *LMapCacheRoleIDs) Get(names []string) ([]string, error) {
	l.b.dbBackupLock.RLock()
	defer l.b.dbBackupLock.RUnlock()

	mapRoleIDs := make(map[string]struct{})
	for _, name := range names {
		if roleIDs, ok := l.cache[name]; ok {
			for _, roleID := range roleIDs {
				mapRoleIDs[roleID] = struct{}{}
			}

			continue
		}

		var lmap data.LMap
		if err := l.b.db.Get(name, &lmap); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				slog.Warn("lmap not found", slog.String("name", name))

				l.cache[name] = []string{}

				continue
			}

			return nil, err
		}

		for _, roleID := range lmap.RoleIDs {
			mapRoleIDs[roleID] = struct{}{}
		}

		l.cache[name] = lmap.RoleIDs
	}

	roleIDSlice := make([]string, 0, len(mapRoleIDs))
	for roleID := range mapRoleIDs {
		roleIDSlice = append(roleIDSlice, roleID)
	}

	return roleIDSlice, nil
}
