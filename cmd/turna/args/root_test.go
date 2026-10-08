package args

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rakunlabs/gofret"
	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/internal/loader"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gcptoken"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/providers"
	"github.com/rakunlabs/turna/pkg/service"
)

func TestConfigMapCodecOmitsSecrets(t *testing.T) {
	const secret = "secret-must-not-appear"
	app := config.Application
	app.Env = map[string]any{"TOKEN": secret}
	app.Services = service.Services{{Name: "app", Env: map[string]any{"PASSWORD": secret}}}
	app.Loads = loader.Configs{{Name: "settings", Statics: []loader.ConfigStatic{
		{Content: &loader.ConfigContent{Content: secret}},
		{HTTP: &loader.ConfigHTTP{URL: "https://example.com", Headers: map[string]string{"Authorization": secret}, Query: map[string]string{"token": secret}, Body: secret}},
	}}}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"application", app},
		{"gcp", &gcptoken.GCPToken{Audience: "public-audience", Credentials: secret, Key: secret, ClientSecret: secret}},
		{"oauth client", session.Oauth2{ClientID: "public-client", ClientSecret: secret}},
		{"provider", providers.Generic{ClientID: "public-client", ClientSecret: secret, ClientSecretExternal: secret}},
		{"existing auth tags", auth.Database{DSN: secret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logged, err := configMapCodec.To[map[string]any](tc.value)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(logged)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) {
				t.Fatal("secret leaked into config log")
			}
			// The cfg codec must still include secrets for normal configuration use.
			raw, err := gofret.New().To[map[string]any](tc.value)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err = json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), secret) {
				t.Fatal("log tags affected cfg serialization")
			}
		})
	}
}

func TestConfigMapCodecTagFallback(t *testing.T) {
	type nested struct {
		Public string `cfg:"public_name"`
		Secret string `cfg:"secret" log:"-"`
	}
	type payload struct {
		Children []*nested `cfg:"children"`
		Ignored  string    `cfg:"-"`
	}
	value := payload{Children: []*nested{{Public: "visible", Secret: "hidden"}}, Ignored: "ignored"}
	logged, err := configMapCodec.To[map[string]any](value)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(logged)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"children":[{"public_name":"visible"}]}`; got != want {
		t.Fatalf("log map = %s, want %s", got, want)
	}
	var decoded nested
	if err := config.Decode(map[string]any{"public_name": "visible", "secret": "loaded"}, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Secret != "loaded" {
		t.Fatal("log tag affected configuration decoding")
	}
}
