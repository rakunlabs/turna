package gateway

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// gRPC status codes used by the gateway, see
// https://github.com/grpc/grpc/blob/master/doc/http-grpc-status-mapping.md
const (
	grpcInternal      = 13
	grpcUnimplemented = 12
	grpcUnavailable   = 14
	grpcDeadline      = 4
	grpcUnknown       = 2
)

// isGRPC matches application/grpc and application/grpc+proto like content
// types, gRPC-Web (application/grpc-web) is plain HTTP for the gateway.
func isGRPC(r *http.Request) bool {
	return isGRPCContentType(r.Header.Get("Content-Type"))
}

func isGRPCContentType(ct string) bool {
	rest, ok := strings.CutPrefix(ct, "application/grpc")
	if !ok {
		return false
	}

	return rest == "" || rest[0] == '+' || rest[0] == ';'
}

func grpcCode(status int) int {
	switch status {
	case http.StatusNotFound:
		return grpcUnimplemented
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return grpcUnavailable
	case http.StatusGatewayTimeout:
		return grpcDeadline
	case http.StatusInternalServerError:
		return grpcInternal
	default:
		return grpcUnknown
	}
}

// writeError answers with status, as a gRPC trailers-only response for gRPC
// requests.
func writeError(w http.ResponseWriter, r *http.Request, status int) {
	if isGRPC(r) {
		h := w.Header()
		h.Set("Content-Type", "application/grpc")
		h.Set("Grpc-Status", strconv.Itoa(grpcCode(status)))
		h.Set("Grpc-Message", http.StatusText(status))
		w.WriteHeader(http.StatusOK)

		return
	}

	httputil.HandleError(w, httputil.NewError("", nil, status))
}

// grpcErrorWriter turns error responses written by later handlers (for
// example an unreachable upstream) into gRPC status responses. Responses
// from gRPC upstreams carry their own content type and pass unchanged.
type grpcErrorWriter struct {
	http.ResponseWriter

	req      *http.Request
	started  bool
	replaced bool
}

func (w *grpcErrorWriter) WriteHeader(code int) {
	if informational(code) {
		if !w.started {
			w.ResponseWriter.WriteHeader(code)
		}

		return
	}

	if w.started {
		return
	}

	w.started = true

	if code >= 400 && !isGRPCContentType(w.Header().Get("Content-Type")) {
		w.replaced = true
		w.Header().Del("Content-Length")
		writeError(w.ResponseWriter, w.req, code)

		return
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *grpcErrorWriter) Write(b []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}

	if w.replaced {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *grpcErrorWriter) Flush() {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.replaced {
		f.Flush()
	}
}

func (w *grpcErrorWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func grpcErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isGRPC(r) {
			next.ServeHTTP(w, r)

			return
		}

		next.ServeHTTP(&grpcErrorWriter{ResponseWriter: w, req: r}, r)
	})
}

// splitGRPCPath splits "/pkg.Service/Method".
func splitGRPCPath(path string) (string, string, bool) {
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return "", "", false
	}

	service, method, ok := strings.Cut(rest, "/")
	if !ok || service == "" || method == "" || strings.Contains(method, "/") {
		return "", "", false
	}

	return service, method, true
}

// lessGRPC implements the GRPCRoute precedence: longer service, longer
// method, more header matches, then rule order.
func lessGRPC(a, b *entry) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}

	if la, lb := len(a.match.grpcService.value), len(b.match.grpcService.value); la != lb {
		return la > lb
	}

	if la, lb := len(a.match.grpcMethod.value), len(b.match.grpcMethod.value); la != lb {
		return la > lb
	}

	if len(a.match.headers) != len(b.match.headers) {
		return len(a.match.headers) > len(b.match.headers)
	}

	return a.order < b.order
}
