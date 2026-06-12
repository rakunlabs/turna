package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"text/template"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/xhit/go-str2duration/v2"
)

const grantTypeEmailCode = "email_code"

// emailFlow is the payload stored in auth_flow_codes for an email login.
type emailFlow struct {
	Alias    string   `json:"alias"`
	ClientID string   `json:"client_id"`
	Scope    []string `json:"scope"`
}

type EmailTemplateData struct {
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	MagicLink   string   `json:"magic_link"`
	ExpiresIn   string   `json:"expires_in"`
	ClientID    string   `json:"client_id"`
	RedirectURI string   `json:"redirect_uri"`
	UserID      string   `json:"user_id"`
	UserAlias   []string `json:"user_alias"`
}

type emailMessage struct {
	Subject   string            `json:"subject"`
	Body      string            `json:"body"`
	MagicLink string            `json:"magic_link,omitempty"`
	Data      EmailTemplateData `json:"data"`
}

type emailPreviewRequest struct {
	Settings    EmailSettings `json:"settings"`
	Email       string        `json:"email"`
	Code        string        `json:"code"`
	ClientID    string        `json:"client_id"`
	RedirectURI string        `json:"redirect_uri"`
}

const defaultEmailSubjectTemplate = "Your login code"

const defaultEmailMagicLinkSubjectTemplate = "Your login link"

const defaultEmailCodeBodyTemplate = "Your one-time login code is:\r\n\r\n{{.Code}}\r\n\r\nThe code expires in {{.ExpiresIn}}."

const defaultEmailMagicLinkBodyTemplate = "Click the link to sign in:\r\n\r\n{{.MagicLink}}\r\n\r\nOr use this one-time code: {{.Code}}\r\n\r\nThe link expires in {{.ExpiresIn}}."

func hashEmailCode(code string) string {
	sum := sha256.Sum256([]byte(code))

	return hex.EncodeToString(sum[:])
}

func renderEmailTemplate(name, source string, data EmailTemplateData) (string, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}

	return out.String(), nil
}

func cleanEmailSubject(subject string) string {
	parts := strings.FieldsFunc(subject, func(r rune) bool {
		return r == '\r' || r == '\n'
	})

	return strings.TrimSpace(strings.Join(parts, " "))
}

// emailTemplatePair returns the subject/body templates and their defaults for
// the relevant flow: the magic-link mail when a link is present, otherwise the
// one-time code mail. The two flows are configured independently.
func emailTemplatePair(cfg EmailSettings, magic bool) (subjectTpl, bodyTpl, defSubject, defBody string) {
	if magic {
		return cfg.MagicLinkSubject, cfg.MagicLinkBodyTemplate,
			defaultEmailMagicLinkSubjectTemplate, defaultEmailMagicLinkBodyTemplate
	}

	return cfg.Subject, cfg.BodyTemplate,
		defaultEmailSubjectTemplate, defaultEmailCodeBodyTemplate
}

func renderEmailSubject(cfg EmailSettings, data EmailTemplateData) (string, error) {
	subjectTemplate, _, defSubject, _ := emailTemplatePair(cfg, data.MagicLink != "")
	if strings.TrimSpace(subjectTemplate) == "" {
		subjectTemplate = defSubject
	}

	subject, err := renderEmailTemplate("email_subject", subjectTemplate, data)
	if err != nil {
		return "", err
	}

	subject = cleanEmailSubject(subject)
	if subject == "" {
		subject = defSubject
	}

	return subject, nil
}

func renderEmailBody(cfg EmailSettings, data EmailTemplateData) (string, error) {
	_, bodyTemplate, _, defBody := emailTemplatePair(cfg, data.MagicLink != "")
	if strings.TrimSpace(bodyTemplate) == "" {
		bodyTemplate = defBody
	}

	return renderEmailTemplate("email_body", bodyTemplate, data)
}

func emailMagicLink(redirectURI string, whitelist []string, code string) string {
	if redirectURI == "" || !redirectAllowed(redirectURI, whitelist) {
		return ""
	}

	linkURL, err := url.Parse(redirectURI)
	if err != nil {
		return ""
	}

	q := linkURL.Query()
	q.Set("code", code)
	linkURL.RawQuery = q.Encode()

	return linkURL.String()
}

func buildEmailMessage(cfg EmailSettings, to, code, clientID, redirectURI string, whitelist []string, user *data.UserExtended) (emailMessage, error) {
	// magic link is an independent feature; only generate it when enabled.
	magicLink := ""
	if cfg.GetMagicLink() {
		magicLink = emailMagicLink(redirectURI, whitelist, code)
	}

	data := EmailTemplateData{
		Email:       to,
		Code:        code,
		MagicLink:   magicLink,
		ExpiresIn:   cfg.GetCodeLifetime().String(),
		ClientID:    clientID,
		RedirectURI: redirectURI,
	}
	if user != nil {
		data.UserID = user.ID
		data.UserAlias = user.Alias
	}

	subject, err := renderEmailSubject(cfg, data)
	if err != nil {
		return emailMessage{}, fmt.Errorf("render email subject: %w", err)
	}
	body, err := renderEmailBody(cfg, data)
	if err != nil {
		return emailMessage{}, fmt.Errorf("render email body: %w", err)
	}

	return emailMessage{Subject: subject, Body: body, MagicLink: data.MagicLink, Data: data}, nil
}

func validateEmailTemplates(cfg EmailSettings) error {
	base := EmailTemplateData{
		Email:       "user@example.com",
		Code:        "123456",
		ExpiresIn:   cfg.GetCodeLifetime().String(),
		ClientID:    "client",
		RedirectURI: "https://app.example.com/callback",
		UserID:      "user-id",
		UserAlias:   []string{"user@example.com"},
	}

	// validate the one-time code mail
	codeData := base
	if _, err := renderEmailSubject(cfg, codeData); err != nil {
		return fmt.Errorf("code mail: %w", err)
	}
	if _, err := renderEmailBody(cfg, codeData); err != nil {
		return fmt.Errorf("code mail: %w", err)
	}

	// validate the magic-link mail
	magicData := base
	magicData.MagicLink = "https://app.example.com/callback?code=123456"
	if _, err := renderEmailSubject(cfg, magicData); err != nil {
		return fmt.Errorf("magic link mail: %w", err)
	}
	if _, err := renderEmailBody(cfg, magicData); err != nil {
		return fmt.Errorf("magic link mail: %w", err)
	}

	return nil
}

func parseEmailSettingDurations(cfg *EmailSettings) error {
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

// sendEmail delivers a plain-text mail through the configured SMTP relay.
func sendEmail(cfg EmailSettings, to, subject, body string) error {
	if cfg.SMTP.Host == "" {
		return fmt.Errorf("smtp host is not configured")
	}

	addr := net.JoinHostPort(cfg.SMTP.Host, fmt.Sprint(cfg.SMTP.GetPort()))

	from := cfg.From
	if from == "" {
		from = cfg.SMTP.Username
	}
	if from == "" {
		return fmt.Errorf("email from address is not configured")
	}

	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	tlsConfig := &tls.Config{
		ServerName:         cfg.SMTP.Host,
		InsecureSkipVerify: cfg.SMTP.InsecureSkipVerify, //nolint:gosec // operator opt-in
	}

	var client *smtp.Client

	if cfg.SMTP.TLS {
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("smtp tls dial: %w", err)
		}

		client, err = smtp.NewClient(conn, cfg.SMTP.Host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
	} else {
		var err error
		client, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}

		if cfg.SMTP.StartTLS {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}
	defer client.Close()

	if cfg.SMTP.Username != "" && !cfg.SMTP.NoAuth {
		auth := smtp.PlainAuth("", cfg.SMTP.Username, cfg.SMTP.Password, cfg.SMTP.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	return client.Quit()
}

// redirectAllowed checks the redirect uri against client whitelist prefixes.
func redirectAllowed(redirectURI string, whitelist []string) bool {
	if redirectURI == "" {
		return false
	}

	if len(whitelist) == 0 {
		return true
	}

	for _, prefix := range whitelist {
		if prefix != "" && strings.HasPrefix(redirectURI, prefix) {
			return true
		}
	}

	return false
}

// APIEmailCode sends a one-time login code (and magic link) to the user's
// email. The response is always 200 to avoid account enumeration.
func (m *Auth) APIEmailCode(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Email
	if cfg.Disabled || cfg.SMTP.Host == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "email login is disabled",
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
	var emailClient *AccessClient
	if clientID != "" {
		emailClient, _ = m.resolveClient(clientID, clientSecret)
	}
	if emailClient == nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client not valid",
			code:             http.StatusUnauthorized,
		})

		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Username))
	if email == "" || !strings.Contains(email, "@") {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "username (email) is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	accepted := func() {
		httputil.JSON(w, http.StatusOK, map[string]any{
			"message": "if the account exists, an email was sent",
		})
	}

	user, err := m.cache.GetUser(data.GetUserRequest{Alias: email})
	if err != nil || user.Disabled {
		// do not leak account existence
		accepted()

		return
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		accepted()

		return
	}
	code := hex.EncodeToString(raw)

	flow := emailFlow{
		Alias:    email,
		ClientID: clientID,
		Scope:    splitFields(req.Scope),
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindEmail, hashEmailCode(code), flow, cfg.GetCodeLifetime()); err != nil {
		slog.Error("email login code store failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	message, err := buildEmailMessage(cfg, email, code, clientID, req.RedirectURI, emailClient.WhitelistURLs, user)
	if err != nil {
		slog.Error("email login template render failed", slog.String("error", err.Error()))
		accepted()

		return
	}

	go func() {
		if err := sendEmail(cfg, email, message.Subject, message.Body); err != nil {
			slog.Error("email login send failed", slog.String("error", err.Error()))
		}
	}()

	accepted()
}

func (m *Auth) EmailPreviewAPI(w http.ResponseWriter, r *http.Request) {
	var req emailPreviewRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode email preview", err, http.StatusBadRequest))
		return
	}

	if err := parseEmailSettingDurations(&req.Settings); err != nil {
		httputil.HandleError(w, httputil.NewError("invalid code_lifetime", err, http.StatusBadRequest))
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = "user@example.com"
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		code = "123456"
	}

	message, err := buildEmailMessage(req.Settings, email, code, req.ClientID, req.RedirectURI, nil, nil)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot render email preview", err, http.StatusBadRequest))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[emailMessage]{Payload: message})
}

// emailCodeGrant handles grant_type=email_code at the token endpoint.
func (m *Auth) emailCodeGrant(w http.ResponseWriter, r *http.Request, req AccessTokenRequest, clientID, clientSecret string) {
	cfg := m.cache.Snapshot().Email
	if cfg.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "email login is disabled",
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

	if req.Code == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "code is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	codeHash := hashEmailCode(req.Code)

	flow := emailFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindEmail, codeHash, &flow); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "code not found or expired",
			code:             http.StatusUnauthorized,
		})

		return
	}

	// single use
	_ = m.store.DeleteFlowCode(r.Context(), flowKindEmail, codeHash)

	if flow.ClientID != clientID {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "client mismatch",
			code:             http.StatusBadRequest,
		})

		return
	}

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:         flow.Alias,
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

	m.writeToken(w, r, user, clientID, flow.Scope, nil)
}
