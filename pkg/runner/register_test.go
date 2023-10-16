package runner

import (
	"context"
	"sync"
	"testing"
)

func TestStoreReg_Run(t *testing.T) {
	type fields struct {
		commands []*Command
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "empty",
		},
		{
			name: "one command",
			fields: fields{
				commands: []*Command{
					{
						Name:    "echo",
						Command: []string{"echo", "hello"},
					},
				},
			},
		},
		{
			name: "two command",
			fields: fields{
				commands: []*Command{
					{
						Name:    "echo",
						Command: []string{"echo", "hello"},
					},
					{
						Name:    "echo2",
						Command: []string{"echo", "hello2"},
					},
				},
			},
		},
		{
			name: "two order",
			fields: fields{
				commands: []*Command{
					{
						Name:    "echo",
						Command: []string{"echo", "hello"},
						Order:   1,
					},
					{
						Name:    "echo2",
						Command: []string{"echo", "hello2"},
						Order:   2,
					},
				},
			},
		},
		{
			name: "mix order",
			fields: fields{
				commands: []*Command{
					{
						Name:    "echo-1-1",
						Command: []string{"echo", "hello 1-1"},
						Order:   1,
					},
					{
						Name:         "echo-1-2",
						Command:      []string{"sh", "-c", "exit 1"},
						Order:        1,
						AllowFailure: true,
					},
					{
						Name:    "echo-2-1",
						Command: []string{"echo", "hello 2-1"},
						Order:   2,
					},
					{
						Name:    "echo-5-1",
						Command: []string{"echo", "hello 5-1"},
						Order:   5,
					},
					{
						Name:    "echo-x-2",
						Command: []string{"echo", "hello x-2"},
						Depends: []string{"echo-1-1"},
						Order:   10,
					},
				},
			},
		},
		{
			name:    "mix mistake",
			wantErr: true,
			fields: fields{
				commands: []*Command{
					{
						Name:    "echo-1-1",
						Command: []string{"echo", "hello 1-1"},
						Order:   1,
					},
					{
						Name:    "echo-1-2",
						Command: []string{"sh", "-c", "exit 1"},
						Order:   1,
					},
					{
						Name:    "echo-2-1",
						Command: []string{"echo", "hello 2-1"},
						Order:   2,
					},
					{
						Name:    "echo-5-1",
						Command: []string{"echo", "hello 5-1"},
						Order:   5,
					},
					{
						Name:    "echo-x-2",
						Command: []string{"echo", "hello x-2"},
						Depends: []string{"echo-1-1"},
						Order:   10,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wg := new(sync.WaitGroup)
			s := NewStoreReg(wg)

			for _, command := range tt.fields.commands {
				s.Add(command)
			}

			if tt.args.ctx == nil {
				tt.args.ctx = context.Background()
			}

			if err := s.Run(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("StoreReg.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
