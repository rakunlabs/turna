package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/ada/middleware/auth/strategy/passkey"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

const (
	passkeyRegisterPrefix = "passkey_reg_"
	passkeyLoginPrefix    = "passkey_login_"

	// passkeyChallengeTTL matches the code store state TTL so challenges
	// expire together with their store entries.
	passkeyChallengeTTL = 2 * time.Minute
)

// passkeyEngine builds the WebAuthn engine from the "passkey" runtime
// setting, deriving rp_id/origins from the request when not configured.
func (m *Auth) passkeyEngine(r *http.Request) (*passkey.WebAuthn, error) {
	cfg := m.cache.Snapshot().Passkey
	if cfg.Disabled {
		return nil, errors.New("passkey is disabled")
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	rpID := cfg.RPID
	if rpID == "" {
		rpID = host
		if hostname, _, err := net.SplitHostPort(host); err == nil {
			rpID = hostname
		}
	}

	origins := cfg.Origins
	if len(origins) == 0 {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}

		origins = []string{scheme + "://" + host}
	}

	displayName := cfg.RPDisplayName
	if displayName == "" {
		displayName = "Turna Auth"
	}

	uv := passkey.UVPreferred
	switch cfg.UserVerification {
	case "required":
		uv = passkey.UVRequired
	case "discouraged":
		uv = passkey.UVDiscouraged
	}

	return passkey.New(&passkey.Config{
		RPID:             rpID,
		RPDisplayName:    displayName,
		RPOrigins:        origins,
		UserVerification: uv,
		ChallengeTTL:     passkeyChallengeTTL,
	})
}

// passkey challenge session helpers backed by the code store.

func (m *Auth) passkeySessionSave(ctx context.Context, key string, session *passkey.SessionData) (string, error) {
	codeStore, err := m.codeStoreRuntime(ctx)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(session)
	if err != nil {
		return "", err
	}

	sessionID := ulid.Make().String()
	if err := codeStore.State.Set(ctx, key+sessionID, string(raw)); err != nil {
		return "", err
	}

	return sessionID, nil
}

func (m *Auth) passkeySessionTake(ctx context.Context, key, sessionID string) (*passkey.SessionData, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	codeStore, err := m.codeStoreRuntime(ctx)
	if err != nil {
		return nil, err
	}

	raw, ok, err := codeStore.State.Get(ctx, key+sessionID)
	if err != nil || !ok {
		return nil, errors.New("passkey session not found or expired")
	}

	_ = codeStore.State.Delete(ctx, key+sessionID)

	var session passkey.SessionData
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (m *Auth) passkeyExcludeList(ctx context.Context, userID string) []passkey.PublicKeyCredentialDescriptor {
	ids, err := m.store.ListPasskeyCredentialIDs(ctx, userID)
	if err != nil {
		return nil
	}

	exclude := make([]passkey.PublicKeyCredentialDescriptor, 0, len(ids))
	for _, id := range ids {
		exclude = append(exclude, passkey.PublicKeyCredentialDescriptor{
			Type: "public-key",
			ID:   passkey.Base64URLEncode(id),
		})
	}

	return exclude
}

// ////////////////////////////////////////////////////////////////////
// registration (X-User authenticated management plane)

type PasskeyRegisterRequest struct {
	// UserID, when set, registers the passkey for that user instead of the
	// X-User identity. Targeting another user requires admin capability.
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	// Name is a user-facing label for the credential.
	Name string `json:"name"`
	// Credential is the browser's RegistrationResponseJSON; nil means begin.
	Credential json.RawMessage `json:"credential"`
}

// passkeyRegisterTarget resolves the user a passkey is being registered for.
// An explicit userID requires admin capability unless it is the caller's own
// X-User identity; without one it falls back to the X-User self-service plane.
// It writes the error response and returns nil when resolution/authorization
// fails.
func (m *Auth) passkeyRegisterTarget(w http.ResponseWriter, r *http.Request, userID string) *data.UserExtended {
	if userID == "" {
		return m.xUser(w, r)
	}

	if alias := r.Header.Get("X-User"); alias != "" {
		if self, err := m.cache.GetUser(data.GetUserRequest{Alias: alias}); err == nil && self.ID == userID {
			return self
		}
	}

	if !m.requireAdmin(w, r) {
		return nil
	}

	user, err := m.cache.GetUser(data.GetUserRequest{ID: userID})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
		return nil
	}

	return user
}

type PasskeyBeginResponse struct {
	SessionID string `json:"session_id"`
	Options   any    `json:"options"`
}

// PasskeyRegisterAPI begins/finishes passkey registration. Without user_id it
// targets the X-User identity (self-service); with user_id it registers for
// that user and requires admin capability.
func (m *Auth) PasskeyRegisterAPI(w http.ResponseWriter, r *http.Request) {
	var req PasskeyRegisterRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	user := m.passkeyRegisterTarget(w, r, req.UserID)
	if user == nil {
		return
	}

	engine, err := m.passkeyEngine(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("passkey not available", err, http.StatusServiceUnavailable))
		return
	}

	username := user.ID
	if len(user.Alias) > 0 && user.Alias[0] != "" {
		username = user.Alias[0]
	}

	// begin
	if len(req.Credential) == 0 {
		name, _ := user.Details["name"].(string)
		if name == "" {
			name = username
		}

		options, session, err := engine.BeginRegistration(passkey.User{
			Handle:      []byte(user.ID),
			Name:        username,
			DisplayName: name,
		}, m.passkeyExcludeList(r.Context(), user.ID))
		if err != nil {
			httputil.HandleError(w, httputil.NewError("cannot begin registration", err, http.StatusInternalServerError))
			return
		}

		sessionID, err := m.passkeySessionSave(r.Context(), passkeyRegisterPrefix, session)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("cannot save session", err, http.StatusInternalServerError))
			return
		}

		httputil.JSON(w, http.StatusOK, Response[PasskeyBeginResponse]{
			Payload: PasskeyBeginResponse{SessionID: sessionID, Options: options},
		})

		return
	}

	// finish
	session, err := m.passkeySessionTake(r.Context(), passkeyRegisterPrefix, req.SessionID)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("registration session invalid", err, http.StatusBadRequest))
		return
	}

	cred, _, err := engine.FinishRegistration(session, req.Credential)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("registration failed", err, http.StatusBadRequest))
		return
	}

	if err := m.store.CreatePasskeyCredential(r.Context(), user.ID, req.Name, cred); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save credential", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message": "passkey registered",
			"id":      passkeyCredentialKey(cred.ID),
		},
	})
}

// PasskeyCredentialsAPI lists own passkeys. Querying another user_id requires
// admin capability.
func (m *Auth) PasskeyCredentialsAPI(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		user := m.xUser(w, r)
		if user == nil {
			return
		}

		userID = user.ID
	} else if alias := r.Header.Get("X-User"); alias != "" {
		user, err := m.cache.GetUser(data.GetUserRequest{Alias: alias})
		if err != nil || user.ID != userID {
			if !m.requireAdmin(w, r) {
				return
			}
		}
	} else if !m.requireAdmin(w, r) {
		return
	}

	list, err := m.store.ListPasskeyCredentials(r.Context(), userID)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot list passkeys", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[[]PasskeyCredentialMeta]{
		Meta:    &Meta{TotalItemCount: uint64(len(list))},
		Payload: list,
	})
}

// PasskeyCredentialDeleteAPI removes a stored passkey credential.
func (m *Auth) PasskeyCredentialDeleteAPI(w http.ResponseWriter, r *http.Request) {
	meta, err := m.store.GetPasskeyCredentialMeta(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("passkey not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("cannot get passkey", err, http.StatusInternalServerError))
		return
	}

	alias := r.Header.Get("X-User")
	if alias == "" {
		if !m.requireAdmin(w, r) {
			return
		}
	} else {
		user, err := m.cache.GetUser(data.GetUserRequest{Alias: alias})
		if err != nil || user.ID != meta.UserID {
			if !m.requireAdmin(w, r) {
				return
			}
		}
	}

	if err := m.store.DeletePasskeyCredential(r.Context(), meta.ID); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("passkey not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("cannot delete passkey", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "passkey deleted"},
	})
}

// ////////////////////////////////////////////////////////////////////
// login (public token plane)

type PasskeyTokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// Username scopes allowCredentials to a known user; empty uses the
	// discoverable (passwordless) flow.
	Username  string `json:"username"`
	Scope     string `json:"scope"`
	SessionID string `json:"session_id"`
	// Assertion is the browser's AssertionResponseJSON; nil means begin.
	Assertion json.RawMessage `json:"assertion"`
}

// APIPasskeyToken begins/finishes passkey login and issues tokens.
// Finish responses use the same shape as the token endpoint.
func (m *Auth) APIPasskeyToken(w http.ResponseWriter, r *http.Request) {
	var req PasskeyTokenRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	accessClient, err := m.GetAccessClient(req.ClientID, req.ClientSecret)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	engine, err := m.passkeyEngine(r)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "passkey_unavailable",
			ErrorDescription: err.Error(),
			code:             http.StatusServiceUnavailable,
		})

		return
	}

	// begin
	if len(req.Assertion) == 0 {
		var allowed [][]byte
		if req.Username != "" {
			if user, err := m.cache.GetUser(data.GetUserRequest{Alias: req.Username}); err == nil {
				allowed, _ = m.store.ListPasskeyCredentialIDs(r.Context(), user.ID)
			}
		}

		options, session, err := engine.BeginLogin(allowed)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		sessionID, err := m.passkeySessionSave(r.Context(), passkeyLoginPrefix, session)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		httputil.JSON(w, http.StatusOK, PasskeyBeginResponse{SessionID: sessionID, Options: options})

		return
	}

	// finish
	session, err := m.passkeySessionTake(r.Context(), passkeyLoginPrefix, req.SessionID)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	var assertion struct {
		RawID string `json:"rawId"`
	}
	if err := json.Unmarshal(req.Assertion, &assertion); err != nil || assertion.RawID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "assertion rawId not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	rawID, err := passkey.Base64URLDecode(assertion.RawID)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "assertion rawId invalid",
			code:             http.StatusBadRequest,
		})

		return
	}

	userID, cred, err := m.store.GetPasskeyCredential(r.Context(), rawID)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "credential not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	result, err := engine.FinishLogin(session, cred, req.Assertion)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	// best-effort sign counter persistence for clone detection
	_ = m.store.UpdatePasskeySignCount(r.Context(), cred.ID, result.NewSignCount)

	user, err := m.cache.GetUser(data.GetUserRequest{ID: userID, AddScopeRoles: true})
	if err != nil || user.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	m.writeToken(w, r, user, req.ClientID, splitFields(req.Scope), accessClient.Scope)
}
