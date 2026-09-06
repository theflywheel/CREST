package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func request() Request {
	return Request{IdempotencyKey: "instruction-1", InstructionID: "instruction-1", Reference: "claim-1", AmountMinor: 125, Currency: "KES", Destination: "did:crest:party:worker"}
}

func TestHTTPProviderCarriesPendingWithoutCallingItSettled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["idempotency_key"] != "instruction-1" {
			t.Fatalf("provider request = %#v, err=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{IdempotencyKey: "instruction-1", InstructionID: "instruction-1", Status: Pending})
	}))
	defer server.Close()
	p, err := NewHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Submit(context.Background(), request())
	if err != nil || got.Status != Pending {
		t.Fatalf("pending provider result = %+v, err=%v", got, err)
	}
	if _, _, ok := SettledAmount(got); ok {
		t.Fatal("a pending response supplied no settlement amount but was considered settled")
	}
}

func TestHTTPProviderNeverAcceptsConfirmedOnFailedHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(Response{
			IdempotencyKey: "instruction-1", InstructionID: "instruction-1", Reference: "rail-1",
			Status: Confirmed, SettledAmountMinor: ptr(125), SettledCurrency: "KES",
		})
	}))
	defer server.Close()
	p, err := NewHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Submit(context.Background(), request())
	if err == nil {
		t.Fatal("failed HTTP response was accepted")
	}
	var httpErr *HTTPError
	if !asHTTPError(err, &httpErr) || httpErr.Code != http.StatusBadGateway || got.Status != Confirmed {
		t.Fatalf("failed HTTP result = %+v, err=%v", got, err)
	}
}

func TestCatalogueKeepsSimulatorDevelopmentOnly(t *testing.T) {
	catalogue := NewCatalogue()
	names := catalogue.Names()
	if len(names) != 2 || names[0] != "http" || names[1] != "simulator" {
		t.Fatalf("catalogue names = %v, want stable sorted built-ins", names)
	}
	if _, err := catalogue.Build(Config{Name: "unknown", Env: "local"}); err == nil {
		t.Fatal("unknown provider was accepted")
	}
	if _, err := catalogue.Build(Config{Name: "simulator", Env: "production"}); err == nil || !strings.Contains(err.Error(), "local or development") {
		t.Fatalf("production simulator configuration error = %v", err)
	}
}

func TestCatalogueRegisterRejectsDuplicateAndBuildsExtension(t *testing.T) {
	catalogue := NewCatalogue()
	factory := func(Config) (Provider, error) {
		return providerFunc(func(context.Context, Request) (Response, error) { return Response{Status: Pending}, nil }), nil
	}
	if err := catalogue.Register(" Example ", factory); err != nil {
		t.Fatal(err)
	}
	if err := catalogue.Register("example", factory); err == nil {
		t.Fatal("catalogue accepted duplicate provider name")
	}
	if _, err := catalogue.Build(Config{Name: "EXAMPLE"}); err != nil {
		t.Fatalf("registered provider was not buildable: %v", err)
	}
}

type providerFunc func(context.Context, Request) (Response, error)

func (f providerFunc) Submit(ctx context.Context, req Request) (Response, error) {
	return f(ctx, req)
}

func TestProviderResponseCannotPointAtAnotherInstruction(t *testing.T) {
	if err := ValidateResponse(request(), Response{InstructionID: "instruction-2"}); err == nil {
		t.Fatal("response for another instruction was accepted")
	}
	if err := ValidateResponse(request(), Response{IdempotencyKey: "instruction-1", InstructionID: "instruction-1"}); err != nil {
		t.Fatalf("matching provider identity was rejected: %v", err)
	}
}

func ptr(v int64) *int64 { return &v }

func asHTTPError(err error, out **HTTPError) bool {
	var e *HTTPError
	if errors.As(err, &e) {
		*out = e
		return true
	}
	return false
}
