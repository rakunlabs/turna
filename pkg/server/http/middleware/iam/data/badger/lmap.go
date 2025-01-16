package badger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/logi"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetLMaps(req data.GetLMapRequest) (*data.Response[[]data.LMap], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var lmaps []data.LMap
	var count uint64

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		badgerHoldQuery := &badgerhold.Query{}

		if req.Name != "" {
			badgerHoldQuery = badgerhold.Where("Name").Eq(req.Name).Index("Name")
		} else if len(req.RoleIDs) > 0 {
			badgerHoldQuery = badgerhold.Where("RoleIDs").ContainsAny(toInterfaceSlice(req.RoleIDs)...)
		}

		var err error
		count, err = b.db.TxCount(txn, data.LMap{}, badgerHoldQuery)
		if err != nil {
			return err
		}

		if req.Offset > 0 {
			badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
		}
		if req.Limit > 0 {
			badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
		}

		if err := b.db.TxFind(txn, &lmaps, badgerHoldQuery); err != nil {
			return err
		}

		return nil
	}); err != nil {
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

func (b *Badger) CreateLMap(ctx context.Context, lmap data.LMap) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	lmap.CreatedAt = time.Now().Format(time.RFC3339)
	lmap.UpdatedAt = lmap.CreatedAt
	lmap.UpdatedBy = data.CtxUserName(ctx)

	if err := b.db.Insert(lmap.Name, lmap); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("lmap with name %s already exists; %w", lmap.Name, data.ErrConflict)
		}
	}

	logi.Ctx(ctx).Info("lmap created", slog.String("name", lmap.Name), slog.String("by", data.CtxUserName(ctx)))

	return nil
}

func (b *Badger) TxCheckCreateLMap(ctx context.Context, txn *badger.Txn, lmapChecks []data.LMapCheckCreate) error {
	createdAt := time.Now().Format(time.RFC3339)
	userName := data.CtxUserName(ctx)

	for _, lmapCheck := range lmapChecks {
		if err := b.db.TxGet(txn, lmapCheck.Name, &data.LMap{}); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				// create role
				role := data.Role{
					ID:          ulid.Make().String(),
					Name:        lmapCheck.Name,
					Description: lmapCheck.Description,
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt,
					UpdatedBy:   userName,
				}

				if err := b.db.TxInsert(txn, role.ID, role); err != nil {
					if errors.Is(err, badgerhold.ErrKeyExists) {
						slog.Warn("role already exists", slog.String("name", role.Name), slog.String("error", err.Error()))
					} else {
						return fmt.Errorf("failed to create role; %w", err)
					}
				}

				logi.Ctx(ctx).Info("role created", slog.String("name", role.Name), slog.String("by", userName))

				// create lmap
				lmap := data.LMap{
					Name:      lmapCheck.Name,
					RoleIDs:   []string{role.ID},
					CreatedAt: createdAt,
					UpdatedAt: createdAt,
					UpdatedBy: userName,
				}

				if err := b.db.TxInsert(txn, lmap.Name, lmap); err != nil {
					if errors.Is(err, badgerhold.ErrKeyExists) {
						slog.Warn("lmap already exists", slog.String("name", lmap.Name), slog.String("error", err.Error()))
					} else {
						return fmt.Errorf("failed to create lmap; %w", err)
					}
				} else {
					logi.Ctx(ctx).Info("lmap created", slog.String("name", lmap.Name), slog.String("by", userName))
				}
			} else {
				return fmt.Errorf("failed to get lmap, %s; %w", lmapCheck.Name, err)
			}
		}
	}

	return nil
}

func (b *Badger) PutLMap(ctx context.Context, lmap data.LMap) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		var lmapOld data.LMap
		if err := b.db.TxGet(txn, lmap.Name, &lmapOld); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("lmap with name %s not found; %w", lmap.Name, data.ErrNotFound)
			}

			return err
		}

		lmap.CreatedAt = lmapOld.CreatedAt
		lmap.UpdatedAt = time.Now().Format(time.RFC3339)
		lmap.UpdatedBy = data.CtxUserName(ctx)

		if err := b.db.TxUpdate(txn, lmap.Name, lmap); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	logi.Ctx(ctx).Info("lmap replaced", slog.String("name", lmap.Name), slog.String("by", data.CtxUserName(ctx)))

	return nil
}

func (b *Badger) DeleteLMap(ctx context.Context, name string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Delete(name, data.LMap{}); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
		}

		return err
	}

	logi.Ctx(ctx).Info("lmap deleted", slog.String("name", name), slog.String("by", data.CtxUserName(ctx)))

	return nil
}

func (b *Badger) LMapRoleIDs() *LMapCacheRoleIDs {
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

func (l *LMapCacheRoleIDs) TxGet(txn *badger.Txn, names []string) ([]string, error) {
	mapRoleIDs := make(map[string]struct{})
	for _, name := range names {
		if roleIDs, ok := l.cache[name]; ok {
			for _, roleID := range roleIDs {
				mapRoleIDs[roleID] = struct{}{}
			}

			continue
		}

		var lmap data.LMap
		if err := l.b.db.TxGet(txn, name, &lmap); err != nil {
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
