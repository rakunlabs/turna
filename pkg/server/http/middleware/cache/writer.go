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

// bufferedWriter holds the whole response.
type bufferedWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newBufferedWriter() *bufferedWriter {
	return &bufferedWriter{header: http.Header{}}
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

	return w.body.Write(b)
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
