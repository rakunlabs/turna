package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	httputil2 "github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

// ///////////////////////////////
// BASED ON ECHO'S PROXY MIDDLEWARE
// ///////////////////////////////

// ProxyConfig defines the config for Proxy middleware.
type ProxyConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Balancer defines a load balancing technique.
	// Required.
	Balancer ProxyBalancer

	// RetryCount defines the number of times a failed proxied request should be retried
	// using the next available ProxyTarget. Defaults to 0, meaning requests are never retried.
	RetryCount int

	// RetryFilter defines a function used to determine if a failed request to a
	// ProxyTarget should be retried. The RetryFilter will only be called when the number
	// of previous retries is less than RetryCount. If the function returns true, the
	// request will be retried. The provided error indicates the reason for the request
	// failure. When the ProxyTarget is unavailable, the error will be an instance of
	// echo.HTTPError with a Code of http.StatusBadGateway. In all other cases, the error
	// will indicate an internal error in the Proxy middleware. When a RetryFilter is not
	// specified, all requests that fail with http.StatusBadGateway will be retried. A custom
	// RetryFilter can be provided to only retry specific requests. Note that RetryFilter is
	// only called when the request to the target fails, or an internal error in the Proxy
	// middleware has occurred. Successful requests that return a non-200 response code cannot
	// be retried.
	RetryFilter func(w http.ResponseWriter, r *http.Request, e error) bool

	// ErrorHandler defines a function which can be used to return custom errors from
	// the Proxy middleware. ErrorHandler is only invoked when there has been
	// either an internal error in the Proxy middleware or the ProxyTarget is
	// unavailable. Due to the way requests are proxied, ErrorHandler is not invoked
	// when a ProxyTarget returns a non-200 response. In these cases, the response
	// is already written so errors cannot be modified. ErrorHandler is only
	// invoked after all retry attempts have been exhausted.
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error) error

	// Rewrite defines URL path rewrite rules. The values captured in asterisk can be
	// retrieved by index e.g. $1, $2 and so on.
	// Examples:
	// "/old":              "/new",
	// "/api/*":            "/$1",
	// "/js/*":             "/public/javascripts/$1",
	// "/users/*/orders/*": "/user/$1/order/$2",
	Rewrite map[string]string

	// RegexRewrite defines rewrite rules using regexp.Rexexp with captures
	// Every capture group in the values can be retrieved by index e.g. $1, $2 and so on.
	// Example:
	// "^/old/[0.9]+/":     "/new",
	// "^/api/.+?/(.*)":    "/v2/$1",
	RegexRewrite map[*regexp.Regexp]string

	// Context key to store selected ProxyTarget into context.
	// Optional. Default value "target".
	ContextKey string

	// To customize the transport to remote.
	// Examples: If custom TLS certificates are required.
	Transport http.RoundTripper

	// ModifyResponse defines function to modify response from ProxyTarget.
	ModifyResponse func(*http.Response) error
}

var (
	// DefaultProxyConfig is the default Proxy middleware config.
	DefaultProxyConfig = ProxyConfig{
		Skipper:    DefaultSkipper,
		ContextKey: "target",
	}
)

func proxyRaw(t *ProxyTarget, errHolder *httputil2.Error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			errHolder.Err = fmt.Errorf("proxy raw, hijack error=%w, url=%s", err, t.URL)
			return
		}
		defer in.Close()

		out, err := net.Dial("tcp", t.URL.Host)
		if err != nil {
			errHolder.Err = fmt.Errorf("proxy raw, dial error=%w, url=%s", err, t.URL)
			errHolder.Code = http.StatusBadGateway
			return
		}
		defer out.Close()

		// Write header
		err = r.Write(out)
		if err != nil {
			errHolder.Err = fmt.Errorf("proxy raw, request header copy error=%w, url=%s", err, t.URL)
			errHolder.Code = http.StatusBadGateway
			return
		}

		errCh := make(chan error, 2)
		cp := func(dst io.Writer, src io.Reader) {
			_, err = io.Copy(dst, src)
			errCh <- err
		}

		go cp(out, in)
		go cp(in, out)
		err = <-errCh
		if err != nil && err != io.EOF {
			errHolder.Err = fmt.Errorf("proxy raw, copy error=%w, url=%s", err, t.URL)
			errHolder.Code = http.StatusBadGateway
		}
	})
}

// Proxy returns a Proxy middleware.
//
// Proxy middleware forwards the request to upstream server using a configured load balancing technique.
func Proxy(balancer ProxyBalancer) func(http.Handler) http.Handler {
	c := DefaultProxyConfig
	c.Balancer = balancer
	return ProxyWithConfig(c)
}

// ProxyWithConfig returns a Proxy middleware with config.
// See: `Proxy()`
func ProxyWithConfig(config ProxyConfig) func(http.Handler) http.Handler {
	if config.Balancer == nil {
		panic("echo: proxy middleware requires balancer")
	}
	// Defaults
	if config.Skipper == nil {
		config.Skipper = DefaultProxyConfig.Skipper
	}
	if config.RetryFilter == nil {
		config.RetryFilter = func(w http.ResponseWriter, r *http.Request, e error) bool {
			if httpErr, ok := e.(*httputil2.Error); ok {
				return httpErr.Code == http.StatusBadGateway
			}
			// if httpErr, ok := e.(*echo.HTTPError); ok {
			// return httpErr.Code == http.StatusBadGateway
			// }
			return false
		}
	}
	if config.ErrorHandler == nil {
		config.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) error {
			return err
		}
	}
	if config.Rewrite != nil {
		if config.RegexRewrite == nil {
			config.RegexRewrite = make(map[*regexp.Regexp]string)
		}
		for k, v := range rewriteRulesRegex(config.Rewrite) {
			config.RegexRewrite[k] = v
		}
	}

	provider, isTargetProvider := config.Balancer.(TargetProvider)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if config.Skipper(w, r) {
				next.ServeHTTP(w, r)

				return
			}

			if err := rewriteURL(config.RegexRewrite, r); err != nil {
				httputil2.HandleError(w, httputil2.NewErrorAs(config.ErrorHandler(w, r, err)))
				return
			}

			// Fix header
			// Basically it's not good practice to unconditionally pass incoming x-real-ip header to upstream.
			// However, for backward compatibility, legacy behavior is preserved unless you configure Echo#IPExtractor.
			// if r.Header.Get(echo.HeaderXRealIP) == "" || c.Echo().IPExtractor != nil {
			if r.Header.Get(echo.HeaderXRealIP) == "" {
				r.Header.Set(echo.HeaderXRealIP, httputil2.RealIP(r))
			}
			if r.Header.Get(echo.HeaderXForwardedProto) == "" {
				r.Header.Set(echo.HeaderXForwardedProto, httputil2.Scheme(r))
			}
			if httputil2.IsWebSocket(r) && r.Header.Get(echo.HeaderXForwardedFor) == "" { // For HTTP, it is automatically set by Go HTTP reverse proxy.
				r.Header.Set(echo.HeaderXForwardedFor, httputil2.RealIP(r))
			}

			retries := config.RetryCount
			errHolder := httputil2.Error{}

			for {
				var tgt *ProxyTarget
				var err error
				if isTargetProvider {
					tgt, err = provider.NextTarget(w, r)
					if err != nil {
						httputil2.HandleError(w, httputil2.NewErrorAs(config.ErrorHandler(w, r, err)))
						return
					}
				} else {
					tgt = config.Balancer.Next(w, r)
				}

				tcontext.Set(r, config.ContextKey, tgt)
				// c.Set(config.ContextKey, tgt)

				//If retrying a failed request, clear any previous errors from
				//context here so that balancers have the option to check for
				//errors that occurred using previous target
				if retries < config.RetryCount {
					errHolder = httputil2.Error{}
				}

				// This is needed for ProxyConfig.ModifyResponse and/or ProxyConfig.Transport to be able to process the Request
				// that Balancer may have replaced with c.SetRequest.

				// Proxy
				switch {
				case httputil2.IsWebSocket(r):
					proxyRaw(tgt, &errHolder).ServeHTTP(w, r)
				case r.Header.Get(echo.HeaderAccept) == "text/event-stream":
				default:
					proxyHTTP(tgt, &errHolder, config).ServeHTTP(w, r)
				}

				if errHolder.Err == nil {
					return
				}

				retry := retries > 0 && config.RetryFilter(w, r, &errHolder)
				if !retry {
					httputil2.HandleError(w, httputil2.NewErrorAs(config.ErrorHandler(w, r, &errHolder)))
					return
				}

				retries--
			}
		})
	}
}

// StatusCodeContextCanceled is a custom HTTP status code for situations
// where a client unexpectedly closed the connection to the server.
// As there is no standard error code for "client closed connection", but
// various well-known HTTP clients and server implement this HTTP code we use
// 499 too instead of the more problematic 5xx, which does not allow to detect this situation
const StatusCodeContextCanceled = 499

func proxyHTTP(tgt *ProxyTarget, errHolder *httputil2.Error, config ProxyConfig) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(tgt.URL)
	proxy.ErrorHandler = func(resp http.ResponseWriter, req *http.Request, err error) {
		desc := tgt.URL.String()
		if tgt.Name != "" {
			desc = fmt.Sprintf("%s(%s)", tgt.Name, tgt.URL.String())
		}
		// If the client canceled the request (usually by closing the connection), we can report a
		// client error (4xx) instead of a server error (5xx) to correctly identify the situation.
		// The Go standard library (at of late 2020) wraps the exported, standard
		// context.Canceled error with unexported garbage value requiring a substring check, see
		// https://github.com/golang/go/blob/6965b01ea248cabb70c3749fd218b36089a21efb/src/net/net.go#L416-L430
		if err == context.Canceled || strings.Contains(err.Error(), "operation was canceled") {
			errHolder.Err = fmt.Errorf("client closed connection: %v", err)
			errHolder.Code = StatusCodeContextCanceled
		} else {
			errHolder.Err = fmt.Errorf("remote %s unreachable, could not forward: %v", desc, err)
			errHolder.Code = http.StatusBadGateway
		}
	}
	proxy.Transport = config.Transport
	proxy.ModifyResponse = config.ModifyResponse
	return proxy
}

func rewriteURL(rewriteRegex map[*regexp.Regexp]string, req *http.Request) error {
	if len(rewriteRegex) == 0 {
		return nil
	}

	// Depending how HTTP request is sent RequestURI could contain Scheme://Host/path or be just /path.
	// We only want to use path part for rewriting and therefore trim prefix if it exists
	rawURI := req.RequestURI
	if rawURI != "" && rawURI[0] != '/' {
		prefix := ""
		if req.URL.Scheme != "" {
			prefix = req.URL.Scheme + "://"
		}
		if req.URL.Host != "" {
			prefix += req.URL.Host // host or host:port
		}
		if prefix != "" {
			rawURI = strings.TrimPrefix(rawURI, prefix)
		}
	}

	for k, v := range rewriteRegex {
		if replacer := captureTokens(k, rawURI); replacer != nil {
			url, err := req.URL.Parse(replacer.Replace(v))
			if err != nil {
				return err
			}
			req.URL = url

			return nil // rewrite only once
		}
	}
	return nil
}

func captureTokens(pattern *regexp.Regexp, input string) *strings.Replacer {
	groups := pattern.FindAllStringSubmatch(input, -1)
	if groups == nil {
		return nil
	}
	values := groups[0][1:]
	replace := make([]string, 2*len(values))
	for i, v := range values {
		j := 2 * i
		replace[j] = "$" + strconv.Itoa(i+1)
		replace[j+1] = v
	}
	return strings.NewReplacer(replace...)
}

func rewriteRulesRegex(rewrite map[string]string) map[*regexp.Regexp]string {
	// Initialize
	rulesRegex := map[*regexp.Regexp]string{}
	for k, v := range rewrite {
		k = regexp.QuoteMeta(k)
		k = strings.ReplaceAll(k, `\*`, "(.*?)")
		if strings.HasPrefix(k, `\^`) {
			k = strings.ReplaceAll(k, `\^`, "^")
		}
		k = k + "$"
		rulesRegex[regexp.MustCompile(k)] = v
	}
	return rulesRegex
}
