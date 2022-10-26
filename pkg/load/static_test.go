package load

import (
	"reflect"
	"testing"

	"github.com/worldline-go/turna/pkg/load/static"
)

func TestStatic_GetLoaders(t *testing.T) {
	type fields struct {
		Consul *static.Consul
		Vault  *static.Vault
		File   *static.File
	}
	tests := []struct {
		name   string
		fields fields
		want   func(fields) []Loader
	}{
		{
			name: "empty",
			want: func(fields) []Loader {
				return []Loader{}
			},
		},
		{
			name: "order default",
			fields: fields{
				Consul: &static.Consul{},
				Vault:  &static.Vault{},
				File:   &static.File{},
			},
			want: func(f fields) []Loader {
				return []Loader{
					f.Consul,
					f.Vault,
					f.File,
				}
			},
		},
		{
			name: "order mix",
			fields: fields{
				Consul: &static.Consul{
					Order: 5,
				},
				Vault: &static.Vault{
					Order: 1,
				},
				File: &static.File{
					Order: -1,
				},
			},
			want: func(f fields) []Loader {
				return []Loader{
					f.File,
					f.Vault,
					f.Consul,
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Static{
				Consul: tt.fields.Consul,
				Vault:  tt.fields.Vault,
				File:   tt.fields.File,
			}

			want := tt.want(tt.fields)

			if got := s.GetLoaders(); !reflect.DeepEqual(got, want) {
				t.Errorf("Static.GetLoaders() = %v, want %v", got, want)
			}
		})
	}
}
