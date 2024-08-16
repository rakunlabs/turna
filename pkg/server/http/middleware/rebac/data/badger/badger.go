package badger

import (
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

type Badger struct {
	db *badgerhold.Store
}

var _ data.Database = &Badger{}

func New(path string) (*Badger, error) {
	options := badgerhold.DefaultOptions
	options.Dir = path
	options.ValueDir = path

	db, err := badgerhold.Open(options)
	if err != nil {
		return nil, err
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
