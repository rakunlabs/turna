package splitter

import (
	"net/http"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type Requester struct {
	Req *http.Request
}

func (r *Requester) Header(key, value string) bool {
	return r.Req.Header.Get(key) == value
}

func (r *Requester) Path(path string) bool {
	v, _ := doublestar.Match(path, r.Req.URL.Path)

	return v
}

func (r *Requester) PathPrefix(path string) bool {
	return strings.HasPrefix(r.Req.URL.Path, path)
}

func (r *Requester) Method(method string) bool {
	return strings.EqualFold(r.Req.Method, method)
}

func (r *Requester) Host(host string) bool {
	return r.Req.Host == host
}

func (r *Requester) Query(key, value string) bool {
	return r.Req.URL.Query().Get(key) == value
}
