package app

import (
	"context"
	"encoding/base64"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestSMTPDeliversVerificationMailToLocalRelay(t *testing.T) {
	address, received := localSMTP(t)
	config := MailConfig{Mode: "smtp", TLS: "none", Addr: address, From: "笔记站 <sender@example.com>"}
	if err := config.validate(false); err != nil {
		t.Fatal(err)
	}
	subject, body := verificationMail("笔记站", "https://example.com/notes/", "reader", "123456", EmailPurposeReset)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := newMailer(config).Send(ctx, "reader@example.com", subject, body); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-received:
		headers, encoded, _ := strings.Cut(data, "\n\n")
		text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil || string(text) != body || !strings.Contains(headers, "To: reader@example.com") ||
			!strings.Contains(headers, " <sender@example.com>") || !strings.Contains(body, "重置密码") {
			t.Fatalf("unexpected MIME message: %s, %v", headers, err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestSMTPRequiresStartTLSWhenConfigured(t *testing.T) {
	address, received := localSMTP(t)
	config := MailConfig{Mode: "smtp", TLS: "starttls", Addr: address, From: "sender@example.com"}
	err := newMailer(config).Send(t.Context(), "reader@example.com", "Subject", "body")
	if err == nil || !strings.Contains(err.Error(), "SMTP STARTTLS") {
		t.Fatalf("server without STARTTLS: %v", err)
	}
	select {
	case <-received:
		t.Fatal("message was sent without STARTTLS")
	default:
	}
}

func TestMailConfigRejectsUnsafeOrIncompleteSettings(t *testing.T) {
	for _, config := range []MailConfig{
		{Mode: "log"},
		{Mode: "smtp", TLS: "tls", Addr: "smtp.example.com"},
		{Mode: "smtp", TLS: "tls", Addr: "smtp.example.com:465", From: "sender@example.com", User: "only-user"},
		{Mode: "smtp", TLS: "none", Addr: "smtp.example.com:25", From: "sender@example.com"},
	} {
		if err := config.validate(true); err == nil {
			t.Errorf("expected invalid configuration for %s/%s", config.Mode, config.TLS)
		}
	}
}

func localSMTP(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		server := textproto.NewConn(connection)
		defer server.Close()
		server.PrintfLine("220 localhost")
		for {
			line, err := server.ReadLine()
			if err != nil {
				return
			}
			command, _, _ := strings.Cut(line, " ")
			switch command {
			case "EHLO", "HELO", "MAIL", "RCPT":
				server.PrintfLine("250 OK")
			case "DATA":
				server.PrintfLine("354 End with a dot")
				data, err := server.ReadDotBytes()
				if err != nil {
					return
				}
				received <- string(data)
				server.PrintfLine("250 Queued")
			case "QUIT":
				server.PrintfLine("221 Bye")
				return
			default:
				server.PrintfLine("502 Unsupported")
			}
		}
	}()
	return listener.Addr().String(), received
}
