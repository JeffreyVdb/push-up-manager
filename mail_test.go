package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearAccountMailEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PUSHUP_SMTP_HOST", "PUSHUP_SMTP_PORT", "PUSHUP_SMTP_FROM", "PUSHUP_SMTP_USER", "PUSHUP_SMTP_PASSWORD", "PUSHUP_SMTP_PASSWORD_FILE", "PUSHUP_PUBLIC_URL"} {
		t.Setenv(key, "")
	}
}
func configuredAccountMailEnv(t *testing.T) {
	t.Helper()
	clearAccountMailEnv(t)
	t.Setenv("PUSHUP_SMTP_HOST", "smtp.example.com")
	t.Setenv("PUSHUP_SMTP_FROM", "Push-ups@example.com")
	t.Setenv("PUSHUP_SMTP_USER", "push-ups@example.com")
	t.Setenv("PUSHUP_SMTP_PASSWORD", "fake-test-password")
	t.Setenv("PUSHUP_PUBLIC_URL", "https://pushups.example/")
}

func TestAccountMailConfiguration(t *testing.T) {
	t.Run("optional", func(t *testing.T) {
		clearAccountMailEnv(t)
		sender, origin, err := accountMailFromEnv()
		if err != nil || sender != nil || origin != "" {
			t.Fatalf("optional config: %v %v %q", err, sender, origin)
		}
	})
	t.Run("file wins", func(t *testing.T) {
		configuredAccountMailEnv(t)
		file := filepath.Join(t.TempDir(), "password")
		if err := os.WriteFile(file, []byte("fake-file-password\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PUSHUP_SMTP_PASSWORD_FILE", file)
		sender, origin, err := accountMailFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		mailer := sender.(*smtpMailer)
		if mailer.password != "fake-file-password" || mailer.from != "push-ups@example.com" || mailer.port != "587" || origin != "https://pushups.example" {
			t.Fatal("file/default/origin configuration mismatch")
		}
		t.Setenv("PUSHUP_SMTP_PORT", "465")
		if _, _, err := accountMailFromEnv(); err != nil {
			t.Fatal(err)
		}
	})
	for name, origin := range map[string]string{"missing": "", "http": "http://pushups.example", "path": "https://pushups.example/path", "query": "https://pushups.example?token=x", "fragment": "https://pushups.example#x", "userinfo": "https://user:password@pushups.example", "empty host": "https://:443", "bad port": "https://pushups.example:65536", "empty query": "https://pushups.example?"} {
		t.Run(name, func(t *testing.T) {
			configuredAccountMailEnv(t)
			t.Setenv("PUSHUP_PUBLIC_URL", origin)
			if _, _, err := accountMailFromEnv(); err == nil {
				t.Fatal("accepted invalid origin")
			}
		})
	}
	t.Run("invalid while mail disabled", func(t *testing.T) {
		clearAccountMailEnv(t)
		t.Setenv("PUSHUP_PUBLIC_URL", "https://pushups.example/path")
		if _, _, err := accountMailFromEnv(); err == nil {
			t.Fatal("invalid origin accepted when mail disabled")
		}
	})
	t.Run("loopback development", func(t *testing.T) {
		configuredAccountMailEnv(t)
		t.Setenv("PUSHUP_PUBLIC_URL", "http://localhost:8080")
		if _, _, err := accountMailFromEnv(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("plaintext port", func(t *testing.T) {
		configuredAccountMailEnv(t)
		t.Setenv("PUSHUP_SMTP_PORT", "25")
		if _, _, err := accountMailFromEnv(); err == nil {
			t.Fatal("accepted insecure SMTP port")
		}
	})
	t.Run("unreadable secret", func(t *testing.T) {
		configuredAccountMailEnv(t)
		t.Setenv("PUSHUP_SMTP_PASSWORD_FILE", filepath.Join(t.TempDir(), "absent"))
		_, _, err := accountMailFromEnv()
		if err == nil || strings.Contains(err.Error(), "fake-test-password") {
			t.Fatal("missing file did not fail safely")
		}
	})
}

func TestAccountMailAddressValidation(t *testing.T) {
	for _, address := range []string{"", "x", "A User <user@example.com>", "user@example.com\r\nBcc:other@example.com", "user @example.com", strings.Repeat("a", 255) + "@example.com"} {
		if _, err := normalizeAccountEmail(address); err == nil {
			t.Errorf("accepted invalid address %q", address)
		}
	}
	if got, err := normalizeAccountEmail(" User+tag@Example.com "); err != nil || got != "user+tag@example.com" {
		t.Fatalf("address normalization: %q %v", got, err)
	}
}

func TestSMTPLoginRequiresTLS(t *testing.T) {
	auth := &smtpLoginAuth{username: "fake-user", password: "fake-password"}
	if _, _, err := auth.Start(&smtp.ServerInfo{TLS: false}); err == nil {
		t.Fatal("LOGIN offered over plaintext")
	}
	mechanism, initial, err := auth.Start(&smtp.ServerInfo{TLS: true})
	if err != nil || mechanism != "LOGIN" || len(initial) != 0 {
		t.Fatal("LOGIN start failed")
	}
	user, err := auth.Next([]byte("Username:"), true)
	if err != nil || string(user) != "fake-user" {
		t.Fatal("LOGIN username failed")
	}
	password, err := auth.Next([]byte("Password:"), true)
	if err != nil || string(password) != "fake-password" {
		t.Fatal("LOGIN password failed")
	}
	if _, err := auth.Next(nil, true); err == nil {
		t.Fatal("LOGIN unexpected challenge accepted")
	}
}

func TestSMTPRefusesServerWithoutSTARTTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		io.WriteString(conn, "220 test ESMTP\r\n")
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		io.WriteString(conn, "250-test\r\n250 AUTH LOGIN PLAIN\r\n")
		next, _ := io.ReadAll(reader)
		commands <- line + string(next)
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	mailer := &smtpMailer{host: host, port: port, username: "fake-user", password: "fake-secret", from: "from@example.com"}
	err = mailer.Send(context.Background(), "to@example.com", "Test", "Body")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("SMTP accepted plaintext-only server: %v", err)
	}
	select {
	case commands := <-commands:
		if !strings.HasPrefix(commands, "EHLO ") || strings.Contains(commands, "AUTH") || strings.Contains(commands, "fake-secret") || strings.Contains(commands, "MAIL FROM") {
			t.Fatalf("credentials/message offered before TLS: %q", commands)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP client did not close connection")
	}
}
