// Package harness drives a running CREST stack through its real interfaces.
//
// Two rules from docs/TESTING.md shape everything here. It talks HTTP and CLI
// only — a test that writes to the database is testing the database. And it
// moves the clock rather than waiting: the confirmation window is seven days,
// and a suite that sleeps through one is a suite nobody runs.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Stack is a running deployment.
type Stack struct {
	Registry     *Service
	Definitions  *Service
	Evidence     *Service
	Confirmation *Service
	Verification *Service
	Payments     *Service
	Notify       *Service

	SMS  *Service
	Rail *Service

	http *http.Client
}

// Service is one addressable service.
type Service struct {
	Name string
	Base string
	http *http.Client
}

// New builds a Stack from the environment, defaulting to the ports in
// infra/compose/docker-compose.yml.
func New() *Stack {
	c := &http.Client{Timeout: 20 * time.Second}
	svc := func(name, def string) *Service {
		return &Service{Name: name, Base: env(name, def), http: c}
	}
	return &Stack{
		Registry:     svc("REGISTRY_URL", "http://localhost:59001"),
		Definitions:  svc("DEFINITIONS_URL", "http://localhost:59002"),
		Evidence:     svc("EVIDENCE_URL", "http://localhost:59003"),
		Confirmation: svc("CONFIRMATION_URL", "http://localhost:59004"),
		Verification: svc("VERIFICATION_URL", "http://localhost:59005"),
		Payments:     svc("PAYMENTS_URL", "http://localhost:59006"),
		Notify:       svc("NOTIFY_URL", "http://localhost:59007"),
		SMS:          svc("SMS_URL", "http://localhost:59101"),
		Rail:         svc("RAIL_URL", "http://localhost:59102"),
		http:         c,
	}
}

// Services is every CREST service, for the operations that apply to all of them.
func (s *Stack) Services() []*Service {
	return []*Service{s.Registry, s.Definitions, s.Evidence, s.Confirmation,
		s.Verification, s.Payments, s.Notify}
}

// WaitReady polls /readyz until every service answers, or gives up.
//
// Polling, never sleeping (docs/TESTING.md): a fixed sleep is either too short
// on a loaded CI runner or wasted on a fast one, and the first case is a flaky
// suite that gets ignored within a week.
func (s *Stack) WaitReady(ctx context.Context, within time.Duration) error {
	deadline := time.Now().Add(within)
	for _, svc := range append(s.Services(), s.SMS, s.Rail) {
		path := "/readyz"
		if svc == s.SMS || svc == s.Rail {
			path = "/healthz" // the mocks have no dependencies to be ready for
		}
		for {
			err := svc.Get(ctx, path, nil)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s never became ready at %s: %w", svc.Name, svc.Base, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return nil
}

// SetClock moves every service's clock to the same instant.
//
// Every service, together: they compare timestamps with each other, and a stack
// where evidence is on Tuesday and confirmation is on Friday produces results
// that are nobody's design.
func (s *Stack) SetClock(ctx context.Context, at time.Time) error {
	for _, svc := range s.Services() {
		if err := svc.Post(ctx, "/internal/clock", map[string]any{"now": at}, nil); err != nil {
			return fmt.Errorf("%s: %w", svc.Name, err)
		}
	}
	return nil
}

// Advance moves every service's clock forward by d.
func (s *Stack) Advance(ctx context.Context, d time.Duration) error {
	for _, svc := range s.Services() {
		if err := svc.Post(ctx, "/internal/clock", map[string]any{"advance": d.String()}, nil); err != nil {
			return fmt.Errorf("%s: %w", svc.Name, err)
		}
	}
	return nil
}

// Now reads a service's clock.
func (svc *Service) Now(ctx context.Context) (time.Time, error) {
	var out struct {
		Now time.Time `json:"now"`
	}
	err := svc.Get(ctx, "/internal/clock", &out)
	return out.Now, err
}

// Reset clears the mocks between scenarios, so one scenario's messages are
// never another's evidence.
func (s *Stack) Reset(ctx context.Context) error {
	for _, m := range []*Service{s.SMS, s.Rail} {
		if err := m.Post(ctx, "/reset", nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// HTTPError is a non-2xx response, with the body, because a harness failure
// that says only "500" costs twenty minutes.
type HTTPError struct {
	Service string
	Method  string
	Path    string
	Code    int
	Body    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s %s -> %d: %s", e.Service, e.Method, e.Path, e.Code, e.Body)
}

// Get performs a GET, decoding into out when out is non-nil.
func (svc *Service) Get(ctx context.Context, path string, out any) error {
	return svc.do(ctx, http.MethodGet, path, "", nil, out)
}

// Post performs a JSON POST.
func (svc *Service) Post(ctx context.Context, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	return svc.do(ctx, http.MethodPost, path, "application/json", body, out)
}

// PostRaw posts bytes with a content type — the CSV a batch upload carries.
func (svc *Service) PostRaw(ctx context.Context, path, contentType string, body []byte, out any) error {
	return svc.do(ctx, http.MethodPost, path, contentType, body, out)
}

// Status performs a request and returns the status code instead of an error,
// for the endpoints where a 404 or a 409 is the designed answer rather than a
// failure — resolve returning a hold, for instance.
func (svc *Service) Status(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var body []byte
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, svc.Base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := svc.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func (svc *Service) do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, svc.Base+path, reader)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := svc.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Service: svc.Name, Method: method, Path: path,
			Code: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Kill stops a container the way a crash does, then brings it back.
//
// SIGKILL rather than a graceful stop, and that distinction is the whole point.
// A service asked politely to shut down could drain its outbox on the way out,
// and a test built on that proves the drain works rather than that the outbox
// does. What has to survive is the case nobody gets to handle: the process is
// gone between the COMMIT that recorded a state change and the call that was
// supposed to act on it.
//
// It shells out to compose because the harness has no other handle on a
// container, and because "docker compose kill" is exactly what an operator
// would reach for.
func Kill(ctx context.Context, service string) error {
	return compose(ctx, "kill", "-s", "SIGKILL", service)
}

// Start brings a killed service back up.
func Start(ctx context.Context, service string) error {
	return compose(ctx, "up", "-d", "--no-deps", service)
}

func compose(ctx context.Context, args ...string) error {
	// The compose file declares `name: crest`, so the project name comes from
	// the file rather than from where this happens to be run.
	file := env("COMPOSE_FILE", "infra/compose/docker-compose.yml")
	full := append([]string{"compose", "-f", file}, args...)

	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = env("CREST_ROOT", "../..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w: %s", strings.Join(full, " "), err, out)
	}
	return nil
}
