package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/access"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/xhit/go-str2duration/v2"
)

// signupFlow is the pending registration stored in auth_flow_codes until the
// email is verified. The password is stored as a bcrypt hash, never plain.
type signupFlow struct {
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"password_hash"`
	ClientID     string `json:"client_id"`
}

// resetFlow is the pending password reset stored in auth_flow_codes.
type resetFlow struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	ClientID string `json:"client_id"`
}

const (
	defaultSignupVerifySubject  = "Verify your email"
	defaultSignupVerifyBody     = "{{if .MagicLink}}Click the link to verify your email:\r\n\r\n{{.MagicLink}}\r\n\r\nOr use this verification code: {{.Code}}{{else}}Your verification code is:\r\n\r\n{{.Code}}{{end}}\r\n\r\nThe code expires in {{.ExpiresIn}}."
	defaultPasswordResetSubject = "Reset your password"
	defaultPasswordResetBody    = "{{if .MagicLink}}Click the link to reset your password:\r\n\r\n{{.MagicLink}}\r\n\r\nOr use this reset code: {{.Code}}{{else}}Your password reset code is:\r\n\r\n{{.Code}}{{end}}\r\n\r\nThe code expires in {{.ExpiresIn}}."
)

type SignupRequest struct {
	ClientID     string `form:"client_id"     json:"client_id"`
	ClientSecret string `form:"client_secret" json:"client_secret"`
	Email        string `form:"email"         json:"email"`
	Name         string `form:"name"          json:"name"`
	Password     string `form:"password"      json:"password"`
	// RedirectURI builds the magic link in the mail; it must match the
	// client whitelist. The verification code is appended as ?code=...
	RedirectURI string `form:"redirect_uri" json:"redirect_uri"`
	// Code finishes a flow (verify / reset confirm).
	Code string `form:"code" json:"code"`
}

// renderSignupMail renders a subject/body template pair with fallbacks.
func renderSignupMail(subjectTpl, defaultSubject, bodyTpl, defaultBody string, data EmailTemplateData) (string, string, error) {
	if strings.TrimSpace(subjectTpl) == "" {
		subjectTpl = defaultSubject
	}
	if strings.TrimSpace(bodyTpl) == "" {
		bodyTpl = defaultBody
	}

	subject, err := renderEmailTemplate("signup_subject", subjectTpl, data)
	if err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}
	subject = cleanEmailSubject(subject)
	if subject == "" {
		subject = defaultSubject
	}

	body, err := renderEmailTemplate("signup_body", bodyTpl, data)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}

	return subject, body, nil
}

// validateSignupTemplates renders all signup mail templates with sample data
// so broken templates are rejected at save time.
func validateSignupTemplates(cfg SignupSettings) error {
	sample := EmailTemplateData{
		Email:       "user@example.com",
		Name:        "User Name",
		Code:        "123456",
		MagicLink:   "https://app.example.com/login/?code=123456",
		ExpiresIn:   cfg.GetCodeLifetime().String(),
		ClientID:    "client",
		RedirectURI: "https://app.example.com/login/",
	}

	if _, _, err := renderSignupMail(cfg.VerifySubject, defaultSignupVerifySubject, cfg.VerifyBodyTemplate, defaultSignupVerifyBody, sample); err != nil {
		return fmt.Errorf("verify template: %w", err)
	}
	if _, _, err := renderSignupMail(cfg.ResetSubject, defaultPasswordResetSubject, cfg.ResetBodyTemplate, defaultPasswordResetBody, sample); err != nil {
		return fmt.Errorf("reset template: %w", err)
	}

	return nil
}

func parseSignupSettingDurations(cfg *SignupSettings) error {
	if strings.TrimSpace(cfg.CodeLifetime) == "" {
		return nil
	}

	d, err := str2duration.ParseDuration(cfg.CodeLifetime)
	if err != nil {
		return err
	}
	cfg.codeLifetime = d

	return nil
}

// signupClient validates the posted client credentials like the email login
// endpoint; signup endpoints are public, so a registered client is required.
func (m *Auth) signupClient(w http.ResponseWriter, r *http.Request, req SignupRequest) (string, *AccessClient) {
	clientID := req.ClientID
	if clientID == "" {
		if basicID, basicSecret, ok := r.BasicAuth(); ok {
			clientID, req.ClientSecret = basicID, basicSecret
		}
	}

	var client *AccessClient
	if clientID != "" {
		client, _ = m.resolveClient(clientID, req.ClientSecret)
	}
	if client == nil {
		httputil.HandleError(w, httputil.NewError("client not valid", nil, http.StatusUnauthorized))
		return "", nil
	}

	return clientID, client
}

func newFlowCodeValue() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw), nil
}

// APISignup registers a new local user. With email verification enabled the
// account is created only after /oauth2/signup/verify; the response is always
// generic to avoid account enumeration. Without verification the user is
// created immediately and duplicate addresses answer 409.
func (m *Auth) APISignup(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Signup
	if !cfg.Enabled {
		httputil.HandleError(w, httputil.NewError("signup is disabled", nil, http.StatusForbidden))
		return
	}

	var req SignupRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	clientID, client := m.signupClient(w, r, req)
	if client == nil {
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		httputil.HandleError(w, httputil.NewError("a valid email is required", nil, http.StatusBadRequest))
		return
	}
	if minLen := cfg.GetPasswordMinLength(); len(req.Password) < minLen {
		httputil.HandleError(w, httputil.NewError(fmt.Sprintf("password must be at least %d characters", minLen), nil, http.StatusBadRequest))
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
	}

	verification := cfg.GetEmailVerification()

	if !verification {
		_, err := m.createSignupUser(r.Context(), email, name, req.Password, cfg.DefaultRoleIDs)
		if err != nil {
			if errors.Is(err, data.ErrConflict) {
				httputil.HandleError(w, httputil.NewError("account already exists", nil, http.StatusConflict))
				return
			}

			httputil.HandleError(w, httputil.NewError("cannot create account", err, http.StatusInternalServerError))

			return
		}

		httputil.JSON(w, http.StatusOK, Response[map[string]any]{
			Payload: map[string]any{
				"message":               "account created",
				"verification_required": false,
			},
		})

		return
	}

	// verification path needs a working mail relay
	emailCfg := m.cache.Snapshot().Email
	if emailCfg.SMTP.Host == "" {
		httputil.HandleError(w, httputil.NewError("email delivery is not configured", nil, http.StatusServiceUnavailable))
		return
	}

	accepted := func() {
		httputil.JSON(w, http.StatusOK, Response[map[string]any]{
			Payload: map[string]any{
				"message":               "if the address is new, a verification email was sent",
				"verification_required": true,
			},
		})
	}

	// do not leak whether the address is registered
	if _, err := m.cache.GetUser(data.GetUserRequest{Alias: email}); err == nil {
		accepted()
		return
	}

	passwordHash, err := access.ToBcrypt([]byte(req.Password))
	if err != nil {
		accepted()
		return
	}

	code, err := newFlowCodeValue()
	if err != nil {
		accepted()
		return
	}

	flow := signupFlow{
		Email:        email,
		Name:         name,
		PasswordHash: passwordHash,
		ClientID:     clientID,
	}
	if err := m.store.CreateFlowCode(r.Context(), flowKindSignup, hashEmailCode(code), flow, cfg.GetCodeLifetime()); err != nil {
		slog.Error("signup code store failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	mailData := EmailTemplateData{
		Email:       email,
		Name:        name,
		Code:        code,
		MagicLink:   emailMagicLink(req.RedirectURI, client.WhitelistURLs, code),
		ExpiresIn:   cfg.GetCodeLifetime().String(),
		ClientID:    clientID,
		RedirectURI: req.RedirectURI,
	}

	subject, body, err := renderSignupMail(cfg.VerifySubject, defaultSignupVerifySubject, cfg.VerifyBodyTemplate, defaultSignupVerifyBody, mailData)
	if err != nil {
		slog.Error("signup template render failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	go func() {
		if err := sendEmail(emailCfg, email, subject, body); err != nil {
			slog.Error("signup mail send failed", slog.String("error", err.Error()))
		}
	}()

	accepted()
}

// createSignupUser creates the local user record for a signup. The password
// may be plain text or an already-bcrypt hash (hashUserPassword skips those).
func (m *Auth) createSignupUser(ctx context.Context, email, name, password string, roleIDs []string) (string, error) {
	id, err := m.store.CreateUser(data.WithContextUserName(ctx, "signup"), data.User{
		Alias:   []string{email},
		Local:   true,
		RoleIDs: slicesUnique(roleIDs),
		Details: map[string]any{
			"email":    email,
			"name":     name,
			"password": password,
		},
	})
	if err != nil {
		return "", err
	}

	if err := m.cache.Reload(ctx); err != nil {
		slog.Error("signup cache reload failed", slog.String("error", err.Error()))
	}

	return id, nil
}

// APISignupVerify finishes email verification and creates the account.
func (m *Auth) APISignupVerify(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Signup
	if !cfg.Enabled {
		httputil.HandleError(w, httputil.NewError("signup is disabled", nil, http.StatusForbidden))
		return
	}

	var req SignupRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		httputil.HandleError(w, httputil.NewError("code is required", nil, http.StatusBadRequest))
		return
	}

	codeHash := hashEmailCode(code)

	flow := signupFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindSignup, codeHash, &flow); err != nil {
		httputil.HandleError(w, httputil.NewError("code not found or expired", nil, http.StatusUnauthorized))
		return
	}

	// single use
	_ = m.store.DeleteFlowCode(r.Context(), flowKindSignup, codeHash)

	if _, err := m.createSignupUser(r.Context(), flow.Email, flow.Name, flow.PasswordHash, cfg.DefaultRoleIDs); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("account already exists", nil, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("cannot create account", err, http.StatusInternalServerError))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "email verified; you can sign in now"},
	})
}

// APIPasswordReset sends a password reset code/magic link. The response is
// always 200 to avoid account enumeration.
func (m *Auth) APIPasswordReset(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Signup
	if !cfg.PasswordReset {
		httputil.HandleError(w, httputil.NewError("password reset is disabled", nil, http.StatusForbidden))
		return
	}

	emailCfg := m.cache.Snapshot().Email
	if emailCfg.SMTP.Host == "" {
		httputil.HandleError(w, httputil.NewError("email delivery is not configured", nil, http.StatusServiceUnavailable))
		return
	}

	var req SignupRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	clientID, client := m.signupClient(w, r, req)
	if client == nil {
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		httputil.HandleError(w, httputil.NewError("a valid email is required", nil, http.StatusBadRequest))
		return
	}

	accepted := func() {
		httputil.JSON(w, http.StatusOK, Response[map[string]any]{
			Payload: map[string]any{"message": "if the account exists, a reset email was sent"},
		})
	}

	user, err := m.cache.GetUser(data.GetUserRequest{Alias: email})
	if err != nil || user.Disabled || !user.Local {
		// non-local users reset their password at the upstream IdP/LDAP
		accepted()
		return
	}

	code, err := newFlowCodeValue()
	if err != nil {
		accepted()
		return
	}

	flow := resetFlow{UserID: user.ID, Email: email, ClientID: clientID}
	if err := m.store.CreateFlowCode(r.Context(), flowKindPasswordReset, hashEmailCode(code), flow, cfg.GetCodeLifetime()); err != nil {
		slog.Error("password reset code store failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	name, _ := user.Details["name"].(string)
	mailData := EmailTemplateData{
		Email:       email,
		Name:        name,
		Code:        code,
		MagicLink:   emailMagicLink(req.RedirectURI, client.WhitelistURLs, code),
		ExpiresIn:   cfg.GetCodeLifetime().String(),
		ClientID:    clientID,
		RedirectURI: req.RedirectURI,
		UserID:      user.ID,
		UserAlias:   user.Alias,
	}

	subject, body, err := renderSignupMail(cfg.ResetSubject, defaultPasswordResetSubject, cfg.ResetBodyTemplate, defaultPasswordResetBody, mailData)
	if err != nil {
		slog.Error("password reset template render failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	go func() {
		if err := sendEmail(emailCfg, email, subject, body); err != nil {
			slog.Error("password reset mail send failed", slog.String("error", err.Error()))
		}
	}()

	accepted()
}

// APIPasswordResetConfirm sets a new password with a valid reset code.
func (m *Auth) APIPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Signup
	if !cfg.PasswordReset {
		httputil.HandleError(w, httputil.NewError("password reset is disabled", nil, http.StatusForbidden))
		return
	}

	var req SignupRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		httputil.HandleError(w, httputil.NewError("code is required", nil, http.StatusBadRequest))
		return
	}
	if minLen := cfg.GetPasswordMinLength(); len(req.Password) < minLen {
		httputil.HandleError(w, httputil.NewError(fmt.Sprintf("password must be at least %d characters", minLen), nil, http.StatusBadRequest))
		return
	}

	codeHash := hashEmailCode(code)

	flow := resetFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindPasswordReset, codeHash, &flow); err != nil {
		httputil.HandleError(w, httputil.NewError("code not found or expired", nil, http.StatusUnauthorized))
		return
	}

	// single use
	_ = m.store.DeleteFlowCode(r.Context(), flowKindPasswordReset, codeHash)

	if err := m.store.UpdateUserPassword(data.WithContextUserName(r.Context(), "password-reset"), flow.UserID, req.Password); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot update password", err, http.StatusInternalServerError))
		return
	}

	if err := m.cache.Reload(r.Context()); err != nil {
		slog.Error("password reset cache reload failed", slog.String("error", err.Error()))
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "password updated; you can sign in now"},
	})
}

// ////////////////////////////////////////////////////////////////////
// session/login in-process integration

// SignupFeatures reports which self-service flows are currently enabled so
// the login page can show/hide signup and forgot-password live.
func (m *Auth) SignupFeatures() session.SignupFeatures {
	cfg := m.cache.Snapshot().Signup

	return session.SignupFeatures{
		Signup:            cfg.Enabled,
		PasswordReset:     cfg.PasswordReset,
		PasswordMinLength: cfg.GetPasswordMinLength(),
	}
}

// SignupAction runs a signup/password-reset request in-process for login
// middlewares backed by auth_middleware; body is the JSON request payload.
func (m *Auth) SignupAction(ctx context.Context, action string, body []byte) ([]byte, int, error) {
	var handler http.HandlerFunc

	switch action {
	case session.SignupActionSignup:
		handler = m.APISignup
	case session.SignupActionVerify:
		handler = m.APISignupVerify
	case session.SignupActionReset:
		handler = m.APIPasswordReset
	case session.SignupActionResetConfirm:
		handler = m.APIPasswordResetConfirm
	default:
		return nil, 0, fmt.Errorf("unknown signup action %q", action)
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	r.Header.Set("Content-Type", "application/json")

	w := newBufferResponseWriter()
	handler(w, r)

	return w.buf.Bytes(), w.status, nil
}

// bufferResponseWriter captures an in-process handler response.
type bufferResponseWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newBufferResponseWriter() *bufferResponseWriter {
	return &bufferResponseWriter{header: http.Header{}, status: http.StatusOK}
}

func (w *bufferResponseWriter) Header() http.Header { return w.header }

func (w *bufferResponseWriter) WriteHeader(status int) { w.status = status }

func (w *bufferResponseWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
