package filter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// FileFilter filter *os.File with a defined filter function.
type FileFilter struct {
	To      io.Writer
	started bool
	lock    sync.Mutex
	r       *os.File
	w       *os.File
	Filter  func([]byte) bool
}

// Start create a goroutine to listen->filter->redirect output.
// It is return *os.File to replace it with other *os.File types.
// Safe to call more than once to get filtered *os.File.
func (f *FileFilter) Start(ctx context.Context, wg *sync.WaitGroup) (*os.File, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.started {
		return f.w, nil
	}

	f.started = true

	var err error

	f.r, f.w, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to start FileFilter %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { f.started = false }()
		defer cancel()

		buff := bufio.NewReader(f.r)

		ctxReadWait, ctxReadCancel := context.WithCancel(context.Background())

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()

			f.w.Close()

			var err error

			// read remainings
			<-ctxReadWait.Done()

			for {
				if err = f.read(buff, true); err != nil {
					break
				}
			}

			if !errors.Is(err, io.EOF) {
				slog.Error("loop read remainings failed", "err", err)
			}

			f.r.Close()
		}()

		for {
			if err := f.read(buff, false); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}

				if !errors.Is(err, os.ErrClosed) {
					slog.Error("loop read failed", "err", err)
				}

				break
			}
		}

		ctxReadCancel()
	}()

	return f.w, nil
}

func (f *FileFilter) read(buff *bufio.Reader, withErr bool) error {
	readed, err := buff.ReadBytes('\n')
	if err != nil {
		if !withErr {
			return err
		}

		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed read FileFilter: %w", err)
		}
	}

	if len(readed) == 0 {
		return err
	}

	// filter function
	if f.Filter != nil && !f.Filter(readed) {
		return nil
	}

	if _, err := f.To.Write(readed); err != nil {
		return fmt.Errorf("failed write FileFilter.To: %w", err)
	}

	return err
}
