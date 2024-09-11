package dnspath

import (
	"net"
	"testing"
)

func TestIPHolder_Set(t *testing.T) {
	type fields struct {
		ip map[int]string
	}
	type args struct {
		ips []net.IP
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[int]string
	}{
		{
			name: "Test total new 3 ips",
			fields: fields{
				ip: map[int]string{},
			},
			args: args{
				ips: []net.IP{
					net.ParseIP("10.0.0.1"),
					net.ParseIP("10.0.0.2"),
					net.ParseIP("10.0.0.3"),
				},
			},
			want: map[int]string{
				1: "10.0.0.1",
				2: "10.0.0.2",
				3: "10.0.0.3",
			},
		},
		{
			name: "Test exist 1 ip",
			fields: fields{
				ip: map[int]string{
					1: "10.0.0.1",
				},
			},
			args: args{
				ips: []net.IP{
					net.ParseIP("10.0.0.1"),
					net.ParseIP("10.0.0.2"),
					net.ParseIP("10.0.0.3"),
				},
			},
			want: map[int]string{
				1: "10.0.0.1",
				2: "10.0.0.2",
				3: "10.0.0.3",
			},
		},
		{
			name: "Test mix ips",
			fields: fields{
				ip: map[int]string{
					1: "10.0.0.5",
					2: "10.0.0.10",
					3: "10.0.0.15",
					4: "10.0.0.16",
					9: "10.0.0.17",
				},
			},
			args: args{
				ips: []net.IP{
					net.ParseIP("10.0.0.1"),
					net.ParseIP("10.0.0.2"),
					net.ParseIP("10.0.0.3"),
				},
			},
			want: map[int]string{
				1: "10.0.0.1",
				2: "10.0.0.2",
				3: "10.0.0.3",
			},
		},
		{
			name: "Test existing ips",
			fields: fields{
				ip: map[int]string{
					1: "10.0.0.5",
					2: "10.0.0.10",
					3: "10.0.0.15",
					4: "10.0.0.16",
					5: "10.0.0.17",
					6: "10.0.0.18",
				},
			},
			args: args{
				ips: []net.IP{
					net.ParseIP("10.0.0.17"),
					net.ParseIP("10.0.0.2"),
					net.ParseIP("10.0.0.3"),
					net.ParseIP("10.0.0.18"),
				},
			},
			want: map[int]string{
				1: "10.0.0.2",
				2: "10.0.0.3",
				3: "10.0.0.17",
				4: "10.0.0.18",
			},
		},
		{
			name: "Empty ip",
			fields: fields{
				ip: map[int]string{
					1: "10.0.0.5",
					2: "10.0.0.10",
					3: "10.0.0.15",
					4: "10.0.0.16",
					5: "10.0.0.17",
					6: "10.0.0.18",
				},
			},
			args: args{
				ips: []net.IP{},
			},
			want: map[int]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &IPHolder{
				ip: tt.fields.ip,
			}
			h.Set(tt.args.ips)

			if len(h.ip) != len(tt.want) {
				t.Errorf("IPHolder.Set() = %v, want %v", h.ip, tt.want)
			}

			for number, ip := range h.ip {
				if tt.want[number] != ip {
					t.Errorf("IPHolder.Set() = %v, want %v", h.ip, tt.want)
				}
			}
		})
	}
}
