package auth

import (
	"fmt"
	"net/http"

	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

// validateCustomInfoTemplates parses every custom_info template so syntax errors
// are rejected on save. It deliberately does NOT execute the templates: which
// claim/user fields exist depends on the deployment (LDAP attribute mapping,
// edited user details), so validating against fixed sample data would wrongly
// reject valid templates that reference deployment-specific fields. Use the
// preview endpoint to test rendering against real-shaped sample data.
func validateCustomInfoTemplates(cfg CustomInfoSettings) error {
	for name, set := range cfg.Sets {
		for k, tmpl := range set.Claims {
			if err := render.Validate(tmpl); err != nil {
				return fmt.Errorf("set %q claim %q: %w", name, k, err)
			}
		}
	}

	return nil
}

// customInfoPreviewRequest renders a custom_info set against sample data so the
// management UI can preview unsaved templates.
type customInfoPreviewRequest struct {
	Claims map[string]any `json:"claims"`
	User   *data.User     `json:"user"`
	Set    CustomInfoSet  `json:"set"`
}

// CustomInfoPreviewAPI applies an inline custom_info set to sample claims and
// returns the resulting claims, mirroring the templating in APIUserInfo
// (add, overwrite, or remove on empty render).
func (m *Auth) CustomInfoPreviewAPI(w http.ResponseWriter, r *http.Request) {
	var req customInfoPreviewRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode custom_info preview", err, http.StatusBadRequest))
		return
	}

	user := req.User
	if user == nil {
		user = &data.User{}
	}

	// base is the immutable template input; result is what we mutate/return.
	base := make(map[string]any, len(req.Claims))
	result := make(map[string]any, len(req.Claims))
	for k, v := range req.Claims {
		base[k] = v
		result[k] = v
	}

	tmplData := map[string]any{"claims": base, "user": user}
	for k, tmpl := range req.Set.Claims {
		out, err := render.ExecuteWithData(tmpl, tmplData)
		if err != nil {
			httputil.HandleError(w, httputil.NewError(fmt.Sprintf("claim %q: %s", k, err.Error()), err, http.StatusBadRequest))
			return
		}

		if len(out) == 0 {
			delete(result, k)

			continue
		}

		result[k] = string(out)
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{Payload: result})
}
