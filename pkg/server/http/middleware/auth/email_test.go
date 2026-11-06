package auth

import (
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildEmailMessageTemplate(t *testing.T) {
	cfg := EmailSettings{
		Subject:               "Login {{.Email}}",
		BodyTemplate:          "Code={{.Code}} Expires={{.ExpiresIn}}",
		MagicLinkSubject:      "Link {{.Email}}",
		MagicLinkBodyTemplate: "Code={{.Code}} Link={{.MagicLink}} Expires={{.ExpiresIn}}",
		CodeLifetime:          "2m",
	}
	if err := parseEmailSettingDurations(&cfg); err != nil {
		t.Fatalf("parse duration: %v", err)
	}

	// code mail: no redirect_uri, code templates, no magic link
	code, err := buildEmailMessage(cfg, "user@example.com", "abc123", "ui", "", nil, nil)
	if err != nil {
		t.Fatalf("build code message: %v", err)
	}
	if code.Subject != "Login user@example.com" {
		t.Fatalf("code subject = %q", code.Subject)
	}
	if code.MagicLink != "" {
		t.Fatalf("code mail must not carry a magic link: %q", code.MagicLink)
	}
	if !strings.Contains(code.Body, "Code=abc123") {
		t.Fatalf("code body = %q", code.Body)
	}

	// magic-link mail: redirect_uri allowed, magic templates
	magic, err := buildEmailMessage(cfg, "user@example.com", "abc123", "ui", "https://app.example.com/callback", nil, nil)
	if err != nil {
		t.Fatalf("build magic message: %v", err)
	}
	if magic.Subject != "Link user@example.com" {
		t.Fatalf("magic subject = %q", magic.Subject)
	}
	if !strings.Contains(magic.MagicLink, "code=abc123") {
		t.Fatalf("magic link = %q", magic.MagicLink)
	}
	for _, want := range []string{"Code=abc123", "Link=https://", "Expires=2m0s"} {
		if !strings.Contains(magic.Body, want) {
			t.Fatalf("magic body missing %q: %q", want, magic.Body)
		}
	}
}

func TestBuildEmailMessageMagicLinkDisabled(t *testing.T) {
	disabled := false
	cfg := EmailSettings{MagicLink: &disabled}

	msg, err := buildEmailMessage(cfg, "user@example.com", "abc123", "ui", "https://app.example.com/callback", nil, nil)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if msg.MagicLink != "" {
		t.Fatalf("magic link must be empty when disabled: %q", msg.MagicLink)
	}
}

func TestValidateEmailTemplatesRejectsMissingKey(t *testing.T) {
	if err := validateEmailTemplates(EmailSettings{BodyTemplate: "{{.Missing}}"}); err == nil {
		t.Fatal("expected missing template key error")
	}
}

func TestSendEmailClosesConnOnGreetingFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	closed := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			closed <- err
			return
		}
		defer conn.Close()

		_, _ = conn.Write([]byte("554 go away\r\n"))
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = io.ReadAll(conn)
		closed <- err
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	portN, _ := strconv.Atoi(port)
	cfg := EmailSettings{From: "a@example.com", SMTP: SMTPSettings{Host: host, Port: portN}}
	if err := sendEmail(cfg, "b@example.com", "s", "b"); err == nil {
		t.Fatal("expected greeting failure")
	}

	if err := <-closed; err != nil {
		t.Fatalf("client conn was not closed: %v", err)
	}
}
