package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type ConfigRequest struct {
	Enabled *bool           `json:"enabled"`
	Config  json.RawMessage `json:"config"`
}

type listConfigFunc func(context.Context) ([]ConfigMeta, error)
type getConfigFunc func(context.Context, string) (*ConfigResource, error)
type putConfigFunc func(context.Context, string, json.RawMessage, bool, string) (uint64, error)
type deleteConfigFunc func(context.Context, string) (uint64, error)

func (m *Auth) ListOAuthClients(w http.ResponseWriter, r *http.Request) {
	m.listConfigResources(w, r, m.store.ListOAuthClients, "cannot list oauth clients")
}

func (m *Auth) GetOAuthClient(w http.ResponseWriter, r *http.Request) {
	m.getConfigResource(w, r, m.store.GetOAuthClient, "cannot get oauth client")
}

func (m *Auth) PutOAuthClient(w http.ResponseWriter, r *http.Request) {
	m.putConfigResource(w, r, m.store.PutOAuthClient, "cannot save oauth client")
}

func (m *Auth) DeleteOAuthClient(w http.ResponseWriter, r *http.Request) {
	m.deleteConfigResource(w, r, m.store.DeleteOAuthClient, "cannot delete oauth client")
}

func (m *Auth) ListOAuthProviders(w http.ResponseWriter, r *http.Request) {
	m.listConfigResources(w, r, m.store.ListOAuthProviders, "cannot list oauth providers")
}

func (m *Auth) GetOAuthProvider(w http.ResponseWriter, r *http.Request) {
	m.getConfigResource(w, r, m.store.GetOAuthProvider, "cannot get oauth provider")
}

func (m *Auth) PutOAuthProvider(w http.ResponseWriter, r *http.Request) {
	m.putConfigResource(w, r, m.store.PutOAuthProvider, "cannot save oauth provider")
}

func (m *Auth) DeleteOAuthProvider(w http.ResponseWriter, r *http.Request) {
	m.deleteConfigResource(w, r, m.store.DeleteOAuthProvider, "cannot delete oauth provider")
}

func (m *Auth) ListLDAPConfigs(w http.ResponseWriter, r *http.Request) {
	m.listConfigResources(w, r, m.store.ListLDAPConfigs, "cannot list ldap configs")
}

func (m *Auth) GetLDAPConfig(w http.ResponseWriter, r *http.Request) {
	m.getConfigResource(w, r, m.store.GetLDAPConfig, "cannot get ldap config")
}

func (m *Auth) PutLDAPConfig(w http.ResponseWriter, r *http.Request) {
	m.putConfigResource(w, r, m.store.PutLDAPConfig, "cannot save ldap config")
}

func (m *Auth) DeleteLDAPConfig(w http.ResponseWriter, r *http.Request) {
	m.deleteConfigResource(w, r, m.store.DeleteLDAPConfig, "cannot delete ldap config")
}

func (m *Auth) listConfigResources(w http.ResponseWriter, r *http.Request, fn listConfigFunc, msg string) {
	resources, err := fn(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusInternalServerError))
		return
	}

	q := parseListQuery(r)

	if v := q.GetValue("id"); v != "" {
		filtered := make([]ConfigMeta, 0, len(resources))
		for _, resource := range resources {
			if containsFold(resource.ID, v) {
				filtered = append(filtered, resource)
			}
		}

		resources = filtered
	}

	total := uint64(len(resources))
	limit, offset := getLimitOffset(q)
	resources = paginate(resources, limit, offset)

	httputil.JSON(w, http.StatusOK, Response[[]ConfigMeta]{
		Meta:    &Meta{TotalItemCount: total},
		Payload: resources,
	})
}

func (m *Auth) getConfigResource(w http.ResponseWriter, r *http.Request, fn getConfigFunc, msg string) {
	resource, err := fn(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("config not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[*ConfigResource]{Payload: resource})
}

func (m *Auth) putConfigResource(w http.ResponseWriter, r *http.Request, fn putConfigFunc, msg string) {
	var req ConfigRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode config", err, http.StatusBadRequest))
		return
	}

	if len(req.Config) == 0 {
		httputil.HandleError(w, httputil.NewError("config is required", nil, http.StatusBadRequest))
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	version, err := fn(r.Context(), r.PathValue("id"), req.Config, enabled, getUserName(r))
	if err != nil {
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Meta: &Meta{Version: version},
		Payload: map[string]any{
			"message": "config saved",
		},
	})
}

func (m *Auth) deleteConfigResource(w http.ResponseWriter, r *http.Request, fn deleteConfigFunc, msg string) {
	version, err := fn(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("config not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Meta: &Meta{Version: version},
		Payload: map[string]any{
			"message": "config deleted",
		},
	})
}
