// Package notify provides real notification transports. It deliberately has
// no successful log-only or SMS fallback: a delivery result exists only when
// the configured transport accepts the message.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// Message is the notification payload sent to a configured transport.
type Message struct {
	To             string `json:"to"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	Acknowledgment string `json:"acknowledgmentUrl"`
}

// Result records the provider's acceptance of a notification.
type Result struct {
	Accepted   bool
	ProviderID string
}

// Sender delivers notifications through a configured provider.
type Sender interface {
	Send(context.Context, Message) (Result, error)
}

// SMTPConfig configures an authenticated STARTTLS SMTP transport.
type SMTPConfig struct {
	Addr     string
	From     string
	Username string
	Password string
}

// SMTP sends notifications over STARTTLS SMTP.
type SMTP struct{ cfg SMTPConfig }

// NewSMTP validates and constructs an SMTP sender.
func NewSMTP(cfg SMTPConfig) (*SMTP, error) {
	if strings.TrimSpace(cfg.Addr) == "" || strings.TrimSpace(cfg.From) == "" {
		return nil, errors.New("smtp notification requires addr and from")
	}
	return &SMTP{cfg: cfg}, nil
}

// Send delivers one message and succeeds only after SMTP accepts its body.
func (s *SMTP) Send(ctx context.Context, msg Message) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(msg.To) == "" {
		return Result{}, errors.New("notification recipient is empty")
	}
	host, _, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		return Result{}, fmt.Errorf("smtp address: %w", err)
	}
	for _, value := range []string{s.cfg.From, msg.To, msg.Subject} {
		if strings.ContainsAny(value, "\r\n") {
			return Result{}, errors.New("mail header contains a newline")
		}
	}
	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return Result{}, err
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return Result{}, err
	}
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Close() }()
	deadline := clock.System{}.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return Result{}, err
	}
	cancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer cancelClose()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return Result{}, errors.New("SMTP server does not offer required STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return Result{}, err
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, host)); err != nil {
			return Result{}, err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return Result{}, err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return Result{}, err
	}
	writer, err := c.Data()
	if err != nil {
		return Result{}, err
	}
	content := []byte("To: " + to.String() + "\r\nFrom: " + from.String() + "\r\nSubject: " + msg.Subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + msg.Body + "\r\n")
	if _, err := writer.Write(content); err != nil {
		return Result{}, err
	}
	if err := writer.Close(); err != nil {
		return Result{}, err
	}
	if err := c.Quit(); err != nil {
		return Result{}, err
	}

	return Result{Accepted: true}, nil
}

// HTTPConfig configures an authenticated HTTP notification transport.
type HTTPConfig struct {
	URL            string
	Token          string
	HTTP           *http.Client
	CallbackSecret string
}

// HTTP sends notifications to an HTTP provider.
type HTTP struct{ cfg HTTPConfig }

// NewHTTP validates and constructs an HTTP sender.
func NewHTTP(cfg HTTPConfig) (*HTTP, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("http notification requires url")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &HTTP{cfg: cfg}, nil
}

// Send delivers one message and requires an explicit provider acceptance.
func (s *HTTP) Send(ctx context.Context, msg Message) (Result, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("notification provider returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Accepted   bool   `json:"accepted"`
		ProviderID string `json:"providerId"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("invalid notification provider response: %w", err)
	}
	if !out.Accepted {
		return Result{}, errors.New("notification provider did not accept the message")
	}
	return Result{Accepted: true, ProviderID: out.ProviderID}, nil
}

// CallbackSignature returns the HMAC signature providers must send with a
// callback body. Payments verifies it before considering any provider event.
func CallbackSignature(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyCallback verifies a provider callback HMAC over its exact body.
func VerifyCallback(secret, signature string, body []byte) bool {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(signature) == "" {
		return false
	}
	want := CallbackSignature(secret, body)
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(want))
}
