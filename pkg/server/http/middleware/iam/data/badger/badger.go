package badger

import (
	"strings"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

var (
	DefaultCacheSize int64 = 100 << 20 // 100 MB
	DefaultLogSize   int64 = 200 << 20 // 200 MB
)

type Badger struct {
	db *badgerhold.Store

	dbBackupLock sync.RWMutex
}

var _ data.Database = &Badger{}

func New(path string, memory bool, flatten bool) (*Badger, error) {
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

	db, err := badgerhold.Open(options)
	if err != nil {
		return nil, err
	}

	if flatten {
		db.Badger().Flatten(20)
	}

	return &Badger{db: db}, nil
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

func matchAll(value string) badgerhold.MatchFunc {
	return func(ra *badgerhold.RecordAccess) (bool, error) {
		record, _ := ra.Field().(string)

		if strings.Contains(strings.ToLower(record), strings.ToLower(value)) {
			return true, nil
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

		if record == nil {
			return false, nil
		}

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

		if record == nil {
			return false, nil
		}

		for _, r := range record {
			if checkPath(r.Path, value) {
				return true, nil
			}
		}

		return false, nil
	}
}
