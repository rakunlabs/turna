package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/crewjam/saml"
)

const samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"

// parseSAMLResponse guards crewjam/saml against GO-2026-6909, which has no
// fixed version: an assertion without Issuer panics after the signature is
// verified, and a NameID of a foreign namespace is read as the subject.
func parseSAMLResponse(sp *saml.ServiceProvider, r *http.Request, requestIDs []string) (assertion *saml.Assertion, err error) {
	if raw := r.PostForm.Get("SAMLResponse"); raw != "" {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse base64: %w", err)
		}

		if err := checkSAMLAssertions(decoded); err != nil {
			return nil, err
		}
	}

	defer func() {
		if v := recover(); v != nil {
			assertion, err = nil, fmt.Errorf("invalid saml response: %v", v)
		}
	}()

	return sp.ParseResponse(r, requestIDs)
}

// checkSAMLAssertions refuses plaintext assertions without a SAML Issuer and
// subjects with a NameID outside the SAML assertion namespace.
func checkSAMLAssertions(doc []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(doc))

	type frame struct {
		name      xml.Name
		hasIssuer bool
	}

	var stack []frame

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid saml response xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) > 0 {
				parent := &stack[len(stack)-1]

				if parent.name == (xml.Name{Space: samlAssertionNS, Local: "Assertion"}) && t.Name == (xml.Name{Space: samlAssertionNS, Local: "Issuer"}) {
					parent.hasIssuer = true
				}

				if parent.name == (xml.Name{Space: samlAssertionNS, Local: "Subject"}) && t.Name.Local == "NameID" && t.Name.Space != samlAssertionNS {
					return errors.New("saml subject NameID is not in the assertion namespace")
				}
			}

			stack = append(stack, frame{name: t.Name})
		case xml.EndElement:
			if len(stack) == 0 {
				return errors.New("invalid saml response xml")
			}

			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			if top.name == (xml.Name{Space: samlAssertionNS, Local: "Assertion"}) && !top.hasIssuer {
				return errors.New("saml assertion has no issuer")
			}
		}
	}
}
