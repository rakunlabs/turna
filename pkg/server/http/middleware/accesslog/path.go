package accesslog

import "github.com/bmatcuk/doublestar/v4"

type Path struct {
	Disabled []Check `cfg:"disabled"`
	Enabled  []Check `cfg:"enabled"`
}

type Check struct {
	URL string `cfg:"url"`
	// Method is the HTTP method to check for, default all methods "*"
	Methods []string `cfg:"methods"`

	LogDetails *LogDetails `cfg:"log_details"`

	methods *Method
}

func (c *Check) IsIn(path, method string) bool {
	if c.URL == "" {
		return false
	}

	if v, _ := doublestar.Match(c.URL, path); v {
		if c.methods.IsIn(method) {
			return true
		}
	}

	return false
}

type LogDetails struct {
	// RequestBody is the request body to log default true
	RequestBody *bool `cfg:"request_body"`
	// RequestBodySize is the maximum size of the request body to log
	// default is 512 (bytes), 0 means no limit
	RequestBodySize int `cfg:"request_body_size"`
	// ResponseBody is the response body to log default true
	ResponseBody *bool `cfg:"response_body"`
	// ResponseBodySize is the maximum size of the response body to log
	// default is 512 (bytes), 0 means no limit
	ResponseBodySize int `cfg:"response_body_size"`
	// Headers is the headers to log default true
	Headers *bool `cfg:"headers"`
	// SanitizeHeaders is a list of headers to sanitize before logging.
	// Default added Authorization, Cookie, Set-Cookie
	// and X-Forwarded-For headers.
	// The headers will be sanitized to [REDACTED]
	SanitizeHeaders []string `cfg:"sanitize_headers"`
}

func (l *LogDetails) GetRequestBody() bool {
	if l.RequestBody == nil {
		return true
	}

	return *l.RequestBody
}

func (l *LogDetails) GetResponseBody() bool {
	if l.ResponseBody == nil {
		return true
	}

	return *l.ResponseBody
}

func (l *LogDetails) GetHeaders() bool {
	if l.Headers == nil {
		return true
	}

	return *l.Headers
}

func (l *LogDetails) Default() {
	if l.RequestBodySize == 0 {
		l.RequestBodySize = 512
	}

	if l.ResponseBodySize == 0 {
		l.ResponseBodySize = 512
	}

	if len(l.SanitizeHeaders) == 0 {
		l.SanitizeHeaders = []string{
			"Authorization",
			"Cookie",
			"Set-Cookie",
			"X-Forwarded-For",
		}
	}
}
