package httputil

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ajg/form"
)

// ContentType is an enumeration of common HTTP content types.
type ContentType int

// ContentTypes handled by this package.
const (
	ContentTypeUnknown ContentType = iota
	ContentTypePlainText
	ContentTypeHTML
	ContentTypeJSON
	ContentTypeXML
	ContentTypeForm
	ContentTypeEventStream
)

func GetRequestContentType(r *http.Request) ContentType {
	contentType := r.Header.Get("Content-Type")

	s := strings.TrimSpace(strings.Split(contentType, ";")[0])
	switch s {
	case "text/plain":
		return ContentTypePlainText
	case "text/html", "application/xhtml+xml":
		return ContentTypeHTML
	case "application/json", "text/javascript":
		return ContentTypeJSON
	case "text/xml", "application/xml":
		return ContentTypeXML
	case "application/x-www-form-urlencoded":
		return ContentTypeForm
	case "text/event-stream":
		return ContentTypeEventStream
	default:
		return ContentTypeJSON
	}
}

// Decode detects the correct decoder for use on an HTTP request and
// marshals into a given interface.
func Decode(r *http.Request, v interface{}) error {
	var err error

	switch GetRequestContentType(r) {
	case ContentTypeJSON:
		err = DecodeJSON(r.Body, v)
	case ContentTypeXML:
		err = DecodeXML(r.Body, v)
	case ContentTypeForm:
		err = DecodeForm(r.Body, v)
	default:
		err = errors.New("render: unable to automatically decode the request content type")
	}

	return err
}

// DecodeJSON decodes a given reader into an interface using the json decoder.
func DecodeJSON(r io.Reader, v interface{}) error {
	defer io.Copy(io.Discard, r) //nolint:errcheck
	return json.NewDecoder(r).Decode(v)
}

// DecodeXML decodes a given reader into an interface using the xml decoder.
func DecodeXML(r io.Reader, v interface{}) error {
	defer io.Copy(io.Discard, r) //nolint:errcheck
	return xml.NewDecoder(r).Decode(v)
}

// DecodeForm decodes a given reader into an interface using the form decoder.
func DecodeForm(r io.Reader, v interface{}) error {
	decoder := form.NewDecoder(r) //nolint:errcheck
	return decoder.Decode(v)
}
