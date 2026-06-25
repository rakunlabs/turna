package loader

import (
	"context"
	"os"
	"testing"
)

func TestConfigs_Load(t *testing.T) {
	tempDir := t.TempDir()

	type want struct {
		content string
		path    string
	}

	tests := []struct {
		name    string
		c       Configs
		wantErr bool
		want    want
	}{
		{
			name: "content raw",
			c: Configs{
				{
					Export: tempDir + "/test",
					Statics: []ConfigStatic{
						{
							Content: &ConfigContent{
								Content: "test",
								Raw:     true,
							},
						},
					},
				},
			},
			want: want{
				content: "test",
				path:    tempDir + "/test",
			},
		},
		{
			name: "inner path json",
			c: Configs{
				{
					Export: tempDir + "/test.json",
					Statics: []ConfigStatic{
						{
							Content: &ConfigContent{
								Content:   `{"test": {"test-2": "mycontent"}}`,
								InnerPath: "test",
							},
						},
					},
				},
			},
			want: want{
				content: "{\n  \"test-2\": \"mycontent\"\n}\n",
				path:    tempDir + "/test.json",
			},
		},
		{
			name: "inner path raw",
			c: Configs{
				{
					Export: tempDir + "/test_inner_raw",
					Statics: []ConfigStatic{
						{
							Content: &ConfigContent{
								Content:   `{"test": {"test-2": "mycontent"}}`,
								InnerPath: "test/test-2",
							},
						},
					},
				},
			},
			want: want{
				content: "mycontent",
				path:    tempDir + "/test_inner_raw",
			},
		},
		{
			name: "inner path raw with base64",
			c: Configs{
				{
					Export: tempDir + "/test_b64",
					Statics: []ConfigStatic{
						{
							Content: &ConfigContent{
								Content:   `{"test": {"test-2": "bWVyaGFiYQ=="}}`,
								InnerPath: "test/test-2",
								Base64:    true,
							},
						},
					},
				},
			},
			want: want{
				content: "merhaba",
				path:    tempDir + "/test_b64",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Load(context.Background(), nil, nil); (err != nil) != tt.wantErr {
				t.Fatalf("Configs.Load() error = %v, wantErr %v", err, tt.wantErr)
			}

			v, err := os.ReadFile(tt.want.path)
			if err != nil {
				t.Fatalf("Configs.Load() read export error = %v", err)
			}

			if string(v) != tt.want.content {
				t.Errorf("Configs.Load() content = \n%q\n, want \n%q\n", string(v), tt.want.content)
			}
		})
	}
}
