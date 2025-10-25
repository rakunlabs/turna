package url

import (
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestMatchDoubleStart(t *testing.T) {
	type args struct {
		pattern string
		path    string
	}
	tests := []struct {
		name    string
		args    args
		wantOk  bool
		wantErr bool
	}{
		{
			name: "simple match",
			args: args{
				pattern: "/api/**",
				path:    "/api/v1/users",
			},
			wantOk: true,
		},
		{
			name: "simple match host",
			args: args{
				pattern: "*.test.com",
				path:    "xx.api.test.com",
			},
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := doublestar.Match(tt.args.pattern, tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("doublestar.Match() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if ok != tt.wantOk {
				t.Errorf("doublestar.Match() = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}
