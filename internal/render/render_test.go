package render

import (
	"testing"
)

func TestRender_Execute(t *testing.T) {
	type fields struct {
		Data map[string]interface{}
	}
	type args struct {
		content any
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
				content: "{{ .test }}",
			},
			want: "1234",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			r.Data = tt.fields.Data

			got, err := r.Execute(tt.args.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Render.Execute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRender_IsTemplateExist(t *testing.T) {
	if got := GlobalRender.IsTemplateExist(); got != true {
		t.Errorf("Render.IsTemplateExist() = %v, want %v", got, true)
	}
}
