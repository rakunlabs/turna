package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Response[T any] struct {
	Payload T     `json:"payload"`
	Meta    *Meta `json:"meta,omitempty"`
}

type Meta struct {
	TotalItemCount uint64 `json:"total_item_count,omitempty"`
	Version        uint64 `json:"version,omitempty"`
}

type SettingRequest struct {
	Value json.RawMessage `json:"value"`
}

func getUserName(r *http.Request) string {
	if v := r.Header.Get("X-User"); v != "" {
		return v
	}

	return "unknown"
}

func (m *Auth) Info(w http.ResponseWriter, r *http.Request) {
	version, err := m.store.Version(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot get auth version", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"prefix_path": m.PrefixPath,
			"version":     version,
			"storage":     "postgres",
		},
	})
}

func (m *Auth) ListSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := m.store.ListSettings(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot list auth settings", err, http.StatusInternalServerError))
		return
	}

	q, err := parseAPIQuery(r)
	if err != nil {
		handleQueryError(w, err)
		return
	}

	if v := q.GetValue("namespace"); v != "" {
		filtered := make([]SettingMeta, 0, len(settings))
		for _, setting := range settings {
			if containsFold(setting.Namespace, v) {
				filtered = append(filtered, setting)
			}
		}

		settings = filtered
	}

	total := uint64(len(settings))
	limit, offset := getLimitOffset(q)
	settings = paginate(settings, limit, offset)

	httputil.JSON(w, http.StatusOK, Response[[]SettingMeta]{
		Meta:    &Meta{TotalItemCount: total},
		Payload: settings,
	})
}

// paginate applies limit/offset windowing to a list.
func paginate[T any](items []T, limit, offset int64) []T {
	if offset > 0 {
		if int(offset) >= len(items) {
			items = nil
		} else {
			items = items[offset:]
		}
	}

	if limit > 0 && int(limit) < len(items) {
		items = items[:limit]
	}

	return items
}

func (m *Auth) GetSetting(w http.ResponseWriter, r *http.Request) {
	setting, err := m.store.GetSetting(r.Context(), r.PathValue("namespace"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("setting not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("cannot get auth setting", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[*Setting]{Payload: setting})
}

func (m *Auth) PutSetting(w http.ResponseWriter, r *http.Request) {
	var req SettingRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode setting", err, http.StatusBadRequest))
		return
	}

	if len(req.Value) == 0 {
		httputil.HandleError(w, httputil.NewError("setting value is required", nil, http.StatusBadRequest))
		return
	}

	namespace := r.PathValue("namespace")

	if namespace == "cache" {
		var setting CacheSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode cache setting", err, http.StatusBadRequest))
			return
		}
		if err := validateCodeStoreSettings(setting.CodeStore); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid cache setting", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "token" {
		var setting TokenSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode token setting", err, http.StatusBadRequest))
			return
		}
		if err := validateTokenSettings(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid token setting", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "passkey" {
		var setting PasskeySettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode passkey setting", err, http.StatusBadRequest))
			return
		}
		if err := validatePasskeySettings(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid passkey setting", err, http.StatusBadRequest))
			return
		}
	}

	// the jwt namespace carries the signing key; reject broken material early
	if namespace == jwtSettingNamespace {
		var setting jwtSetting
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode jwt setting", err, http.StatusBadRequest))
			return
		}

		if err := validateJWTSetting(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid jwt setting", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "email" {
		var setting EmailSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode email setting", err, http.StatusBadRequest))
			return
		}
		if err := parseEmailSettingDurations(&setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid email code_lifetime", err, http.StatusBadRequest))
			return
		}
		if err := validateEmailTemplates(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid email template", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "signup" {
		var setting SignupSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode signup setting", err, http.StatusBadRequest))
			return
		}
		if err := parseSignupSettingDurations(&setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid signup code_lifetime", err, http.StatusBadRequest))
			return
		}
		if err := validateSignupTemplates(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid signup template", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "mtls" {
		var setting MTLSSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode mtls setting", err, http.StatusBadRequest))
			return
		}
		if err := validateMTLSSettings(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid mtls setting", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "custom_info" {
		var setting CustomInfoSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode custom_info setting", err, http.StatusBadRequest))
			return
		}
		if err := validateCustomInfoTemplates(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid custom_info template", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "session_providers" {
		var setting SessionProviderSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode session_providers setting", err, http.StatusBadRequest))
			return
		}
		if err := validateSessionProviders(setting); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid session_providers setting", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "authorize" {
		var setting AuthorizeSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode authorize setting", err, http.StatusBadRequest))
			return
		}
		if err := validateDuration(setting.FlowLifetime); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid authorize flow_lifetime", err, http.StatusBadRequest))
			return
		}
	}

	if namespace == "registration" {
		var setting RegistrationSettings
		if err := json.Unmarshal(req.Value, &setting); err != nil {
			httputil.HandleError(w, httputil.NewError("cannot decode registration setting", err, http.StatusBadRequest))
			return
		}
		if err := validateDuration(setting.ClientLifetime); err != nil {
			httputil.HandleError(w, httputil.NewError("invalid registration client_lifetime", err, http.StatusBadRequest))
			return
		}
	}

	version, err := m.store.PutSetting(r.Context(), namespace, req.Value, getUserName(r))
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save auth setting", err, http.StatusInternalServerError))
		return
	}

	// Apply immediately here; peers use notifications with polling as fallback.
	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, httputil.NewError("setting saved but reload failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Meta: &Meta{Version: version},
		Payload: map[string]any{
			"message": "setting saved",
		},
	})
}

// RotateJWTAPI generates a fresh RSA signing key and applies it immediately.
// Outstanding access and refresh tokens become invalid.
func (m *Auth) RotateJWTAPI(w http.ResponseWriter, r *http.Request) {
	setting, err := m.generateJWTSetting(r.Context(), getUserName(r))
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot rotate jwt key", err, http.StatusInternalServerError))
		return
	}

	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, httputil.NewError("key rotated but reload failed", err, http.StatusInternalServerError))
		return
	}

	if _, err := m.jwtRuntime(r.Context()); err != nil {
		httputil.HandleError(w, httputil.NewError("key rotated but signer rebuild failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message": "jwt key rotated",
			"kid":     setting.KID,
		},
	})
}

func (m *Auth) DeleteSetting(w http.ResponseWriter, r *http.Request) {
	version, err := m.store.DeleteSetting(r.Context(), r.PathValue("namespace"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("setting not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("cannot delete auth setting", err, http.StatusInternalServerError))
		return
	}

	// Apply immediately here; peers use notifications with polling as fallback.
	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, httputil.NewError("setting deleted but reload failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Meta: &Meta{Version: version},
		Payload: map[string]any{
			"message": "setting deleted",
		},
	})
}
