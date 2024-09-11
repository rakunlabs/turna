package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Error struct {
	Msg string `json:"message,omitempty"`
	Err error  `json:"error,omitempty"`

	Code int `json:"-"`
}

func (e Error) Error() string {
	if e.Err == nil {
		return e.Msg
	}

	return e.Err.Error()
}

func NewError(message string, err error, code int) Error {
	return Error{
		Msg:  message,
		Err:  err,
		Code: code,
	}
}

func NewErrorAs(err error) Error {
	e := Error{}
	if errors.As(err, &e) {
		return e
	}

	return NewError("", err, http.StatusInternalServerError)
}

func (e Error) MarshalJSON() ([]byte, error) {
	errStr := ""
	if e.Err != nil {
		errStr = e.Err.Error()
	}

	if e.Msg == "" {
		e.Msg = http.StatusText(e.Code)
	}

	ret := struct {
		Msg string `json:"message,omitempty"`
		Err string `json:"error,omitempty"`
	}{
		Msg: e.Msg,
		Err: errStr,
	}

	return json.Marshal(ret)
}

func HandleError(w http.ResponseWriter, resp Error) {
	h := w.Header()

	// Delete the Content-Length header, which might be for some other content.
	// Assuming the error string fits in the writer's buffer, we'll figure
	// out the correct Content-Length for it later.
	//
	// We don't delete Content-Encoding, because some middleware sets
	// Content-Encoding: gzip and wraps the ResponseWriter to compress on-the-fly.
	// See https://go.dev/issue/66343.
	h.Del("Content-Length")

	// There might be content type already set, but we reset it to
	// text/plain for the error message.
	h.Set("Content-Type", "application/json; charset=utf-8")

	code := resp.Code
	if code == 0 {
		code = http.StatusInternalServerError
	}

	_ = JSON(w, code, resp)
}
