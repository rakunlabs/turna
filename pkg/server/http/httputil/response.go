package httputil

import (
	"encoding/json"
	"net/http"
)

func writeContentType(w http.ResponseWriter, value string) {
	header := w.Header()
	if header.Get(HeaderContentType) == "" {
		header.Set(HeaderContentType, value)
	}
}

func JSON(w http.ResponseWriter, code int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	return json.NewEncoder(w).Encode(data)
}

func JSONBlob(w http.ResponseWriter, code int, data []byte) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	_, err := w.Write(data)

	return err
}

func NoContent(w http.ResponseWriter, code int) error {
	w.WriteHeader(code)

	return nil
}

func Blob(w http.ResponseWriter, code int, contentType string, b []byte) error {
	writeContentType(w, contentType)
	w.WriteHeader(code)
	_, err := w.Write(b)

	return err
}
