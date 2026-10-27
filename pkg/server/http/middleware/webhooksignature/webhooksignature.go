// Package webhooksignature verifies HMAC signatures of webhook requests.
package webhooksignature

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // some providers still sign with SHA-1
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type WebhookSignature struct {
	// Preset configures a known provider: github, stripe, slack, shopify,
	// standard (Standard Webhooks, Svix) or generic. Fields set below
	// override the preset.
	Preset string `cfg:"preset"`
	// Secrets verify the signature; any of them may match, for rotation.
	Secrets []string `cfg:"secrets" log:"-"`
	// SecretEncoding of Secrets: raw, hex or base64, default raw.
	SecretEncoding string `cfg:"secret_encoding"`

	// Header holding the signature.
	Header string `cfg:"header"`
	// Algorithm: sha1, sha256 or sha512, default sha256.
	Algorithm string `cfg:"algorithm"`
	// Encoding of the signature: hex or base64, default hex.
	Encoding string `cfg:"encoding"`
	// Prefix before the signature in the header, e.g. "sha256=".
	Prefix string `cfg:"prefix"`

	// TimestampHeader holds the request time, which is signed with the body
	// and checked against Tolerance.
	TimestampHeader string `cfg:"timestamp_header"`
	// Tolerance is the accepted age of the timestamp, default 5m.
	Tolerance time.Duration `cfg:"tolerance"`

	// MaxBodySize is the largest body read, default 1MB.
	MaxBodySize string `cfg:"max_body_size"`
	// StatusCode of rejected requests, default 401.
	StatusCode int `cfg:"status_code"`
}

// verifier extracts candidate signatures and the signed payload.
type verifier struct {
	hash      func() hash.Hash
	secrets   [][]byte
	tolerance time.Duration
	now       func() time.Time
	maxBody   int64

	// signatures returns the signatures sent and the timestamp, if any.
	signatures func(r *http.Request) (sigs [][]byte, ts string, err error)
	// payload builds the signed message.
	payload func(r *http.Request, ts string, body []byte) []byte
}

var errMissing = errors.New("signature missing")

func decode(encoding, v string) ([]byte, error) {
	switch encoding {
	case "hex":
		return hex.DecodeString(v)
	case "base64":
		return base64.StdEncoding.DecodeString(v)
	}

	return nil, fmt.Errorf("unknown encoding %q", encoding)
}

func (m *WebhookSignature) build() (*verifier, error) {
	preset := strings.ToLower(strings.TrimSpace(m.Preset))
	if preset == "" {
		preset = "generic"
	}

	header, algorithm, encoding, prefix, tsHeader := m.Header, m.Algorithm, m.Encoding, m.Prefix, m.TimestampHeader
	secretEncoding := m.SecretEncoding

	def := func(v *string, d string) {
		if *v == "" {
			*v = d
		}
	}

	v := &verifier{now: time.Now}

	switch preset {
	case "github":
		def(&header, "X-Hub-Signature-256")
		def(&prefix, "sha256=")
	case "shopify":
		def(&header, "X-Shopify-Hmac-Sha256")
		def(&encoding, "base64")
	case "slack":
		def(&header, "X-Slack-Signature")
		def(&prefix, "v0=")
		def(&tsHeader, "X-Slack-Request-Timestamp")
	case "stripe":
		def(&header, "Stripe-Signature")
	case "standard":
		def(&header, "Webhook-Signature")
		def(&encoding, "base64")
		def(&secretEncoding, "base64")
	case "generic":
		if header == "" {
			return nil, errors.New("webhook_signature header is required")
		}
	default:
		return nil, fmt.Errorf("webhook_signature preset %q is unknown", m.Preset)
	}

	def(&algorithm, "sha256")
	def(&encoding, "hex")
	def(&secretEncoding, "raw")

	switch strings.ToLower(algorithm) {
	case "sha1":
		v.hash = sha1.New
	case "sha256":
		v.hash = sha256.New
	case "sha512":
		v.hash = sha512.New
	default:
		return nil, fmt.Errorf("webhook_signature algorithm %q is unknown", algorithm)
	}

	encoding = strings.ToLower(encoding)
	if encoding != "hex" && encoding != "base64" {
		return nil, fmt.Errorf("webhook_signature encoding %q is unknown", encoding)
	}

	if len(m.Secrets) == 0 {
		return nil, errors.New("webhook_signature secrets are required")
	}

	for i, s := range m.Secrets {
		if s == "" {
			return nil, fmt.Errorf("webhook_signature secret %d is empty", i)
		}

		var key []byte

		switch strings.ToLower(secretEncoding) {
		case "raw":
			key = []byte(s)
		case "hex", "base64":
			if preset == "standard" {
				s = strings.TrimPrefix(s, "whsec_")
			}

			var err error
			if key, err = decode(strings.ToLower(secretEncoding), s); err != nil {
				return nil, fmt.Errorf("webhook_signature secret %d: %w", i, err)
			}
		default:
			return nil, fmt.Errorf("webhook_signature secret_encoding %q is unknown", secretEncoding)
		}

		v.secrets = append(v.secrets, key)
	}

	v.tolerance = m.Tolerance
	if v.tolerance < 0 {
		return nil, errors.New("webhook_signature tolerance must not be negative")
	}
	if v.tolerance == 0 {
		v.tolerance = 5 * time.Minute
	}

	size, err := humanize.ParseBytes(defaultString(m.MaxBodySize, "1MB"))
	if err != nil || size == 0 || size > 1<<40 {
		return nil, fmt.Errorf("webhook_signature max_body_size %q is invalid", m.MaxBodySize)
	}
	v.maxBody = int64(size)

	switch preset {
	case "stripe":
		// t=<ts>,v1=<sig>,v1=<sig>
		v.signatures = func(r *http.Request) ([][]byte, string, error) {
			var (
				ts   string
				sigs [][]byte
			)

			for _, part := range strings.Split(r.Header.Get(header), ",") {
				k, val, _ := strings.Cut(strings.TrimSpace(part), "=")
				switch k {
				case "t":
					ts = val
				case "v1":
					if b, err := hex.DecodeString(val); err == nil {
						sigs = append(sigs, b)
					}
				}
			}

			if ts == "" || len(sigs) == 0 {
				return nil, "", errMissing
			}

			return sigs, ts, nil
		}
		v.payload = func(_ *http.Request, ts string, body []byte) []byte {
			return join(ts, ".", body)
		}
	case "standard":
		// webhook-id, webhook-timestamp, "v1,<sig> v1,<sig>"
		v.signatures = func(r *http.Request) ([][]byte, string, error) {
			var sigs [][]byte

			for _, part := range strings.Fields(r.Header.Get(header)) {
				version, val, _ := strings.Cut(part, ",")
				if version != "v1" {
					continue
				}

				if b, err := base64.StdEncoding.DecodeString(val); err == nil {
					sigs = append(sigs, b)
				}
			}

			ts := r.Header.Get("Webhook-Timestamp")
			if len(sigs) == 0 || ts == "" || r.Header.Get("Webhook-Id") == "" {
				return nil, "", errMissing
			}

			return sigs, ts, nil
		}
		v.payload = func(r *http.Request, ts string, body []byte) []byte {
			return join(r.Header.Get("Webhook-Id")+"."+ts, ".", body)
		}
	default:
		v.signatures = func(r *http.Request) ([][]byte, string, error) {
			raw := r.Header.Get(header)
			if raw == "" || !strings.HasPrefix(raw, prefix) {
				return nil, "", errMissing
			}

			sig, err := decode(encoding, strings.TrimPrefix(raw, prefix))
			if err != nil {
				return nil, "", errMissing
			}

			var ts string
			if tsHeader != "" {
				if ts = r.Header.Get(tsHeader); ts == "" {
					return nil, "", errMissing
				}
			}

			return [][]byte{sig}, ts, nil
		}

		switch {
		case preset == "slack":
			v.payload = func(_ *http.Request, ts string, body []byte) []byte {
				return join("v0:"+ts, ":", body)
			}
		case tsHeader != "":
			v.payload = func(_ *http.Request, ts string, body []byte) []byte {
				return join(ts, ".", body)
			}
		default:
			v.payload = func(_ *http.Request, _ string, body []byte) []byte { return body }
		}
	}

	return v, nil
}

func defaultString(v, d string) string {
	if v == "" {
		return d
	}

	return v
}

func join(prefix, sep string, body []byte) []byte {
	b := make([]byte, 0, len(prefix)+len(sep)+len(body))
	b = append(b, prefix...)
	b = append(b, sep...)

	return append(b, body...)
}

// checkTimestamp accepts unix seconds within tolerance.
func (v *verifier) checkTimestamp(ts string) error {
	if ts == "" {
		return nil
	}

	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("invalid timestamp")
	}

	if d := v.now().Sub(time.Unix(sec, 0)); d > v.tolerance || d < -v.tolerance {
		return errors.New("timestamp outside tolerance")
	}

	return nil
}

func (v *verifier) verify(r *http.Request, body []byte) error {
	sigs, ts, err := v.signatures(r)
	if err != nil {
		return err
	}

	if err := v.checkTimestamp(ts); err != nil {
		return err
	}

	payload := v.payload(r, ts, body)

	for _, key := range v.secrets {
		mac := hmac.New(v.hash, key)
		mac.Write(payload)
		want := mac.Sum(nil)

		for _, sig := range sigs {
			if hmac.Equal(sig, want) {
				return nil
			}
		}
	}

	return errors.New("signature mismatch")
}

func (m *WebhookSignature) Middleware() (func(http.Handler) http.Handler, error) {
	v, err := m.build()
	if err != nil {
		return nil, err
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusUnauthorized
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body []byte

			if r.Body != nil && r.Body != http.NoBody {
				b, err := io.ReadAll(io.LimitReader(r.Body, v.maxBody+1))
				_ = r.Body.Close()

				if err != nil {
					httputil.HandleError(w, httputil.NewError("read body", nil, http.StatusBadRequest))

					return
				}

				if int64(len(b)) > v.maxBody {
					httputil.HandleError(w, httputil.NewError("request body too large", nil, http.StatusRequestEntityTooLarge))

					return
				}

				body = b
			}

			if err := v.verify(r, body); err != nil {
				httputil.HandleError(w, httputil.NewError("invalid webhook signature", err, status))

				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.TransferEncoding = nil
			r.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
