package runner

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
)

func TestCommand_Run(t *testing.T) {
	type fields struct {
		Name         string
		Path         string
		Env          []string
		Filter       func([]byte) bool
		Command      []string
		AllowFailure bool

		stdin  *os.File
		stdout *os.File
		stderr *os.File
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name       string
		fields     fields
		args       args
		wantErr    bool
		wantStdout []byte
		wantStderr []byte
		// stdin []byte
	}{
		{
			name: "echo",
			fields: fields{
				Name:         "echo",
				Command:      []string{"echo", "hello"},
				AllowFailure: false,
			},
			wantErr:    false,
			wantStdout: []byte(`hello` + "\n"),
		},
		{
			name: "printenv filter",
			fields: fields{
				Name:    "printenv",
				Command: []string{"printenv", "USER", "FOO"},
				Env:     []string{"USER=test", "FOO=bar"},
				Filter: func(b []byte) bool {
					return !bytes.Contains(b, []byte("bar"))
				},
				AllowFailure: false,
			},
			wantErr:    false,
			wantStdout: []byte(`test` + "\n"),
		},
		{
			name: "exit 1",
			fields: fields{
				Name:         "exit",
				Command:      []string{"sh", "-c", "exit 1"},
				AllowFailure: false,
			},
			wantErr: true,
		},
		{
			name: "exit 1 allow failure",
			fields: fields{
				Name:         "exit",
				Command:      []string{"sh", "-c", "exit 1"},
				AllowFailure: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdoutR, stdoutW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}

			stderrR, stderrW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}

			c := &Command{
				Name:         tt.fields.Name,
				Path:         tt.fields.Path,
				Env:          tt.fields.Env,
				Filter:       tt.fields.Filter,
				Command:      tt.fields.Command,
				AllowFailure: tt.fields.AllowFailure,
				stdout:       stdoutW,
				stderr:       stderrW,
			}
			if tt.args.ctx == nil {
				tt.args.ctx = context.Background()
			}
			if err := c.Run(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Fatalf("Command.Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			defer func() {
				stdoutR.Close()
				stderrR.Close()
			}()

			wg := sync.WaitGroup{}
			defer wg.Wait()

			defer func() {
				stdoutW.Close()
				stderrW.Close()
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()

				if tt.wantStdout != nil {
					got, err := io.ReadAll(stdoutR)
					if err != nil {
						t.Error(err)

						return
					}

					if !bytes.Equal(got, tt.wantStdout) {
						t.Errorf("Command.Run() stdout = %s, want %s", got, tt.wantStdout)
					}
				}

				if tt.wantStderr != nil {
					got, err := io.ReadAll(stderrR)
					if err != nil {
						t.Error(err)

						return
					}

					if !bytes.Equal(got, tt.wantStderr) {
						t.Errorf("Command.Run() stderr = %s, want %s", got, tt.wantStderr)
					}
				}
			}()
		})
	}
}
