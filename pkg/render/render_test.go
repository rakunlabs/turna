package render

import (
	"testing"
)

func TestRender_Execute(t *testing.T) {
	type fields struct {
		Data map[string]interface{}
	}
	type args struct {
		content string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "test",
			fields: fields{
				Data: map[string]interface{}{
					"test": "test",
				},
			},
			args: args{
				content: "{{ .test }}",
			},
			want: "test",
		},
		{
			name: "test",
			fields: fields{
				Data: map[string]interface{}{
					"test": 1234,
				},
			},
			args: args{
				content: "{{ addf 1.1 .test }}",
			},
			want: "1235.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExecuteWithData(tt.args.content, tt.fields.Data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf("Render.Execute() = %v, want %v", string(got), tt.want)
			}
		})
	}
}
