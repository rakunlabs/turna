package filter

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestFileFilter_Start(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		write   func(*os.File)
		res     [][]byte
		filter  func([]byte) bool
	}{
		{
			name: "stdout test",
			write: func(f *os.File) {
				fmt.Fprintln(f, "hellooo")
				fmt.Fprintln(f, "hellooo2")
			},
			res: [][]byte{
				[]byte("hellooo\n"),
				[]byte("hellooo2\n"),
			},
		},
		{
			name: "stdout test with filter",
			write: func(f *os.File) {
				fmt.Fprintln(f, "hellooo")
				fmt.Fprintln(f, "hellooo2")
				fmt.Fprintln(f, "hellooo3")
			},
			res: [][]byte{
				[]byte("hellooo\n"),
				[]byte("hellooo3\n"),
			},
			filter: func(b []byte) bool {
				return !bytes.Contains(b, []byte("hellooo2"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Error(err)
				return
			}

			defer func() {
				r.Close()
				w.Close()
			}()

			f := &FileFilter{
				To:     w,
				Filter: tt.filter,
			}

			wg := new(sync.WaitGroup)
			ctx, ctxCancel := context.WithCancel(context.Background())
			defer ctxCancel()

			got, err := f.Start(ctx, wg)
			if (err != nil) != tt.wantErr {
				t.Errorf("FileFilter.Start() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// fill data
			tt.write(got)

			// checking reader
			buff := bufio.NewReader(r)

			for count := 0; count < len(tt.res); count++ {
				readed, err := buff.ReadBytes('\n')
				if err != nil {
					t.Error(err)
					break
				}

				if !bytes.Equal(readed, tt.res[count]) {
					t.Errorf("compore result = %s, want %s", readed, tt.res[count])
					break
				}
			}
		})
	}
}
