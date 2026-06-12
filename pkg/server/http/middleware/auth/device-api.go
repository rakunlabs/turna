package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

const grantTypeDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"

// device flow status values.
const (
	deviceStatusPending  = "pending"
	deviceStatusApproved = "approved"
	deviceStatusDenied   = "denied"
)

// deviceFlow is the payload stored in auth_flow_codes for a device login.
type deviceFlow struct {
	ClientID  string   `json:"client_id"`
	Scope     []string `json:"scope"`
	UserCode  string   `json:"user_code"`
	Status    string   `json:"status"`
	UserAlias string   `json:"user_alias,omitempty"`
	LastPoll  int64    `json:"last_poll,omitempty"`
	Interval  int      `json:"interval"`
}

type DeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// user code alphabet avoids vowels and ambiguous characters (RFC 8628 §6.1).
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

func generateUserCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	chars := make([]byte, 8)
	for i, b := range raw {
		chars[i] = userCodeAlphabet[int(b)%len(userCodeAlphabet)]
	}

	return string(chars[:4]) + "-" + string(chars[4:]), nil
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")

	if len(code) == 8 {
		return code[:4] + "-" + code[4:]
	}

	return code
}

// resolveClient validates the client; public clients (no stored secret) are
// allowed when secret is empty, otherwise the secret must match.
func (m *Auth) resolveClient(clientID, clientSecret string) (*AccessClient, error) {
	sn := m.cache.Snapshot()

	client, ok := sn.OAuthClients[clientID]
	if !ok {
		user, err := m.cache.GetUser(data.GetUserRequest{
			Alias:          clientID,
			ServiceAccount: &data.True,
		})
		if err != nil {
			return nil, fmt.Errorf("client %s not found", clientID)
		}

		secret, _ := user.Details["secret"].(string)
		scope, _ := user.Details["scope"].(string)
		whitelistURLs, _ := user.Details["whitelist_urls"].(string)

		client = AccessClient{
			ClientSecret:  secret,
			Scope:         splitFields(scope),
			WhitelistURLs: splitFields(whitelistURLs),
		}
	}

	if client.ClientSecret == "" && clientSecret == "" {
		return &client, nil
	}

	if subtle.ConstantTimeCompare([]byte(client.ClientSecret), []byte(clientSecret)) != 1 {
		return nil, fmt.Errorf("client secret mismatch")
	}

	return &client, nil
}

// APIDeviceAuthorization implements the RFC 8628 device authorization endpoint.
func (m *Auth) APIDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Device
	if cfg.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "device flow is disabled",
			code:             http.StatusBadRequest,
		})

		return
	}

	req := AccessTokenRequest{}
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	clientID, clientSecret := clientCredentials(r, req)
	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client_id is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	if _, err := m.resolveClient(clientID, clientSecret); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}
	deviceCode := hex.EncodeToString(raw)

	userCode, err := generateUserCode()
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	lifetime := cfg.GetCodeLifetime()
	interval := cfg.GetInterval()

	flow := deviceFlow{
		ClientID: clientID,
		Scope:    splitFields(req.Scope),
		UserCode: userCode,
		Status:   deviceStatusPending,
		Interval: interval,
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindDevice, deviceCode, flow, lifetime); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindDeviceUser, userCode,
		map[string]string{"device_code": deviceCode}, lifetime); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	verificationURI := cfg.VerificationURI
	if verificationURI == "" {
		verificationURI = strings.TrimSuffix(m.issuerURL(r), "/oauth2") + "/ui/device"
	}

	w.Header().Set("Cache-Control", "no-store")

	httputil.JSON(w, http.StatusOK, DeviceAuthorizationResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURI + "?user_code=" + userCode,
		ExpiresIn:               int64(lifetime.Seconds()),
		Interval:                interval,
	})
}

// deviceFlowByUserCode loads the device flow entry referenced by a user code.
func (m *Auth) deviceFlowByUserCode(r *http.Request, userCode string) (string, *deviceFlow, error) {
	ref := map[string]string{}
	if err := m.store.GetFlowCode(r.Context(), flowKindDeviceUser, userCode, &ref); err != nil {
		return "", nil, err
	}

	deviceCode := ref["device_code"]

	flow := deviceFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindDevice, deviceCode, &flow); err != nil {
		return "", nil, err
	}

	return deviceCode, &flow, nil
}

// DeviceInfoAPI shows the pending device request for consent display.
func (m *Auth) DeviceInfoAPI(w http.ResponseWriter, r *http.Request) {
	userCode := normalizeUserCode(r.PathValue("user_code"))

	_, flow, err := m.deviceFlowByUserCode(r, userCode)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("device code not found or expired", nil, http.StatusNotFound))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"client_id": flow.ClientID,
			"scope":     strings.Join(flow.Scope, " "),
			"status":    flow.Status,
		},
	})
}

type DeviceApproveRequest struct {
	UserCode string `json:"user_code"`
	// Action is "approve" (default) or "deny".
	Action string `json:"action"`
}

// DeviceApproveAPI approves or denies a device login as the X-User.
func (m *Auth) DeviceApproveAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	var req DeviceApproveRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	userCode := normalizeUserCode(req.UserCode)

	deviceCode, flow, err := m.deviceFlowByUserCode(r, userCode)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("device code not found or expired", nil, http.StatusNotFound))
		return
	}

	if flow.Status != deviceStatusPending {
		httputil.HandleError(w, httputil.NewError("device code already handled", nil, http.StatusConflict))
		return
	}

	if req.Action == "deny" {
		flow.Status = deviceStatusDenied
	} else {
		flow.Status = deviceStatusApproved
		flow.UserAlias = r.Header.Get("X-User")
	}

	if err := m.store.UpdateFlowCode(r.Context(), flowKindDevice, deviceCode, flow); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot update device code", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "device " + flow.Status},
	})
}

// deviceCodeGrant handles grant_type=urn:ietf:params:oauth:grant-type:device_code.
func (m *Auth) deviceCodeGrant(w http.ResponseWriter, r *http.Request, req AccessTokenRequest, clientID, clientSecret string) {
	cfg := m.cache.Snapshot().Device
	if cfg.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "device flow is disabled",
			code:             http.StatusBadRequest,
		})

		return
	}

	if _, err := m.resolveClient(clientID, clientSecret); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	if req.DeviceCode == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "device_code is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	flow := deviceFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindDevice, req.DeviceCode, &flow); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "expired_token",
			ErrorDescription: "device code not found or expired",
			code:             http.StatusBadRequest,
		})

		return
	}

	if flow.ClientID != clientID {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "client mismatch",
			code:             http.StatusBadRequest,
		})

		return
	}

	// poll rate limiting (RFC 8628 §3.5 slow_down)
	now := time.Now().Unix()
	if flow.LastPoll > 0 && now-flow.LastPoll < int64(flow.Interval) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error: "slow_down",
			code:  http.StatusBadRequest,
		})

		return
	}

	flow.LastPoll = now
	_ = m.store.UpdateFlowCode(r.Context(), flowKindDevice, req.DeviceCode, &flow)

	switch flow.Status {
	case deviceStatusDenied:
		_ = m.store.DeleteFlowCode(r.Context(), flowKindDevice, req.DeviceCode)
		_ = m.store.DeleteFlowCode(r.Context(), flowKindDeviceUser, flow.UserCode)

		httputil.HandleError(w, AccessTokenErrorResponse{
			Error: "access_denied",
			code:  http.StatusBadRequest,
		})

		return
	case deviceStatusApproved:
		user, err := m.cache.GetUser(data.GetUserRequest{
			Alias:         flow.UserAlias,
			AddScopeRoles: true,
		})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		_ = m.store.DeleteFlowCode(r.Context(), flowKindDevice, req.DeviceCode)
		_ = m.store.DeleteFlowCode(r.Context(), flowKindDeviceUser, flow.UserCode)

		m.writeToken(w, r, user, clientID, flow.Scope, nil)

		return
	default:
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error: "authorization_pending",
			code:  http.StatusBadRequest,
		})

		return
	}
}
