package badger

import (
	"io"
	"log/slog"
)

var maxPendingWrites = 256

func (b *Badger) Backup(w io.Writer, since uint64) (uint64, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	b.db.Badger().MaxVersion()

	slog.Info("backup database", slog.Uint64("since", since))
	backupVersion, err := b.db.Badger().Backup(w, since)
	if err != nil {
		return backupVersion, err
	}

	return backupVersion, nil
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

func (b *Badger) Version() uint64 {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().MaxVersion()
}
