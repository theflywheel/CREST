package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProvider speaks the provider contract over POST /instructions. It
// retains a decoded response even for a non-2xx response so explicit provider
// rejection can be recorded, while callers still receive the HTTP error.
type HTTPProvider struct {
	base string
	http *http.Client
}

// NewHTTP returns an HTTP payment provider for rawURL.
func NewHTTP(rawURL string) (*HTTPProvider, error) {
	base := strings.TrimSpace(rawURL)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("payment provider URL must be an HTTP(S) URL without credentials")
	}
	return &HTTPProvider{base: strings.TrimRight(base, "/"), http: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Submit sends one transfer request to the HTTP payment provider.
func (p *HTTPProvider) Submit(ctx context.Context, req Request) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(map[string]any{
		"idempotency_key": req.IdempotencyKey,
		"instruction_id":  req.InstructionID,
		"context_id":      req.ContextID,
		"reference":       req.Reference,
		"amount_minor":    req.AmountMinor,
		"currency":        req.Currency,
		"destination":     req.Destination,
	})
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/instructions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return Response{}, readErr
	}
	var out Response
	decodeErr := json.Unmarshal(raw, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, &HTTPError{Code: resp.StatusCode, Body: string(raw), URL: httpReq.URL.String(), DecodeErr: decodeErr}
	}
	if decodeErr != nil {
		return Response{}, fmt.Errorf("decode provider response: %w", decodeErr)
	}
	if err := ValidateResponse(req, out); err != nil {
		return out, err
	}
	return out, nil
}

// HTTPError reports a non-successful response from an HTTP payment provider.
type HTTPError struct {
	Code      int
	Body      string
	URL       string
	DecodeErr error
}

func (e *HTTPError) Error() string {
	if e.DecodeErr != nil {
		return fmt.Sprintf("provider HTTP %d: %v", e.Code, e.DecodeErr)
	}
	return fmt.Sprintf("provider HTTP %d: %s", e.Code, e.Body)
}
