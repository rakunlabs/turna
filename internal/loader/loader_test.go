package loader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rakunlabs/chu/utils/decoder"
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
								ConfigCommon: ConfigCommon{Raw: true},
								Content:      "test",
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
								ConfigCommon: ConfigCommon{InnerPath: "test"},
								Content:      `{"test": {"test-2": "mycontent"}}`,
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
								ConfigCommon: ConfigCommon{InnerPath: "test/test-2"},
								Content:      `{"test": {"test-2": "mycontent"}}`,
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
								ConfigCommon: ConfigCommon{InnerPath: "test/test-2", Base64: true},
								Content:      `{"test": {"test-2": "bWVyaGFiYQ=="}}`,
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
								ConfigCommon: ConfigCommon{InnerPath: "server"},
								URL:          srv.URL + "/json",
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
								ConfigCommon: ConfigCommon{InnerPath: "server/address"},
								URL:          srv.URL + "/yaml",
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

func TestConfigs_Decode(t *testing.T) {
	in := map[string]any{
		"loads": []any{
			map[string]any{
				"name": "app",
				"statics": []any{
					map[string]any{"content": map[string]any{"name": "c", "raw": true, "content": "x"}},
					map[string]any{"gcp_secret": map[string]any{"name": "g", "secret": "app", "project_id": "p", "inner_path": "db"}},
					map[string]any{"aws_ssm": map[string]any{"parameter": "app/config", "codec": "JSON"}},
				},
			},
		},
	}

	var got struct {
		Loads Configs `cfg:"loads"`
	}

	if err := decoder.New().Decode(in, &got); err != nil {
		t.Fatalf("decode error = %v", err)
	}

	s := got.Loads[0].Statics
	if s[0].Content.Name != "c" || !s[0].Content.Raw || s[0].Content.Content != "x" {
		t.Errorf("content = %+v", s[0].Content)
	}

	if s[1].GCPSecret.Name != "g" || s[1].GCPSecret.InnerPath != "db" || s[1].GCPSecret.ProjectID != "p" {
		t.Errorf("gcp_secret = %+v", s[1].GCPSecret)
	}

	if s[2].AWSSSM.Codec != "JSON" || s[2].AWSSSM.Parameter != "app/config" {
		t.Errorf("aws_ssm = %+v", s[2].AWSSSM)
	}
}

func TestEnvFallback(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "env-project")
	t.Setenv("GCP_SECRET_LOCATION", "")
	t.Setenv("GCP_SECRET_VERSION", "")

	project, location, version, err := gcpTarget("", "", "", "GCP_SECRET_LOCATION", "GCP_SECRET_VERSION")
	if err != nil {
		t.Fatal(err)
	}

	if project != "env-project" || location != "global" || version != "latest" {
		t.Errorf("got %s %s %s", project, location, version)
	}

	project, _, _, _ = gcpTarget("cfg-project", "", "", "GCP_SECRET_LOCATION", "GCP_SECRET_VERSION")
	if project != "cfg-project" {
		t.Errorf("config value must override env, got %s", project)
	}

	t.Setenv("GCP_PROJECT_ID", "")
	if _, _, _, err := gcpTarget("", "", "", "GCP_SECRET_LOCATION", "GCP_SECRET_VERSION"); err == nil {
		t.Error("expected error without project id")
	}

	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")
	if r, _ := awsRegion(""); r != "eu-west-1" {
		t.Errorf("aws region = %s", r)
	}
}

func TestConfigs_LoadFile(t *testing.T) {
	tempDir := t.TempDir()

	if err := os.WriteFile(tempDir+"/in.json", []byte(`{"a": {"b": "c"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := Configs{{
		Export: tempDir + "/out.yaml",
		Statics: []ConfigStatic{{
			File: &ConfigFile{ConfigCommon: ConfigCommon{InnerPath: "a"}, Path: tempDir + "/in.json"},
		}},
	}}

	if err := c.Load(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}

	v, _ := os.ReadFile(tempDir + "/out.yaml")
	if string(v) != "b: c\n" {
		t.Errorf("got %q", v)
	}
}

func TestConfigs_LoadVault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		if r.URL.Path != "/v1/secret/data/app" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"data": {"db": {"password": "cGFzcw=="}}, "metadata": {}}}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_ROLE_ID", "")
	t.Setenv("VAULT_SECRET_BASE_PATH", "secret")

	tempDir := t.TempDir()

	c := Configs{{
		Export: tempDir + "/vault",
		Statics: []ConfigStatic{{
			Vault: &ConfigVault{
				ConfigCommon: ConfigCommon{InnerPath: "db/password", Base64: true},
				Path:         "app",
			},
		}},
	}}

	if err := c.Load(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}

	v, _ := os.ReadFile(tempDir + "/vault")
	if string(v) != "pass" {
		t.Errorf("got %q", v)
	}
}
