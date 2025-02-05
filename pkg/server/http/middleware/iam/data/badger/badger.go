package badger

import (
	"strings"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/spf13/cast"
	badgerhold "github.com/timshannon/badgerhold/v4"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
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

func (b *Badger) Update(fn func(txn *badger.Txn) error) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(fn)
}
