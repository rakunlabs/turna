package badger

import (
	"io"
	"log/slog"
)

var maxPendingWrites = 20

func (b *Badger) Backup(w io.Writer, since uint64) error {
	b.dbBackupLock.Lock()
	defer b.dbBackupLock.Unlock()

	timestamp, err := b.db.Badger().Backup(w, since)
	if err != nil {
		return err
	}

	slog.Info("backup created", slog.Uint64("backup_time", timestamp))

	return nil
}

func (b *Badger) Restore(r io.Reader) error {
	b.dbBackupLock.Lock()
	defer b.dbBackupLock.Unlock()

	if err := b.db.Badger().Load(r, maxPendingWrites); err != nil {
		return err
	}

	slog.Info("restored database")

	return nil
}
