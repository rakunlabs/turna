// Package waf inspects requests and responses with the Coraza web
// application firewall, optionally with the embedded OWASP Core Rule Set.
package waf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/corazawaf/coraza/v3"
	corazahttp "github.com/corazawaf/coraza/v3/http"
	"github.com/corazawaf/coraza/v3/types"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const (
	defaultRequestBodyLimit         = 13107200
	defaultInboundAnomalyThreshold  = 5
	defaultOutboundAnomalyThreshold = 4
)

type WAF struct {
	// Mode is on, detection_only or off, default is on.
	Mode string `cfg:"mode"`
	// CRS loads the embedded OWASP Core Rule Set.
	CRS bool `cfg:"crs"`
	// ParanoiaLevel of the CRS, 1 to 4, default is 1.
	ParanoiaLevel int `cfg:"paranoia_level"`
	// InboundAnomalyThreshold is the CRS request score that blocks, default is 5.
	InboundAnomalyThreshold int `cfg:"inbound_anomaly_threshold"`
	// OutboundAnomalyThreshold is the CRS response score that blocks, default is 4.
	OutboundAnomalyThreshold int `cfg:"outbound_anomaly_threshold"`
	// DirectivesFiles are SecLang files loaded after the CRS, globs allowed.
	DirectivesFiles []string `cfg:"directives_files"`
	// Directives are inline SecLang directives loaded last.
	Directives string `cfg:"directives"`
	// RequestBodyAccess inspects request bodies, default is true.
	RequestBodyAccess *bool `cfg:"request_body_access"`
	// RequestBodyLimit in bytes, default is 13107200.
	RequestBodyLimit int `cfg:"request_body_limit"`
	// RequestBodyInMemoryLimit in bytes, larger bodies are buffered to disk.
	RequestBodyInMemoryLimit int `cfg:"request_body_in_memory_limit"`
	// ResponseBodyAccess inspects response bodies, default is false.
	ResponseBodyAccess bool `cfg:"response_body_access"`
	// ResponseBodyMimeTypes are the response types inspected.
	ResponseBodyMimeTypes []string `cfg:"response_body_mime_types"`
	// ResponseBodyLimit in bytes.
	ResponseBodyLimit int `cfg:"response_body_limit"`
	// Log matched rules, default is true.
	Log *bool `cfg:"log"`
}

type requestIDKey struct{}

func (m *WAF) Middleware(_ context.Context) (func(http.Handler) http.Handler, error) {
	engine, err := m.engine()
	if err != nil {
		return nil, err
	}

	if engine == "Off" {
		return func(next http.Handler) http.Handler { return next }, nil
	}

	directives, err := m.directives(engine)
	if err != nil {
		return nil, err
	}

	cfg := coraza.NewWAFConfig().WithRootFS(rootFS{})
	cfg = cfg.WithDirectives(directives.before)
	for _, file := range directives.files {
		cfg = cfg.WithDirectivesFromFile(file)
	}
	if strings.TrimSpace(m.Directives) != "" {
		cfg = cfg.WithDirectives(m.Directives)
	}

	if m.Log == nil || *m.Log {
		cfg = cfg.WithErrorCallback(logMatch)
	}

	waf, err := coraza.NewWAF(cfg)
	if err != nil {
		return nil, fmt.Errorf("waf: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get(httputil.HeaderXRequestID); id != "" {
				r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
			}

			ew := &errWriter{ResponseWriter: w}

			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ew.reached = true
				next.ServeHTTP(w, r)
			})

			// coraza only wraps the hijacker it can see on the writer
			var ww http.ResponseWriter = ew
			if hj, ok := w.(http.Hijacker); ok {
				ww = struct {
					*errWriter
					http.Hijacker
				}{ew, hj}
			}

			corazahttp.WrapHandler(waf, inner).ServeHTTP(ww, r)

			// coraza returns without a response when the body cannot be read
			if !ew.reached && !ew.blocked {
				httputil.HandleError(w, httputil.NewError("cannot process request", nil, http.StatusBadRequest))
			}
		})
	}, nil
}

func (m *WAF) engine() (string, error) {
	switch strings.ToLower(strings.TrimSpace(m.Mode)) {
	case "", "on":
		return "On", nil
	case "detection_only", "detectiononly":
		return "DetectionOnly", nil
	case "off":
		return "Off", nil
	default:
		return "", fmt.Errorf("waf mode %q is unknown, expected on, detection_only or off", m.Mode)
	}
}

type directiveSet struct {
	before string
	files  []string
}

func (m *WAF) directives(engine string) (directiveSet, error) {
	paranoia := m.ParanoiaLevel
	if paranoia == 0 {
		paranoia = 1
	}
	if paranoia < 1 || paranoia > 4 {
		return directiveSet{}, fmt.Errorf("waf paranoia_level %d is out of range, expected 1 to 4", m.ParanoiaLevel)
	}

	inbound := m.InboundAnomalyThreshold
	if inbound == 0 {
		inbound = defaultInboundAnomalyThreshold
	}
	outbound := m.OutboundAnomalyThreshold
	if outbound == 0 {
		outbound = defaultOutboundAnomalyThreshold
	}
	if inbound < 0 || outbound < 0 {
		return directiveSet{}, errors.New("waf anomaly thresholds must be greater than 0")
	}

	requestBodyLimit := m.RequestBodyLimit
	if requestBodyLimit == 0 {
		requestBodyLimit = defaultRequestBodyLimit
	}
	if requestBodyLimit < 0 || m.RequestBodyInMemoryLimit < 0 || m.ResponseBodyLimit < 0 {
		return directiveSet{}, errors.New("waf body limits must be greater than 0")
	}

	var b strings.Builder
	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}

	if m.CRS {
		// sets DetectionOnly, so the engine is set after it
		line("Include @coraza.conf-recommended")
	}

	line("SecRuleEngine " + engine)
	line("SecAuditEngine Off")
	line("SecRequestBodyAccess " + onOff(m.RequestBodyAccess == nil || *m.RequestBodyAccess))
	line("SecRequestBodyLimit " + strconv.Itoa(requestBodyLimit))
	if m.RequestBodyInMemoryLimit > 0 {
		line("SecRequestBodyInMemoryLimit " + strconv.Itoa(m.RequestBodyInMemoryLimit))
	}
	line("SecResponseBodyAccess " + onOff(m.ResponseBodyAccess))
	if len(m.ResponseBodyMimeTypes) > 0 {
		line("SecResponseBodyMimeType " + strings.Join(m.ResponseBodyMimeTypes, " "))
	}
	if m.ResponseBodyLimit > 0 {
		line("SecResponseBodyLimit " + strconv.Itoa(m.ResponseBodyLimit))
	}

	if m.CRS {
		line("Include @crs-setup.conf.example")
		line(fmt.Sprintf(`SecAction "id:900000,phase:1,pass,t:none,nolog,setvar:tx.blocking_paranoia_level=%d,setvar:tx.detection_paranoia_level=%d"`, paranoia, paranoia))
		line(fmt.Sprintf(`SecAction "id:900110,phase:1,pass,t:none,nolog,setvar:tx.inbound_anomaly_score_threshold=%d,setvar:tx.outbound_anomaly_score_threshold=%d"`, inbound, outbound))
		line("Include @owasp_crs/*.conf")
	}

	set := directiveSet{before: b.String()}

	for _, pattern := range m.DirectivesFiles {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return directiveSet{}, fmt.Errorf("waf directives_files %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return directiveSet{}, fmt.Errorf("waf directives_files %q matches no file", pattern)
		}

		set.files = append(set.files, matches...)
	}

	return set, nil
}

func onOff(v bool) string {
	if v {
		return "On"
	}

	return "Off"
}

func logMatch(mr types.MatchedRule) {
	ctx := context.Background()
	if c, ok := mr.(interface{ Context() context.Context }); ok && c.Context() != nil {
		ctx = c.Context()
	}

	args := []any{
		"rule_id", mr.Rule().ID(),
		"msg", mr.Message(),
		"data", mr.Data(),
		"disruptive", mr.Disruptive(),
		"client_ip", mr.ClientIPAddress(),
		"uri", mr.URI(),
	}
	if s := mr.Rule().Severity(); s != types.RuleSeverityUnset {
		args = append(args, "severity", s.String())
	}
	if id, _ := ctx.Value(requestIDKey{}).(string); id != "" {
		args = append(args, "request_id", id)
	}

	slog.Log(ctx, severityLevel(mr.Rule().Severity()), "waf rule matched", args...)
}

func severityLevel(s types.RuleSeverity) slog.Level {
	switch {
	case s == types.RuleSeverityUnset:
		return slog.LevelInfo
	case s <= types.RuleSeverityError:
		return slog.LevelError
	case s == types.RuleSeverityWarning:
		return slog.LevelWarn
	case s == types.RuleSeverityDebug:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

// errWriter turns the bare status coraza writes for a blocked request into a
// JSON error. Once the next handler runs it passes everything through.
type errWriter struct {
	http.ResponseWriter

	reached bool
	blocked bool
}

func (w *errWriter) WriteHeader(code int) {
	if w.reached {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	if w.blocked {
		return
	}
	w.blocked = true

	// drop and redirect interruptions come without an error status
	if code < http.StatusBadRequest {
		code = http.StatusForbidden
	}

	httputil.HandleError(w.ResponseWriter, httputil.NewError("request blocked by waf", nil, code))
}

func (w *errWriter) Write(b []byte) (int, error) {
	if w.blocked {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *errWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *errWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
