package decompress

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// Decompress decodes gzip, br and zstd encoded request bodies.
type Decompress struct{}

type pooledReader struct {
	io.Reader

	release func()
}

func (m *Decompress) Middleware() func(http.Handler) http.Handler {
	gzipPool := sync.Pool{New: func() any { return new(gzip.Reader) }}
	brotliPool := sync.Pool{New: func() any { return brotli.NewReader(nil) }}
	zstdPool := sync.Pool{New: func() any {
		// lowmem and single goroutine; request bodies are usually small
		d, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))

		return d
	}}

	newReader := func(encoding string, body io.Reader) (*pooledReader, error) {
		switch encoding {
		case "gzip", "x-gzip":
			gr := gzipPool.Get().(*gzip.Reader)
			if err := gr.Reset(body); err != nil {
				gzipPool.Put(gr)

				return nil, err
			}

			return &pooledReader{Reader: gr, release: func() {
				_ = gr.Close()
				gzipPool.Put(gr)
			}}, nil
		case "br":
			br := brotliPool.Get().(*brotli.Reader)
			if err := br.Reset(body); err != nil {
				brotliPool.Put(br)

				return nil, err
			}

			return &pooledReader{Reader: br, release: func() {
				_ = br.Reset(nil)
				brotliPool.Put(br)
			}}, nil
		case "zstd":
			zr := zstdPool.Get().(*zstd.Decoder)
			if err := zr.Reset(body); err != nil {
				zstdPool.Put(zr)

				return nil, err
			}

			return &pooledReader{Reader: zr, release: func() {
				_ = zr.Reset(nil)
				zstdPool.Put(zr)
			}}, nil
		}

		return nil, nil
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encoding := strings.ToLower(strings.TrimSpace(r.Header.Get(httputil.HeaderContentEncoding)))
			if encoding == "" || encoding == "identity" {
				next.ServeHTTP(w, r)

				return
			}

			b := r.Body
			defer b.Close()

			reader, err := newReader(encoding, b)
			if err != nil {
				if errors.Is(err, io.EOF) { // ignore if body is empty
					next.ServeHTTP(w, r)

					return
				}

				httputil.HandleError(w, httputil.NewError("", err, http.StatusBadRequest))

				return
			}

			if reader == nil {
				// unknown encoding, pass it as is
				next.ServeHTTP(w, r)

				return
			}
			defer reader.release()

			r.Body = io.NopCloser(reader)
			r.Header.Del(httputil.HeaderContentEncoding)
			r.Header.Del("Content-Length")
			r.ContentLength = -1

			next.ServeHTTP(w, r)
		})
	}
}
