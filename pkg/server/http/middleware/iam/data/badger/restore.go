package badger

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	badgerhold "github.com/timshannon/badgerhold/v4"
)

func restore(backupPath string) (*os.File, error) {
	if backupPath == "" {
		return nil, nil
	}

	// Open the backup file
	backup, err := os.Open(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("backup file not found, skipping restore", slog.String("backup", backupPath))
			return nil, nil
		}

		return nil, err
	}

	return backup, nil
}

func moveOld(backupPath, dir string) error {
	// create a new directory with the old name
	// move the old files to the new directory
	folderName := fmt.Sprintf("old-%s", time.Now().Format("2006-01-02T150405"))
	// check folder exists than add a number
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, folderName)); os.IsNotExist(err) {
			break
		}

		folderName += fmt.Sprintf("-%d", i)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	createdFolder := false

	backupPath = filepath.Clean(backupPath)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Join(dir, file.Name()) == backupPath {
			continue
		}

		if !createdFolder {
			if err := os.MkdirAll(filepath.Join(dir, folderName), 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", folderName, err)
			}
		}

		if err := os.Rename(filepath.Join(dir, file.Name()), filepath.Join(dir, folderName, file.Name())); err != nil {
			return fmt.Errorf("failed to move old file %s: %w", file.Name(), err)
		}
	}

	return nil
}

func OpenAndRestore(backupPath string, options badgerhold.Options) (*badgerhold.Store, error) {
	backup, err := restore(backupPath)
	if err != nil {
		return nil, err
	}

	if backup != nil {
		if !options.InMemory {
			// move old other files
			if err := moveOld(backupPath, options.Dir); err != nil {
				return nil, fmt.Errorf("failed to move old files: %w", err)
			}
		}

		defer backup.Close()
	}

	db, err := badgerhold.Open(options)
	if err != nil {
		return nil, err
	}

	if backup != nil {
		slog.Info("restoring database from backup", slog.String("backup", backupPath))
		if err := db.Badger().Load(backup, maxPendingWrites); err != nil {
			return nil, err
		}

		// remove the backup file
		backup.Close()
		slog.Info("restored database, cleaning backup file")
		if err := os.Remove(backupPath); err != nil {
			return nil, fmt.Errorf("failed to remove backup file: %w", err)
		}
	}

	return db, nil
}
