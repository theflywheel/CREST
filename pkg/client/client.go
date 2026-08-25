// Package client is how one CREST service calls another.
//
// Services talk over HTTP, exactly as they do in production — importing another
// service's package makes a distributed system that only works when it isn't
// distributed, which depguard enforces. This is the small amount of code that
// makes doing it properly no harder than doing it wrong.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to one service.
type Client struct {
	base string
	http *http.Client
}

// New builds a client for a base URL such as http://parties:8080.
//
// The timeout is on the client rather than per-call because a call with no
// timeout is a goroutine that never returns, and one of those inside a payment
// path is a payment that is neither made nor reported.
func New(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 15 * time.Second}}
}

// Status is a non-2xx response, kept as a value so callers can branch on the
// code — 404 from resolve means "unclear queue", 409 means "a hold", and both
// are ordinary outcomes rather than failures.
type Status struct {
	Code int
	Body string
	URL  string
}

func (e *Status) Error() string {
	return fmt.Sprintf("%s: %d %s", e.URL, e.Code, e.Body)
}

// Code returns the HTTP status of err, or 0 if it is not a Status.
func Code(err error) int {
	var s *Status
	if ok := asStatus(err, &s); ok {
		return s.Code
	}
	return 0
}

// Do sends a request and decodes a JSON response into out, which may be nil.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Status{Code: resp.StatusCode, Body: string(raw), URL: c.base + path}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", c.base+path, err)
	}
	return nil
}

// Get is Do with no body.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post is Do with a body.
func (c *Client) Post(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPost, path, in, out)
}

// PostRaw sends bytes with a content type — for the CSV a batch upload carries,
// which is not JSON and should not be pretending to be.
func (c *Client) PostRaw(ctx context.Context, path, contentType string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Status{Code: resp.StatusCode, Body: string(raw), URL: c.base + path}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func asStatus(err error, target **Status) bool {
	for err != nil {
		if s, ok := err.(*Status); ok { //nolint:errorlint // this is the unwrap loop
			*target = s
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
