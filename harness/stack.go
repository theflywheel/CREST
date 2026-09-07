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
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Stack is a running deployment.
type Stack struct {
	Parties      *Service
	Definitions  *Service
	Evidence     *Service
	Confirmation *Service
	Verification *Service
	Payments     *Service

	Rail *Service

	http       *http.Client
	runtimeMu  sync.RWMutex
	runtimeIDs map[string]string
}

// Service is one addressable service.
type Service struct {
	Name string
	Base string
	http *http.Client

	// headers is what every request from this view carries. A view rather than
	// a mutable field on a shared Service: scenarios run against one stack, and
	// one of them setting "I am now this worker" on a value the others share is
	// the kind of coupling that reads as a product defect for an afternoon
	// before anybody suspects the harness.
	headers http.Header
	resolve func(string) string
	reverse func(string) string
}

// As returns a view of this service that authenticates as a caller (#89).
//
// The zero Caller is an unauthenticated view, which is a thing to test rather
// than an accident: an endpoint that acts in somebody's name must refuse it.
func (svc *Service) As(c Caller) *Service {
	view := *svc
	view.headers = c.header()
	return &view
}

// New builds a Stack from the environment, defaulting to the ports in
// infra/compose/docker-compose.yml.
func New() *Stack {
	c := &http.Client{Timeout: 20 * time.Second}
	svc := func(name, def string) *Service {
		return &Service{Name: name, Base: env(name, def), http: c}
	}
	s := &Stack{
		// The four member names answer from the one core service (#150); the
		// clients keep their names because the questions they ask kept theirs.
		Parties:     svc("PARTIES_URL", "http://localhost:59000"),
		Definitions: svc("DEFINITIONS_URL", "http://localhost:59000"),
		Evidence:    svc("EVIDENCE_URL", "http://localhost:59000"),
		// Confirmation windows answer on the core application; the client keeps
		// the name because the questions it asks kept theirs.
		Confirmation: svc("CONFIRMATION_URL", "http://localhost:59000"),
		Verification: svc("VERIFICATION_URL", "http://localhost:59000"),
		Payments:     svc("PAYMENTS_URL", "http://localhost:59006"),
		Rail:         svc("RAIL_URL", "http://localhost:59102"),
		http:         c,
		runtimeIDs:   make(map[string]string),
	}
	for _, service := range []*Service{s.Parties, s.Definitions, s.Evidence, s.Confirmation, s.Verification, s.Payments, s.Rail} {
		service.resolve = s.resolveRuntimeText
		service.reverse = s.reverseRuntimeText
	}
	return s
}

// SetRuntimeID records a server-assigned identifier for a stable fixture name.
func (s *Stack) SetRuntimeID(fixtureID, runtimeID string) {
	if fixtureID == "" || runtimeID == "" || fixtureID == runtimeID {
		return
	}
	s.runtimeMu.Lock()
	s.runtimeIDs[fixtureID] = runtimeID
	s.runtimeMu.Unlock()
}

func (s *Stack) resolveRuntimeText(text string) string {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	for fixtureID, runtimeID := range s.runtimeIDs {
		text = strings.ReplaceAll(text, url.QueryEscape(fixtureID), url.QueryEscape(runtimeID))
		text = strings.ReplaceAll(text, fixtureID, runtimeID)
	}
	return text
}

func (s *Stack) reverseRuntimeText(text string) string {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	for fixtureID, runtimeID := range s.runtimeIDs {
		text = strings.ReplaceAll(text, runtimeID, fixtureID)
	}
	return text
}

// Services is every CREST service, for the operations that apply to all of them.
func (s *Stack) Services() []*Service {
	// One entry per PROCESS, not per name: Parties/Definitions/Evidence/
	// Verification are one core deployable (#150), so it appears once.
	return []*Service{s.Parties, s.Payments}
}

// WaitReady polls /readyz until every service answers, or gives up.
//
// Polling, never sleeping (docs/TESTING.md): a fixed sleep is either too short
// on a loaded CI runner or wasted on a fast one, and the first case is a flaky
// suite that gets ignored within a week.
func (s *Stack) WaitReady(ctx context.Context, within time.Duration) error {
	deadline := time.Now().Add(within)
	for _, svc := range append(s.Services(), s.Rail) {
		path := "/readyz"
		if svc == s.Rail {
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

// LiveClock puts every service back on real time.
//
// The counterpart to walking a stack forward: the story seeder shifts the
// clock a week into the past and steps through a programme week, and this is
// how it hands the stack back running on the same clock as everyone else. A
// demo stack left frozen at the seeder's last step is one where no window ever
// reaches T=7 again.
func (s *Stack) LiveClock(ctx context.Context) error {
	for _, svc := range s.Services() {
		if err := svc.Post(ctx, "/internal/clock", map[string]any{"live": true}, nil); err != nil {
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
	for _, m := range []*Service{s.Rail} {
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
	path, body = svc.resolveRequest(path, body)
	return svc.do(ctx, http.MethodPost, path, "application/json", body, out)
}

// PostRaw posts bytes with a content type — the CSV a batch upload carries.
func (svc *Service) PostRaw(ctx context.Context, path, contentType string, body []byte, out any) error {
	return svc.do(ctx, http.MethodPost, path, contentType, body, out)
}

// StatusRaw is Status for a non-JSON body — a CSV batch whose refusal is the
// designed answer.
func (svc *Service) StatusRaw(ctx context.Context, method, path, contentType string, body []byte) (int, []byte, error) {
	if svc.resolve != nil {
		path = svc.resolve(path)
	}
	req, err := http.NewRequestWithContext(ctx, method, svc.Base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	svc.apply(req)
	resp, err := svc.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
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
	path, body = svc.resolveRequest(path, body)
	req, err := http.NewRequestWithContext(ctx, method, svc.Base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	svc.apply(req)
	resp, err := svc.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func (svc *Service) do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	if svc.resolve != nil {
		path = svc.resolve(path)
	}
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
	svc.apply(req)
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
	if svc.reverse != nil {
		raw = rewriteJSONIDs(raw, svc.reverse)
	}
	return json.Unmarshal(raw, out)
}

func (svc *Service) resolveRequest(path string, body []byte) (string, []byte) {
	if svc.resolve == nil {
		return path, body
	}
	path = svc.resolve(path)
	if len(body) == 0 {
		return path, body
	}
	return path, rewriteJSONIDs(body, svc.resolve)
}

func rewriteJSONIDs(raw []byte, resolve func(string) string) []byte {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	rewriteJSONValue(&value, resolve)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func rewriteJSONValue(value *any, resolve func(string) string) {
	switch v := (*value).(type) {
	case string:
		*value = resolve(v)
	case []any:
		for i := range v {
			rewriteJSONValue(&v[i], resolve)
		}
	case map[string]any:
		if _, signed := v["proof"]; signed {
			return
		}
		if _, signed := v["signature"]; signed {
			return
		}
		for key, child := range v {
			// Never rewrite ids inside signed material: doing so would make a
			// valid credential or presentation fail verification.
			if key == "credential" || key == "presentation" || key == "proof" || key == "signature" {
				continue
			}
			rewriteJSONValue(&child, resolve)
			v[key] = child
		}
	}
}

// apply copies this view's caller headers onto a request.
func (svc *Service) apply(req *http.Request) {
	for k, vs := range svc.headers {
		for _, v := range vs {
			if strings.EqualFold(k, "X-CREST-On-Behalf-Of") && svc.resolve != nil {
				v = svc.resolve(v)
			}
			req.Header.Add(k, v)
		}
	}
	if strings.HasPrefix(req.URL.Path, "/internal/") {
		if token := os.Getenv("CREST_SERVICE_TOKEN"); token != "" {
			req.Header.Set("X-CREST-Service-Token", token)
		}
	}
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
	return compose(ctx, "start", service)
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
