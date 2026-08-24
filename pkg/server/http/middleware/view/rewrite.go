package view

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// DefaultRewriteMaxBodySize is the biggest response body that is buffered for rewriting.
var DefaultRewriteMaxBodySize int64 = 10 << 20 // 10MiB

// DefaultRewriteContentTypes are the media types rewritten when not configured.
var DefaultRewriteContentTypes = []string{
	"text/html",
	"application/xhtml+xml",
	"text/css",
}

// Rewrite adapts a proxied page so it keeps working under the page prefix.
//
// A page is served under /{prefix_path}/page/{path} but the backend usually
// assumes it lives at the root. Every option below fixes one class of that
// mismatch and all of them are opt-in.
type Rewrite struct {
	// Base injects <base href="{prefix}/"> into the head of html responses so
	// document relative references resolve under the page prefix.
	Base bool `cfg:"base"`
	// Absolute rewrites root absolute references (href="/x", url(/x)) to the
	// page prefix. Protocol relative (//host/x) references are left alone.
	Absolute bool `cfg:"absolute"`
	// Origin replaces the backend origin (scheme://host, //host and the json
	// escaped scheme:\/\/host form) with the page prefix.
	Origin bool `cfg:"origin"`
	// Location rewrites the Location response header back into the page prefix.
	Location bool `cfg:"location"`
	// Cookie rewrites the Path attribute of Set-Cookie into the page prefix and
	// drops the Domain attribute.
	Cookie bool `cfg:"cookie"`
	// Frame drops the response headers that stop the page from being embedded
	// in an iframe (X-Frame-Options and the CSP frame-ancestors directive).
	Frame bool `cfg:"frame"`
	// ForwardPrefix sends the page prefix as X-Forwarded-Prefix to the backend.
	ForwardPrefix bool `cfg:"forward_prefix"`

	// ContentTypes limits body rewriting to these media types.
	//  - Default is text/html, application/xhtml+xml and text/css.
	ContentTypes []string `cfg:"content_types"`
	// MaxBodySize is the biggest body that gets buffered, bigger ones are
	// streamed untouched. Default is 10MiB.
	MaxBodySize int64 `cfg:"max_body_size"`

	// Replace is applied after the built-in rules.
	Replace []Replace `cfg:"replace"`
}

// Replace is a custom body replacement.
type Replace struct {
	// Regex to match, Old is ignored when set.
	Regex string `cfg:"regex"`
	// Old is the literal value to replace.
	Old string `cfg:"old"`
	// New is the replacement, $1 style expansion works with Regex.
	//  - {{prefix}} is replaced with the page prefix.
	//  - {{url}} is replaced with the backend url.
	New string `cfg:"new"`
	// ContentTypes limits this replacement, defaults to the rewrite ones.
	ContentTypes []string `cfg:"content_types"`
}

type compiledReplace struct {
	regex *regexp.Regexp
	old   []byte
	new   []byte
	types []string
}

type replacement struct {
	old []byte
	new []byte
}

type rewriter struct {
	cfg *Rewrite

	// prefix is the public path of the page without a trailing slash.
	prefix string
	target *url.URL

	types       []string
	maxBodySize int64

	baseTag  []byte
	origins  []replacement
	replaces []compiledReplace
}

var (
	// attribute values, one regex per quote style because RE2 has no backreferences.
	reAttrDouble = regexp.MustCompile(`(?i)(\s(?:href|src|action|formaction|poster|data|manifest|ping|background|longdesc)\s*=\s*")/([^/"][^"]*)?"`)
	reAttrSingle = regexp.MustCompile(`(?i)(\s(?:href|src|action|formaction|poster|data|manifest|ping|background|longdesc)\s*=\s*')/([^/'][^']*)?'`)

	reSrcsetDouble = regexp.MustCompile(`(?i)(\s(?:srcset|imagesrcset)\s*=\s*")([^"]*)"`)
	reSrcsetSingle = regexp.MustCompile(`(?i)(\s(?:srcset|imagesrcset)\s*=\s*')([^']*)'`)

	reCSSURLDouble = regexp.MustCompile(`(?i)url\(\s*"/([^/"][^"]*)?"\s*\)`)
	reCSSURLSingle = regexp.MustCompile(`(?i)url\(\s*'/([^/'][^']*)?'\s*\)`)
	reCSSURLBare   = regexp.MustCompile(`(?i)url\(\s*/([^/"'()\s][^)\s]*)?\s*\)`)

	reCSSImportDouble = regexp.MustCompile(`(?i)(@import\s+)"/([^/"][^"]*)?"`)
	reCSSImportSingle = regexp.MustCompile(`(?i)(@import\s+)'/([^/'][^']*)?'`)

	reHeadOpen = regexp.MustCompile(`(?i)<head[^>]*>`)
	reHTMLOpen = regexp.MustCompile(`(?i)<html[^>]*>`)
	reBaseTag  = regexp.MustCompile(`(?i)<base\s[^>]*href`)
)

func newRewriter(cfg *Rewrite, prefix string, target *url.URL) (*rewriter, error) {
	if cfg == nil {
		return nil, nil //nolint:nilnil // no rewrite configured
	}

	r := &rewriter{
		cfg:         cfg,
		prefix:      strings.TrimSuffix(path.Join("/", prefix), "/"),
		target:      target,
		types:       normalizeContentTypes(cfg.ContentTypes, DefaultRewriteContentTypes),
		maxBodySize: cfg.MaxBodySize,
	}

	if r.maxBodySize <= 0 {
		r.maxBodySize = DefaultRewriteMaxBodySize
	}

	if cfg.Base {
		r.baseTag = fmt.Appendf(nil, `<base href="%s/">`, r.prefix)
	}

	if cfg.Origin && target != nil && target.Host != "" {
		prefixBytes := []byte(r.prefix)
		r.origins = []replacement{
			{[]byte(target.Scheme + `:\/\/` + target.Host), []byte(strings.ReplaceAll(r.prefix, "/", `\/`))},
			{[]byte(target.Scheme + "://" + target.Host), prefixBytes},
			{[]byte("//" + target.Host), prefixBytes},
		}
	}

	for i := range cfg.Replace {
		c, err := compileReplace(&cfg.Replace[i], r)
		if err != nil {
			return nil, err
		}

		r.replaces = append(r.replaces, c)
	}

	return r, nil
}

func compileReplace(replace *Replace, r *rewriter) (compiledReplace, error) {
	newValue := strings.NewReplacer(
		"{{prefix}}", r.prefix,
		"{{url}}", r.target.String(),
	).Replace(replace.New)

	c := compiledReplace{
		new:   []byte(newValue),
		types: normalizeContentTypes(replace.ContentTypes, r.types),
	}

	if replace.Regex != "" {
		reg, err := regexp.Compile(replace.Regex)
		if err != nil {
			return c, fmt.Errorf("page rewrite replace regex %q: %w", replace.Regex, err)
		}

		c.regex = reg

		return c, nil
	}

	if replace.Old == "" {
		return c, fmt.Errorf("page rewrite replace needs regex or old")
	}

	c.old = []byte(replace.Old)

	return c, nil
}

func normalizeContentTypes(values []string, fallback []string) []string {
	if len(values) == 0 {
		return fallback
	}

	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out = append(out, v)
		}
	}

	if len(out) == 0 {
		return fallback
	}

	return out
}

// modifyRequest is called before the request leaves to the backend.
func (r *rewriter) modifyRequest(req *http.Request) {
	if r == nil {
		return
	}

	if httputil.IsWebSocket(req) && req.Header.Get(httputil.HeaderOrigin) != "" && r.target != nil && r.target.Host != "" {
		req.Header.Set(httputil.HeaderOrigin, r.target.Scheme+"://"+r.target.Host)
	}

	if r.cfg.ForwardPrefix {
		req.Header.Set("X-Forwarded-Prefix", r.prefix)
	}

	if r.needBody() {
		// rewriting a compressed body needs a decoder for every algorithm the
		// backend might pick, asking for identity keeps that surface at zero.
		req.Header.Set(httputil.HeaderAcceptEncoding, "identity")
	}
}

func (r *rewriter) needBody() bool {
	return r.cfg.Base || r.cfg.Absolute || r.cfg.Origin || len(r.replaces) > 0
}

func (r *rewriter) modifyResponse(resp *http.Response) error {
	if r == nil {
		return nil
	}

	if r.cfg.Frame {
		r.stripFrameGuards(resp.Header)
	}

	if r.cfg.Location {
		if location := resp.Header.Get(httputil.HeaderLocation); location != "" {
			resp.Header.Set(httputil.HeaderLocation, r.rewriteLocation(location))
		}
	}

	if r.cfg.Cookie {
		r.rewriteCookies(resp.Header)
	}

	if !r.needBody() {
		return nil
	}

	return r.rewriteBody(resp)
}

func (r *rewriter) rewriteBody(resp *http.Response) error {
	if resp.Body == nil || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}

	if !matchContentType(resp.Header.Get(httputil.HeaderContentType), r.types) {
		return nil
	}

	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get(httputil.HeaderContentEncoding)))
	if encoding != "" && encoding != "identity" && encoding != "gzip" {
		// unknown encoding, better to pass it through than to corrupt it.
		return nil
	}

	body, ok, err := readLimited(resp.Body, r.maxBodySize)
	if err != nil {
		return err
	}

	if !ok {
		// too big, restore the stream and leave it untouched.
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}

		return nil
	}

	_ = resp.Body.Close()

	if encoding == "gzip" {
		compressed := body
		body, ok, err = gunzipLimited(body, r.maxBodySize)
		if err != nil {
			return err
		}
		if !ok {
			resp.Body = io.NopCloser(bytes.NewReader(compressed))

			return nil
		}

		resp.Header.Del(httputil.HeaderContentEncoding)
	}

	body = r.rewriteContent(body, resp.Header.Get(httputil.HeaderContentType))

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set(httputil.HeaderContentLength, strconv.Itoa(len(body)))
	// the body no longer matches what the backend hashed.
	resp.Header.Del("ETag")

	return nil
}

func (r *rewriter) rewriteContent(body []byte, contentType string) []byte {
	if r.cfg.Absolute {
		body = r.rewriteAbsolute(body)
		body = r.rewriteSrcsets(body)
	}

	// Origin replacement runs after root-absolute URL rewriting. Otherwise an
	// absolute backend URL first becomes the prefix and then gets prefixed again.
	for _, o := range r.origins {
		body = bytes.ReplaceAll(body, o.old, o.new)
	}

	if len(r.baseTag) > 0 {
		body = r.injectBase(body)
	}

	for _, c := range r.replaces {
		if !matchContentType(contentType, c.types) {
			continue
		}

		if c.regex != nil {
			body = c.regex.ReplaceAll(body, c.new)

			continue
		}

		body = bytes.ReplaceAll(body, c.old, c.new)
	}

	return body
}

func (r *rewriter) rewriteAbsolute(body []byte) []byte {
	for _, rule := range []struct {
		regex      *regexp.Regexp
		pathGroup  int
		prefix     []byte
		suffix     []byte
		keepGroups []int
	}{
		{reAttrDouble, 2, nil, []byte(`"`), []int{1}},
		{reAttrSingle, 2, nil, []byte(`'`), []int{1}},
		{reCSSURLDouble, 1, []byte(`url("`), []byte(`")`), nil},
		{reCSSURLSingle, 1, []byte(`url('`), []byte(`')`), nil},
		{reCSSURLBare, 1, []byte(`url(`), []byte(`)`), nil},
		{reCSSImportDouble, 2, nil, []byte(`"`), []int{1}},
		{reCSSImportSingle, 2, nil, []byte(`'`), []int{1}},
	} {
		body = rule.regex.ReplaceAllFunc(body, func(match []byte) []byte {
			sub := rule.regex.FindSubmatch(match)
			if len(sub) <= rule.pathGroup {
				return match
			}

			value := append([]byte{'/'}, sub[rule.pathGroup]...)
			rewritten := r.absoluteURL(value)
			if bytes.Equal(rewritten, value) {
				return match
			}

			out := make([]byte, 0, len(match)+len(r.prefix))
			out = append(out, rule.prefix...)
			for _, group := range rule.keepGroups {
				out = append(out, sub[group]...)
			}
			out = append(out, rewritten...)
			out = append(out, rule.suffix...)

			return out
		})
	}

	return body
}

// injectBase puts the base tag right after <head>, never twice.
func (r *rewriter) injectBase(body []byte) []byte {
	if reBaseTag.Match(body) {
		return body
	}

	if loc := reHeadOpen.FindIndex(body); loc != nil {
		return insertAt(body, loc[1], r.baseTag)
	}

	if loc := reHTMLOpen.FindIndex(body); loc != nil {
		return insertAt(body, loc[1], r.baseTag)
	}

	return body
}

func insertAt(body []byte, index int, value []byte) []byte {
	out := make([]byte, 0, len(body)+len(value))
	out = append(out, body[:index]...)
	out = append(out, value...)
	out = append(out, body[index:]...)

	return out
}

// rewriteSrcsets handles the comma separated candidate lists of srcset.
func (r *rewriter) rewriteSrcsets(body []byte) []byte {
	rewrite := func(re *regexp.Regexp, quote byte) {
		body = re.ReplaceAllFunc(body, func(match []byte) []byte {
			sub := re.FindSubmatch(match)
			if len(sub) < 3 {
				return match
			}

			out := make([]byte, 0, len(match)+len(r.prefix))
			out = append(out, sub[1]...)
			out = append(out, r.rewriteSrcsetValue(sub[2])...)
			out = append(out, quote)

			return out
		})
	}

	rewrite(reSrcsetDouble, '"')
	rewrite(reSrcsetSingle, '\'')

	return body
}

func (r *rewriter) rewriteSrcsetValue(value []byte) []byte {
	candidates := bytes.Split(value, []byte(","))
	for i, candidate := range candidates {
		trimmed := bytes.TrimLeft(candidate, " \t\r\n")
		lead := candidate[:len(candidate)-len(trimmed)]

		urlPart := trimmed
		rest := []byte(nil)

		if idx := bytes.IndexAny(trimmed, " \t\r\n"); idx >= 0 {
			urlPart = trimmed[:idx]
			rest = trimmed[idx:]
		}

		rewritten := r.absoluteURL(urlPart)
		if bytes.Equal(rewritten, urlPart) {
			continue
		}

		out := make([]byte, 0, len(lead)+len(rewritten)+len(rest))
		out = append(out, lead...)
		out = append(out, rewritten...)
		out = append(out, rest...)

		candidates[i] = out
	}

	return bytes.Join(candidates, []byte(","))
}

// absoluteURL prefixes a root absolute reference, everything else is kept.
func (r *rewriter) absoluteURL(value []byte) []byte {
	if len(value) == 0 || value[0] != '/' {
		return value
	}

	if len(value) > 1 && value[1] == '/' {
		return value
	}
	if string(value) == r.prefix || bytes.HasPrefix(value, []byte(r.prefix+"/")) {
		return value
	}

	return append([]byte(r.prefix), value...)
}

func (r *rewriter) rewriteLocation(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return location
	}

	if u.Host != "" {
		if r.target == nil || !strings.EqualFold(u.Host, r.target.Host) {
			return location
		}

		u.Scheme = ""
		u.Host = ""
		u.User = nil
	}

	if !strings.HasPrefix(u.Path, "/") {
		return location
	}

	if strings.HasPrefix(u.Path, r.prefix+"/") || u.Path == r.prefix {
		return u.String()
	}

	u.Path = r.prefix + u.Path
	u.RawPath = ""

	return u.String()
}

func (r *rewriter) rewriteCookies(header http.Header) {
	cookies := header.Values(httputil.HeaderSetCookie)
	if len(cookies) == 0 {
		return
	}

	out := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		out = append(out, r.rewriteSetCookie(cookie))
	}

	header.Del(httputil.HeaderSetCookie)
	for _, cookie := range out {
		header.Add(httputil.HeaderSetCookie, cookie)
	}
}

// rewriteSetCookie edits Path and Domain in place, every other attribute is
// copied over untouched so nothing gets lost on the way.
func (r *rewriter) rewriteSetCookie(cookie string) string {
	parts := strings.Split(cookie, ";")
	out := make([]string, 0, len(parts)+1)
	out = append(out, parts[0])

	hasPath := false

	for _, part := range parts[1:] {
		attr := strings.TrimSpace(part)
		key, value, _ := strings.Cut(attr, "=")

		switch strings.ToLower(strings.TrimSpace(key)) {
		case "path":
			hasPath = true
			out = append(out, "Path="+r.cookiePath(strings.TrimSpace(value)))
		case "domain":
			// the cookie now belongs to the proxy host.
		default:
			out = append(out, attr)
		}
	}

	if !hasPath {
		out = append(out, "Path="+r.prefix+"/")
	}

	return strings.Join(out, "; ")
}

func (r *rewriter) cookiePath(value string) string {
	if value == "" || value == "/" {
		return r.prefix + "/"
	}

	if !strings.HasPrefix(value, "/") {
		return value
	}

	if strings.HasPrefix(value, r.prefix+"/") || value == r.prefix {
		return value
	}

	return r.prefix + value
}

func (r *rewriter) stripFrameGuards(header http.Header) {
	header.Del("X-Frame-Options")

	for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
		values := header.Values(name)
		if len(values) == 0 {
			continue
		}

		out := make([]string, 0, len(values))
		for _, value := range values {
			if stripped := stripFrameAncestors(value); stripped != "" {
				out = append(out, stripped)
			}
		}

		header.Del(name)
		for _, value := range out {
			header.Add(name, value)
		}
	}
}

func stripFrameAncestors(value string) string {
	directives := strings.Split(value, ";")
	out := make([]string, 0, len(directives))

	for _, directive := range directives {
		trimmed := strings.TrimSpace(directive)
		if trimmed == "" {
			continue
		}

		fields := strings.Fields(trimmed)
		name := fields[0]
		if strings.EqualFold(name, "frame-ancestors") {
			continue
		}

		out = append(out, trimmed)
	}

	return strings.Join(out, "; ")
}

func matchContentType(contentType string, types []string) bool {
	if contentType == "" {
		return false
	}

	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		media = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}

	for _, t := range types {
		if media == t {
			return true
		}
	}

	return false
}

// readLimited reads at most limit bytes, ok is false when the source has more.
func readLimited(reader io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}

	if int64(len(body)) > limit {
		return body, false, nil
	}

	return body, true, nil
}

func gunzipLimited(body []byte, limit int64) ([]byte, bool, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = reader.Close() }()

	return readLimited(reader, limit)
}
