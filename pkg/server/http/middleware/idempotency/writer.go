package idempotency

import (
	"bufio"
	"bytes"
	"net"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/replay"
)

// recorder streams the response to the client and keeps a copy of it up to
// limit bytes.
type recorder struct {
	http.ResponseWriter

	limit    int64
	body     bytes.Buffer
	code     int
	snapshot http.Header
	overflow bool
	hijacked bool
	// failed is set when writing to the client failed, so the stored body
	// may be incomplete.
	failed bool
}

// header returns the headers of the response; a handler that wrote nothing
// still set them on w.
func (w *recorder) header(dst http.ResponseWriter) http.Header {
	if w.snapshot == nil {
		return dst.Header()
	}

	return w.snapshot
}

func (w *recorder) status() int {
	if w.code == 0 {
		return http.StatusOK
	}

	return w.code
}

func (w *recorder) WriteHeader(code int) {
	if w.code == 0 && !replay.Informational(code) {
		w.code = code
		w.snapshot = w.ResponseWriter.Header().Clone()
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}

	if !w.overflow {
		if int64(w.body.Len()+len(b)) > w.limit {
			w.overflow = true
			w.body = bytes.Buffer{}
		} else {
			w.body.Write(b)
		}
	}

	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		w.failed = true
	}

	return n, err
}

func (w *recorder) Flush() {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}

	return conn, rw, err
}

func (w *recorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
