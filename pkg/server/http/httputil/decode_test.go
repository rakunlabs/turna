package httputil

import (
	"bytes"
	"io"
	"testing"
)

func TestDecodeForm(t *testing.T) {
	type args struct {
		r io.Reader
		v interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Test DecodeForm",
			args: args{
				r: bytes.NewReader([]byte("username=test&password=test")),
				v: &struct {
					Username string `form:"username"`
					Password string `form:"password"`
				}{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DecodeForm(tt.args.r, tt.args.v); (err != nil) != tt.wantErr {
				t.Errorf("DecodeForm() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
