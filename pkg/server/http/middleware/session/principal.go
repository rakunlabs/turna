package session

import (
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

const apiKeyAuthenticatedContextKey = "session_api_key_authenticated"

// RequestPrincipal returns the stable identity used for authorization. API key
// presentation headers may be friendly, but their authenticated principal remains
// the validated api-key:<id> subject carried in the request claims.
func RequestPrincipal(r *http.Request) string {
	xUser := strings.TrimSpace(r.Header.Get("X-User"))
	authenticated, _ := tcontext.Get(r, apiKeyAuthenticatedContextKey).(bool)
	if !authenticated {
		return xUser
	}

	customClaims, ok := tcontext.Get(r, "claims").(*claims.Custom)
	if !ok {
		return ""
	}

	return apiKeyPrincipal(customClaims)
}

func apiKeyPrincipal(customClaims *claims.Custom) string {
	if customClaims == nil || customClaims.Map["principal_type"] != "api_key" {
		return ""
	}

	id, _ := customClaims.Map["api_key_id"].(string)
	subject, _ := customClaims.Map["sub"].(string)
	id = strings.TrimSpace(id)
	subject = strings.TrimSpace(subject)
	if id == "" || subject != "api-key:"+id {
		return ""
	}

	return subject
}
