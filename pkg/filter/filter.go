package filter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog/log"
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { f.started = false }()

		buff := bufio.NewReader(f.r)

		for {
			select {
			case <-ctx.Done():
				f.w.Close()

				var err error

				// read remainings
				for {
					if err = f.read(buff); err != nil {
						break
					}
				}

				if !errors.Is(err, io.EOF) {
					log.Error().Err(err).Msg("loop read remainings failed")
				}

				f.r.Close()

				return
			default:
				if err := f.read(buff); err != nil && !errors.Is(err, io.EOF) {
					log.Error().Err(err).Msg("loop read failed")
				}
			}
		}
	}()

	return f.w, nil
}

func (f *FileFilter) read(buff *bufio.Reader) error {
	readed, err := buff.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("failed read FileFilter: %w", err)
	}

	// filter function
	if f.Filter != nil {
		if !f.Filter(readed) {
			return nil
		}
	}

	if _, err := f.To.Write(readed); err != nil {
		return fmt.Errorf("failed write FileFilter.To: %w", err)
	}

	return nil
}
