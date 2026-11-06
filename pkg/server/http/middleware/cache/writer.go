package cache

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
)

func informational(code int) bool {
	return code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
}

// teeWriter streams the response to the client and keeps a copy of it up to
// limit bytes.
type teeWriter struct {
	http.ResponseWriter

	limit    int64
	body     bytes.Buffer
	code     int
	snapshot http.Header
	overflow bool
	hijacked bool
}

func (w *teeWriter) WriteHeader(code int) {
	if w.code == 0 && !informational(code) {
		w.code = code
		w.snapshot = w.ResponseWriter.Header().Clone()
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *teeWriter) Write(b []byte) (int, error) {
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

	return w.ResponseWriter.Write(b)
}

func (w *teeWriter) Flush() {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *teeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}

	return conn, rw, err
}

func (w *teeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// bufferedWriter holds the response up to limit bytes. A larger response
// is not held: with a client it is streamed to it from then on, without one
// (background revalidation) the rest is dropped.
type bufferedWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int

	limit    int64
	client   http.ResponseWriter
	overflow bool
	// passed is set once the response went to the client directly
	passed bool
}

func newBufferedWriter(limit int64, client http.ResponseWriter) *bufferedWriter {
	return &bufferedWriter{header: http.Header{}, limit: limit, client: client}
}

func (w *bufferedWriter) Header() http.Header {
	return w.header
}

func (w *bufferedWriter) WriteHeader(code int) {
	if w.code == 0 && !informational(code) {
		w.code = code
	}
}

func (w *bufferedWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	if w.passed {
		return w.client.Write(b)
	}

	if w.overflow {
		return len(b), nil
	}

	if int64(w.body.Len()+len(b)) <= w.limit {
		return w.body.Write(b)
	}

	w.overflow = true

	if w.client == nil {
		w.body = bytes.Buffer{}

		return len(b), nil
	}

	w.passed = true
	w.copyTo(w.client)
	w.body = bytes.Buffer{}

	return w.client.Write(b)
}

// Flush flushes only a response that is streamed to the client.
func (w *bufferedWriter) Flush() {
	if w.passed {
		_ = http.NewResponseController(w.client).Flush()
	}
}

func (w *bufferedWriter) copyTo(dst http.ResponseWriter) {
	h := dst.Header()
	for k, v := range w.header {
		h[k] = v
	}

	code := w.code
	if code == 0 {
		code = http.StatusOK
	}

	dst.WriteHeader(code)
	_, _ = dst.Write(w.body.Bytes())
}

// statusRecorder records the final status.
type statusRecorder struct {
	http.ResponseWriter

	code int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.code == 0 && !informational(code) {
		w.code = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
