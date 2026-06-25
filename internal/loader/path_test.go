package loader

import (
	"reflect"
	"testing"
)

func TestInnerPath(t *testing.T) {
	type args struct {
		s string
		v map[string]interface{}
	}
	tests := []struct {
		name string
		args args
		want interface{}
	}{
		{
			name: "test",
			args: args{
				s: "test",
				v: map[string]interface{}{
					"test": "test",
				},
			},
			want: "test",
		},
		{
			name: "test with inner",
			args: args{
				s: "test/inner",
				v: map[string]interface{}{
					"test": map[string]interface{}{
						"inner": "test",
					},
				},
			},
			want: "test",
		},
		{
			name: "test with inner and nil",
			args: args{
				s: "test/inner",
				v: map[string]interface{}{
					"test": map[string]interface{}{
						"inner": nil,
					},
				},
			},
			want: nil,
		},
		{
			name: "unknown path",
			args: args{
				s: "test/inner/x",
				v: map[string]interface{}{
					"test": map[string]interface{}{
						"inner": nil,
					},
				},
			},
			want: nil,
		},
		{
			name: "empty path",
			args: args{
				s: "",
				v: map[string]interface{}{
					"test": map[string]interface{}{
						"inner": nil,
					},
				},
			},
			want: map[string]interface{}{
				"test": map[string]interface{}{
					"inner": nil,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InnerPath(tt.args.s, tt.args.v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InnerPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapPath(t *testing.T) {
	type args struct {
		s string
		v interface{}
	}
	tests := []struct {
		name string
		args args
		want interface{}
	}{
		{
			name: "test",
			args: args{
				s: "test",
				v: "test",
			},
			want: map[string]interface{}{
				"test": "test",
			},
		},
		{
			name: "test with inner",
			args: args{
				s: "test/inner",
				v: "test",
			},
			want: map[string]interface{}{
				"test": map[string]interface{}{
					"inner": "test",
				},
			},
		},
		{
			name: "empty path",
			args: args{
				s: "",
				v: "test",
			},
			want: "test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapPath(tt.args.s, tt.args.v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
