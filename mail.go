package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type mailSender interface {
	Send(context.Context, string, string, string) error
}

type smtpMailer struct {
	host, port, username, password, from string
}

func normalizeAccountEmail(input string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(input))
	if len(email) == 0 || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", errors.New("Enter a valid email address.")
	}
	for _, ch := range email {
		if ch < 33 || ch > 126 {
			return "", errors.New("Enter a valid email address.")
		}
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("Enter a valid email address.")
	}
	return email, nil
}

// accountMailFromEnv never derives a recovery URL from an incoming request.
// An unset SMTP host disables email recovery without affecting local accounts.
func accountMailFromEnv() (mailSender, string, error) {
	host := strings.TrimSpace(os.Getenv("PUSHUP_SMTP_HOST"))
	if strings.ContainsAny(host, "\r\n/: ") {
		return nil, "", errors.New("invalid PUSHUP_SMTP_HOST")
	}
	origin := strings.TrimSpace(os.Getenv("PUSHUP_PUBLIC_URL"))
	if origin == "" && host == "" {
		return nil, "", nil
	}
	publicURL, err := url.Parse(origin)
	if err != nil || publicURL.Hostname() == "" || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.ForceQuery || publicURL.Fragment != "" || (publicURL.Path != "" && publicURL.Path != "/") {
		return nil, "", errors.New("PUSHUP_PUBLIC_URL must be an explicit origin")
	}
	if port := publicURL.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, "", errors.New("invalid PUSHUP_PUBLIC_URL port")
		}
	}
	if publicURL.Scheme != "https" && !(publicURL.Scheme == "http" && (publicURL.Hostname() == "localhost" || net.ParseIP(publicURL.Hostname()).IsLoopback())) {
		return nil, "", errors.New("PUSHUP_PUBLIC_URL must use HTTPS")
	}
	if host == "" {
		return nil, strings.TrimRight(publicURL.String(), "/"), nil
	}
	port := os.Getenv("PUSHUP_SMTP_PORT")
	if port == "" {
		port = "587"
	}
	if port != "587" && port != "465" {
		return nil, "", errors.New("PUSHUP_SMTP_PORT must be 587 (STARTTLS) or 465 (TLS)")
	}
	from, err := normalizeAccountEmail(os.Getenv("PUSHUP_SMTP_FROM"))
	if err != nil {
		return nil, "", errors.New("invalid PUSHUP_SMTP_FROM")
	}
	username := os.Getenv("PUSHUP_SMTP_USER")
	if username == "" || strings.ContainsAny(username, "\r\n") {
		return nil, "", errors.New("PUSHUP_SMTP_USER is required")
	}
	password := os.Getenv("PUSHUP_SMTP_PASSWORD")
	if file := os.Getenv("PUSHUP_SMTP_PASSWORD_FILE"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, "", errors.New("could not read PUSHUP_SMTP_PASSWORD_FILE")
		}
		password = strings.TrimRight(string(raw), "\r\n")
	}
	if password == "" {
		return nil, "", errors.New("SMTP password is required")
	}
	return &smtpMailer{host: host, port: port, username: username, password: password, from: from}, strings.TrimRight(publicURL.String(), "/"), nil
}

func (m *smtpMailer) Send(ctx context.Context, recipient, subject, body string) error {
	to, err := normalizeAccountEmail(recipient)
	if err != nil {
		return err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("invalid message subject")
	}
	address := net.JoinHostPort(m.host, m.port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return errors.New("SMTP deadline failed")
	}
	tlsConfig := &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}
	if m.port == "465" {
		secured := tls.Client(conn, tlsConfig)
		if err = secured.HandshakeContext(ctx); err != nil {
			return errors.New("SMTP TLS failed")
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return errors.New("SMTP greeting failed")
	}
	defer client.Close()
	if m.port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server requires STARTTLS support")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP TLS failed")
		}
	}
	ok, mechanisms := client.Extension("AUTH")
	if !ok {
		return errors.New("SMTP authentication unavailable")
	}
	var auth smtp.Auth
	for _, mechanism := range strings.Fields(mechanisms) {
		if mechanism == "PLAIN" {
			auth = smtp.PlainAuth("", m.username, m.password, m.host)
			break
		}
		if mechanism == "LOGIN" {
			auth = &smtpLoginAuth{username: m.username, password: m.password}
		}
	}
	if auth == nil {
		return errors.New("SMTP authentication mechanism unsupported")
	}
	if err = client.Auth(auth); err != nil {
		return errors.New("SMTP authentication failed")
	}
	if err = client.Mail(m.from); err != nil {
		return errors.New("SMTP sender rejected")
	}
	if err = client.Rcpt(to); err != nil {
		return errors.New("SMTP recipient rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP message rejected")
	}
	message := "From: " + m.from + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n") + "\r\n"
	if _, err = io.WriteString(writer, message); err != nil {
		return errors.New("SMTP write failed")
	}
	if err = writer.Close(); err != nil {
		return errors.New("SMTP delivery failed")
	}
	if err = client.Quit(); err != nil {
		return errors.New("SMTP close failed")
	}
	return nil
}

// Some transactional mail providers offer LOGIN without PLAIN. Credentials
// are only offered after the TLS connection has been authenticated.
type smtpLoginAuth struct {
	username, password string
	step               int
}

func (a *smtpLoginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("SMTP LOGIN requires TLS")
	}
	a.step = 0
	return "LOGIN", nil, nil
}
func (a *smtpLoginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected SMTP LOGIN challenge %s", strconv.Itoa(a.step))
	}
}
