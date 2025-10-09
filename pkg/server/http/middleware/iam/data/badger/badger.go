package badger

import (
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/spf13/cast"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

var (
	DefaultCacheSize int64 = 100 << 20 // 100 MB
	DefaultLogSize   int64 = 100 << 20 // 100 MB
)

type Badger struct {
	check data.CheckConfig
	db    *badgerhold.Store

	dbBackupLock sync.RWMutex
}

func New(path, backupPath string, memory, flatten bool, check data.CheckConfig) (*Badger, error) {
	options := badgerhold.DefaultOptions
	if memory {
		options.InMemory = memory
	} else {
		options.Dir = path
		options.ValueDir = path
	}

	options.IndexCacheSize = DefaultCacheSize
	options.Logger = NewLogger()
	options.ValueLogFileSize = DefaultLogSize

	db, err := OpenAndRestore(backupPath, options)
	if err != nil {
		return nil, err
	}

	if flatten {
		db.Badger().Flatten(20)
	}

	return &Badger{
		check: check,
		db:    db,
	}, nil
}

func (b *Badger) Close() error {
	if b.db == nil {
		return nil
	}

	return b.db.Close()
}

func toInterfaceSlice(slice []string) []any {
	interfaceSlice := make([]any, len(slice))
	for i, v := range slice {
		interfaceSlice[i] = v
	}

	return interfaceSlice
}

func toInterfaceSliceMap(slice map[string]struct{}) []any {
	interfaceSlice := make([]any, 0, len(slice))
	for k := range slice {
		interfaceSlice = append(interfaceSlice, k)
	}

	return interfaceSlice
}

func matchAll(values ...string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().(string)

		for _, v := range values {
			if strings.Contains(strings.ToLower(record), strings.ToLower(v)) {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchAnyIDs() badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]string)

		return len(record) > 0, nil
	}
}

func matchAnyTmpIDWithCheck() badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.TmpID)
		now := time.Now()

		for _, r := range record {
			// only check expiresAt
			if now.Before(r.ExpiresAt.Time) {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchTmpIDWithCheck(ids ...string) badgerhold.MatchFunc {
	now := time.Now()

	mappedIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		mappedIDs[id] = struct{}{}
	}

	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.TmpID)

		for _, r := range record {
			if _, ok := mappedIDs[r.ID]; ok {
				// only check expiresAt
				if now.Before(r.ExpiresAt.Time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func matchMixIDWithCheck(ids ...string) badgerhold.MatchFunc {
	now := time.Now()

	mappedIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		mappedIDs[id] = struct{}{}
	}

	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.MixID)

		for _, r := range record {
			if _, ok := mappedIDs[r.ID]; ok {
				if r.IsTmp {
					// this is general check startssAt and expiresAt
					if now.Before(r.ExpiresAt.Time) && now.After(r.StartsAt.Time) {
						return true, nil
					}
				} else {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func matchIDs(ids ...string) badgerhold.MatchFunc {
	mappedIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		mappedIDs[id] = struct{}{}
	}

	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]string)

		for _, r := range record {
			if _, ok := mappedIDs[r]; ok {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchMixID(ids ...string) badgerhold.MatchFunc {
	mappedIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		mappedIDs[id] = struct{}{}
	}

	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.MixID)

		for _, r := range record {
			if _, ok := mappedIDs[r.ID]; ok {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchAllField(field string, value string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().(map[string]any)

		if record == nil {
			return false, nil
		}

		if v, ok := record[field].(string); ok {
			if strings.Contains(strings.ToLower(v), strings.ToLower(value)) {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchRequestMethod(value string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.Resource)

		for _, r := range record {
			if checkMethod(r.Methods, value) {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchRequestPath(value string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().([]data.Resource)

		for _, r := range record {
			if checkPath(r.Path, value) {
				return true, nil
			}
		}

		return false, nil
	}
}

func matchData(value map[string]string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().(map[string]any)

		for k, v := range value {
			if vv, ok := record[k]; ok {
				if strings.Contains(strings.ToLower(cast.ToString(vv)), strings.ToLower(v)) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func slicesUnique(ss ...[]string) []string {
	seen := make(map[string]struct{})
	for _, v := range ss {
		for _, vv := range v {
			seen[vv] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}

	return result
}

func validIDs(ids []data.TmpID) []string {
	valid := make([]string, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) && now.After(id.StartsAt.Time) {
			valid = append(valid, id.ID)
		}
	}

	return valid
}

func validIDsWithTmpID(ids []data.TmpID) []data.TmpID {
	valid := make([]data.TmpID, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) && now.After(id.StartsAt.Time) {
			valid = append(valid, id)
		}
	}

	return valid
}

func validTmpIDs(ids []data.TmpID) []data.TmpID {
	valid := make([]data.TmpID, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		if now.Before(id.ExpiresAt.Time) {
			valid = append(valid, id)
		}
	}

	return valid
}

func (b *Badger) Update(fn func(txn *badger.Txn) error) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(fn)
}

// ///////////////////////////////////////////////////////////////////

func totalID(opts ...OptionTotalID) []data.MixID {
	o := &optionTotalID{}
	for _, opt := range opts {
		opt(o)
	}

	var total []data.MixID

	// elimitate duplicate
	seen := make(map[string]struct{})
	for _, v := range o.values {
		for _, vv := range v {
			if _, ok := seen[vv]; !ok {
				total = append(total, data.MixID{
					ID: vv,
				})
				seen[vv] = struct{}{}
			}
		}
	}

	for _, v := range o.tmps {
		for _, vv := range v {
			total = append(total, data.MixID{
				ID:        vv.ID,
				IsTmp:     true,
				StartsAt:  vv.StartsAt,
				ExpiresAt: vv.ExpiresAt,
			})
		}
	}

	return total
}

type optionTotalID struct {
	values [][]string
	tmps   [][]data.TmpID
}

type OptionTotalID func(*optionTotalID)

func WithTotalID(values ...[]string) OptionTotalID {
	return func(o *optionTotalID) {
		o.values = append(o.values, values...)
	}
}

func WithTotalTmpID(tmps ...[]data.TmpID) OptionTotalID {
	return func(o *optionTotalID) {
		o.tmps = append(o.tmps, tmps...)
	}
}
