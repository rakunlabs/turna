package folder

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
)

var precompressedExt = map[string]string{
	"br":   ".br",
	"zstd": ".zst",
	"gzip": ".gz",
}

// precompressed opens the sidecar of filePath with the best encoding accepted
// by the request, it returns a nil file when there is none.
func (f *Folder) precompressed(w http.ResponseWriter, r *http.Request, filePath string) (http.File, fs.FileInfo, string) {
	if len(f.precompressedEnc) == 0 || f.customContent != nil {
		return nil, nil, ""
	}

	switch path.Ext(filePath) {
	case ".br", ".zst", ".gz":
		return nil, nil, ""
	}

	addVary(w.Header(), "Accept-Encoding")

	for _, encoding := range acceptedEncodings(r.Header.Values("Accept-Encoding"), f.precompressedEnc) {
		file, err := f.fs.Open(filePath + precompressedExt[encoding])
		if err != nil {
			continue
		}

		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			file.Close()

			continue
		}

		return file, info, encoding
	}

	return nil, nil, ""
}

// acceptedEncodings returns the encodings acceptable by the Accept-Encoding
// headers, ordered by q-value and then by preference.
func acceptedEncodings(headers []string, preference []string) []string {
	qs := make(map[string]float64)
	star := -1.0

	for _, header := range headers {
		for part := range strings.SplitSeq(header, ",") {
			coding, params, _ := strings.Cut(part, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding == "" {
				continue
			}

			q := 1.0
			for param := range strings.SplitSeq(params, ";") {
				k, v, ok := strings.Cut(param, "=")
				if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
					parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
					if err != nil || parsed < 0 || parsed > 1 {
						parsed = 0
					}

					q = parsed
				}
			}

			switch coding {
			case "*":
				star = q
			case "x-gzip":
				qs["gzip"] = q
			default:
				qs[coding] = q
			}
		}
	}

	type accepted struct {
		encoding string
		q        float64
	}

	list := make([]accepted, 0, len(preference))
	for _, encoding := range preference {
		q, ok := qs[encoding]
		if !ok {
			q = star
		}

		if q > 0 {
			list = append(list, accepted{encoding: encoding, q: q})
		}
	}

	slices.SortStableFunc(list, func(a, b accepted) int {
		switch {
		case a.q > b.q:
			return -1
		case a.q < b.q:
			return 1
		default:
			return 0
		}
	})

	encodings := make([]string, 0, len(list))
	for _, a := range list {
		encodings = append(encodings, a.encoding)
	}

	return encodings
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for v := range strings.SplitSeq(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				return
			}
		}
	}

	header.Add("Vary", value)
}

// lengthWriter sets Content-Length of a full response, http.ServeContent
// leaves it out when Content-Encoding is set.
type lengthWriter struct {
	http.ResponseWriter

	size int64
}

func (w *lengthWriter) WriteHeader(code int) {
	if code == http.StatusOK && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(w.size, 10))
	}

	w.ResponseWriter.WriteHeader(code)
}

// ReadFrom keeps the sendfile path of the underlying writer.
func (w *lengthWriter) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}

	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, src)
}

func (w *lengthWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
