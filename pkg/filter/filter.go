package filter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog/log"
)

// FileFilter filter *os.File with a defined filter function.
type FileFilter struct {
	To      *os.File
	started bool
	closed  bool
	lock    sync.Mutex
	r       *os.File
	w       *os.File
	ch      chan struct{}
	wg      sync.WaitGroup
	Filter  func([]byte) bool
}

// Start create a goroutine to listen->filter->redirect output.
// It is return *os.File to replace it with other *os.File types.
// Safe to call more than once to get filtered *os.File.
func (f *FileFilter) Start() (*os.File, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.started {
		return f.w, nil
	}

	f.started = true
	f.closed = false

	var err error

	f.r, f.w, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to start FileFilter %w", err)
	}

	f.ch = make(chan struct{})

	f.wg.Add(1)

	go func() {
		defer f.wg.Done()

		buff := bufio.NewReader(f.r)

		for loop := true; loop; {
			select {
			case <-f.ch:
				f.ch = nil

				f.w.Close()

				f.closed = true
			default:
				if err := f.read(buff); err != nil && !errors.Is(err, io.EOF) {
					log.Error().Err(err).Msg("loop read failed")
				}

				if f.closed {
					// read until EOF
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

					loop = false
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

// Close should call end of the filter to stop background listen goroutine.
// Safe for calling more than once.
func (f *FileFilter) Close() {
	// if closed channel is nil and throw panic
	// lock mecanism prevent close nil channel
	f.lock.Lock()
	defer f.lock.Unlock()

	if !f.closed {
		f.w.Close()
		close(f.ch)
		f.wg.Wait()
	}
}
