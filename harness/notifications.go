package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AcknowledgeNotification completes the development notification journey for
// a real worker caller. The transport's acceptance is only an inbox result;
// this method extracts its review token and submits the public acknowledgement
// endpoint with the worker bearer supplied by the caller.
func (s *Stack) AcknowledgeNotification(ctx context.Context, claimID string, worker Caller) error {
	poll := env("NOTIFY_POLL_URL", "http://localhost:59104/messages")
	deadline := time.Now().Add(20 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			poll+"?claimId="+url.QueryEscape(claimID), nil)
		if err != nil {
			return err
		}
		if token := env("NOTIFY_HTTP_TOKEN", "dev-notify-token"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := s.http.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if readErr != nil {
				return readErr
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("notification inbox returned HTTP %d: %s", resp.StatusCode, body)
			}
			var inbox struct {
				Messages []struct {
					ClaimID        string `json:"claimId"`
					Acknowledgment string `json:"acknowledgmentUrl"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &inbox); err != nil {
				return err
			}
			for _, msg := range inbox.Messages {
				if msg.ClaimID != claimID || msg.Acknowledgment == "" {
					continue
				}
				u, err := url.Parse(msg.Acknowledgment)
				if err != nil {
					return err
				}
				fragment, err := url.Parse("http://notify" + u.Fragment)
				if err != nil {
					return err
				}
				token := fragment.Query().Get("token")
				if token == "" {
					return fmt.Errorf("notification for %s has no acknowledgement token", claimID)
				}
				return s.Confirmation.As(worker).Post(ctx,
					"/v1/windows/"+url.PathEscape(claimID)+"/ack?token="+url.QueryEscape(token), nil, nil)
			}
		} else if time.Now().After(deadline) {
			return fmt.Errorf("notification for %s was not accepted: %w", claimID, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("notification for %s did not reach the development inbox", claimID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
