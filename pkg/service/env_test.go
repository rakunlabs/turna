package service

import (
	"os"
	"testing"

	"github.com/rakunlabs/turna/pkg/render"
)

func TestGetEnv(t *testing.T) {
	type args struct {
		predefined map[string]interface{}
		environ    bool
		envPaths   []string
	}
	tests := []struct {
		name     string
		args     args
		osEnv    func()
		exported map[string]interface{}
		want     []string
		wantErr  bool
	}{
		{
			name: "test",
			args: args{
				predefined: map[string]interface{}{
					"test": "test",
				},
				environ: false,
			},
			want: []string{"test=test"},
		},
		{
			name: "with env",
			args: args{
				predefined: map[string]interface{}{
					"PATH": "x",
				},
				environ: true,
			},
			osEnv: func() {
				os.Setenv("PATH", "y")
			},
			want: []string{"PATH=x"},
		},
		{
			name: "mix with env",
			args: args{
				predefined: map[string]interface{}{
					"PATH": "x",
					"ABC":  "1234",
				},
				environ: true,
			},
			osEnv: func() {
				os.Setenv("PATH", "y")
				os.Setenv("TR", "31")
			},
			want: []string{"PATH=x", "TR=31", "ABC=1234"},
		},
		{
			name: "mix with env and env_paths",
			args: args{
				predefined: map[string]interface{}{
					"PATH": "x",
					"ABC":  "1234",
				},
				environ: true,
				envPaths: []string{
					"test/env",
				},
			},
			osEnv: func() {
				os.Setenv("PATH", "y")
				os.Setenv("TR", "31")
			},
			exported: map[string]interface{}{
				"test": map[string]interface{}{
					"env": map[string]interface{}{
						"Name": "Eray",
					},
				},
			},
			want: []string{"PATH=x", "TR=31", "ABC=1234", "Name=Eray"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			if tt.osEnv != nil {
				tt.osEnv()
			}

			render.Data = tt.exported

			s := Service{}

			got, err := s.GetEnv(tt.args.predefined, tt.args.environ, tt.args.envPaths)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// slice to map
			gotMap := make(map[string]struct{})
			for _, v := range got {
				gotMap[v] = struct{}{}
			}

			// check existence
			for _, v := range tt.want {
				if _, ok := gotMap[v]; !ok {
					t.Errorf("GetEnv() got = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}
