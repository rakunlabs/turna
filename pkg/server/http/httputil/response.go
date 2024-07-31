package httputil

import (
	"encoding/json"
	"net/http"
)

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
