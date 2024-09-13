package dnspath

import (
	"testing"
	"time"
)

func TestPath_IsFetched(t *testing.T) {
	type fields struct {
		Duration  time.Duration
		lastCheck time.Time
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "return false when lastCheck is zero",
			fields: fields{
				Duration:  10 * time.Second,
				lastCheck: time.Time{},
			},
			want: false,
		},
		{
			name: "return false when lastCheck is not zero but Duration is zero",
			fields: fields{
				Duration:  0,
				lastCheck: time.Now(),
			},
			want: false,
		},
		{
			name: "return false when lastCheck is not zero but Duration is not zero",
			fields: fields{
				Duration:  10 * time.Second,
				lastCheck: time.Now().Add(-11 * time.Second),
			},
			want: false,
		},
		{
			name: "return true when lastCheck is not zero and Duration is not zero",
			fields: fields{
				Duration:  10 * time.Second,
				lastCheck: time.Now().Add(-9 * time.Second),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Path{
				Duration:  tt.fields.Duration,
				lastCheck: tt.fields.lastCheck,
			}
			if got := p.IsFetched(); got != tt.want {
				t.Errorf("Path.IsFetched() = %v, want %v", got, tt.want)
			}
		})
	}
}
