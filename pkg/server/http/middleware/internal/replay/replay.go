// Package replay buffers requests so they can be sent again and holds back
// responses that are retried.
package replay

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
)

// Informational reports 1xx responses that are followed by the final
// response.
func Informational(code int) bool {
	return code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
}

// BufferBody reads the request body when it is not larger than limit. ok is
// false when the body is larger; r.Body then still yields the whole body.
// A body of unknown length is a stream, such as gRPC streaming or a chunked
// upload, which may wait for the response before it ends; it is not
// buffered.
func BufferBody(r *http.Request, limit int64) (body []byte, ok bool, err error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true, nil
	}

	if r.ContentLength > limit || r.ContentLength < 0 {
		return nil, false, nil
	}

	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, false, err
	}

	if int64(len(buf)) > limit {
		r.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(buf), r.Body), Closer: r.Body}

		return nil, false, nil
	}

	_ = r.Body.Close()

	return buf, true, nil
}

// SetBody gives req a fresh copy of body.
func SetBody(req *http.Request, body []byte) {
	if body == nil {
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}

// Writer holds back response headers until the status is known and drops
// the response when the status is retryable.
type Writer struct {
	http.ResponseWriter

	header    http.Header
	retryable func(code int) bool
	written   bool
	discard   bool
}

// NewWriter returns a writer of one attempt; retryable decides whether a
// final status is dropped.
func NewWriter(w http.ResponseWriter, retryable func(code int) bool) *Writer {
	return &Writer{ResponseWriter: w, header: w.Header().Clone(), retryable: retryable}
}

// Discarded reports that the response was dropped and the request can be
// sent again.
func (w *Writer) Discarded() bool {
	return w.discard
}

// Finish sends the headers of a handler that returned without writing, as
// net/http does with an implicit 200.
func (w *Writer) Finish() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *Writer) Header() http.Header {
	if w.written && !w.discard {
		// trailers are set after the status is written
		return w.ResponseWriter.Header()
	}

	return w.header
}

func (w *Writer) WriteHeader(code int) {
	if w.written {
		return
	}

	if Informational(code) {
		// interim responses such as 103 Early Hints are sent on; a later
		// retry still sends its own final response
		w.copyHeader()
		w.ResponseWriter.WriteHeader(code)

		return
	}

	w.written = true

	if w.retryable(code) {
		w.discard = true

		return
	}

	w.copyHeader()
	w.ResponseWriter.WriteHeader(code)
}

// copyHeader sets the headers of the attempt on the response.
func (w *Writer) copyHeader() {
	dst := w.ResponseWriter.Header()
	for k := range dst {
		if _, ok := w.header[k]; !ok {
			delete(dst, k)
		}
	}

	for k, v := range w.header {
		dst[k] = v
	}
}

func (w *Writer) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.discard {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *Writer) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.discard {
		return
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hands the connection over, for example for upgraded connections.
// A hijacked attempt is final.
func (w *Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.written = true
	}

	return conn, rw, err
}

func (w *Writer) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
