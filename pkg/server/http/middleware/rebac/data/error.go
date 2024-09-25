package data

import (
	"encoding/json"
	"errors"
	"net/http"
)

type ResponseError struct {
	Message Message `json:"message"`
}

type ErrorPayload struct {
	Err string `json:"error"`
}

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

func (e Error) GetCode() int {
	return e.Code
}

func (e Error) MarshalJSON() ([]byte, error) {
	errStr := ""
	if e.Err != nil {
		errStr = e.Err.Error()
	}

	if e.Msg == "" {
		e.Msg = http.StatusText(e.Code)
	}

	ret := ResponseError{
		Message: Message{
			Text: e.Msg,
			Err:  errStr,
		},
	}

	return json.Marshal(ret)
}

func NewError(message string, err error, code int) Error {
	return Error{
		Msg:  message,
		Err:  err,
		Code: code,
	}
}

func NewErrorAs(err error) Error {
	if err != nil {
		e := Error{}
		if errors.As(err, &e) {
			return e
		}
	}

	return NewError("", err, http.StatusInternalServerError)
}
