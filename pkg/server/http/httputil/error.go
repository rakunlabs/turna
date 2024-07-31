package httputil

import (
	"errors"
	"net/http"
)

type Error struct {
	Message string
	Status  int
}

func (e Error) Error() string {
	return e.Message
}

func HandleError(w http.ResponseWriter, err error) {
	if err != nil {
		e := Error{}
		if errors.As(err, &e) {
			http.Error(w, e.Message, e.Status)

			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
