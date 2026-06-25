package loader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
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

func TestConfigs_LoadHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"server": {"address": ":8080"}}`))
		case "/yaml":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("server:\n  address: \":9090\"\n"))
		case "/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version": "` + r.URL.Query().Get("version") + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tempDir := t.TempDir()

	tests := []struct {
		name    string
		c       Configs
		wantErr bool
		path    string
		content string
	}{
		{
			name: "json content-type auto codec",
			c: Configs{
				{
					Export: tempDir + "/http.json",
					Statics: []ConfigStatic{
						{
							HTTP: &ConfigHTTP{
								URL:       srv.URL + "/json",
								InnerPath: "server",
							},
						},
					},
				},
			},
			path:    tempDir + "/http.json",
			content: "{\n  \"address\": \":8080\"\n}\n",
		},
		{
			name: "yaml content-type inner raw",
			c: Configs{
				{
					Export: tempDir + "/http_raw",
					Statics: []ConfigStatic{
						{
							HTTP: &ConfigHTTP{
								URL:       srv.URL + "/yaml",
								InnerPath: "server/address",
							},
						},
					},
				},
			},
			path:    tempDir + "/http_raw",
			content: ":9090",
		},
		{
			name: "query and timeout",
			c: Configs{
				{
					Export: tempDir + "/http_query.json",
					Statics: []ConfigStatic{
						{
							HTTP: &ConfigHTTP{
								URL:     srv.URL + "/query",
								Query:   map[string]string{"version": "v2"},
								Timeout: 5 * time.Second,
							},
						},
					},
				},
			},
			path:    tempDir + "/http_query.json",
			content: "{\n  \"version\": \"v2\"\n}\n",
		},
		{
			name: "not found errors",
			c: Configs{
				{
					Statics: []ConfigStatic{
						{
							HTTP: &ConfigHTTP{URL: srv.URL + "/missing"},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Load(context.Background(), nil, nil); (err != nil) != tt.wantErr {
				t.Fatalf("Configs.Load() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			v, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("Configs.Load() read export error = %v", err)
			}

			if string(v) != tt.content {
				t.Errorf("Configs.Load() content = \n%q\n, want \n%q\n", string(v), tt.content)
			}
		})
	}
}
