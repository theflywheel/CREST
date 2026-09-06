package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
)

func TestRailStatusesAreSeparatedBySettlementMeaning(t *testing.T) {
	cases := map[string]string{
		"confirmed": "confirmed", "cleared": "", "SETTLED": "",
		"pending": "pending", "processing": "pending", "queued": "pending",
		"rejected": "failed", "declined": "failed", "FAILED": "failed",
		"": "", "unknown": "",
	}
	for status, want := range cases {
		if got := normalizeRailStatus(status); got != want {
			t.Errorf("normalizeRailStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestSettledAmountUsesRailReportedValue(t *testing.T) {
	amount, currency, ok := settledAmount(railReply{
		AmountMinor: ptrInt64(100), SettledAmountMinor: ptrInt64(97), Currency: "KES",
		SettledCurrency: "KES",
	})
	if !ok || amount != 97 || currency != "KES" {
		t.Fatalf("settled amount = (%d, %q, %v), want (97, KES, true)", amount, currency, ok)
	}
}

func TestSettledAmountCannotBeInferredFromInstruction(t *testing.T) {
	_, _, ok := settledAmount(railReply{})
	if ok {
		t.Fatal("a confirmation without a rail amount was treated as settled")
	}
}

func TestRailReplyMustIdentifyRequestedInstruction(t *testing.T) {
	in := Instruction{ID: "payment-instruction:1"}
	for name, reply := range map[string]railReply{
		"idempotency key": {IdempotencyKey: in.ID},
		"instruction id":  {InstructionID: in.ID},
		"transfer id":     {TransferID: in.ID},
		"reference alone is not an instruction id": {Reference: in.ID},
		"claim reference is not enough":            {Reference: "claim:1"},
		"different instruction":                    {InstructionID: "payment-instruction:2"},
	} {
		t.Run(name, func(t *testing.T) {
			want := name == "idempotency key" || name == "instruction id" || name == "transfer id"
			if got := railReplyMatchesInstruction(reply, in); got != want {
				t.Fatalf("railReplyMatchesInstruction(%+v) = %v, want %v", reply, got, want)
			}
		})
	}
}

func TestPricingUsesWorkPeriodStartAndPersistsRateLink(t *testing.T) {
	workStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/units/unit-1":
			_ = json.NewEncoder(w).Encode(schema.Unit{
				ID: "unit-1", Definition: schema.VersionedRef{ID: "definition-1", Version: 1},
				Outcome: schema.Outcome{Unit: "visit", Value: 2},
				Period:  schema.Period{Start: workStart},
			})
		case "/v1/definitions/definition-1/linked-records":
			_ = json.NewEncoder(w).Encode(map[string]any{"linkedRecords": []schema.LinkedRecord{
				paymentRateRecord("rate-1", 1, workStart, 100),
				paymentRateRecord("rate-2", 2, workStart.AddDate(0, 1, 0), 200),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	h := &handlers{evidence: client.New(server.URL), definitions: client.New(server.URL), payerOwner: "payer"}
	amount, currency, held := h.amountFor(context.Background(), "unit-1", workStart.AddDate(0, 2, 0))
	if held != nil || amount != 200 || currency != "KES" {
		t.Fatalf("amountFor = (%d, %q, %v), want (200, KES, nil) at work-period start", amount, currency, held)
	}
	pricing := h.priceFor(context.Background(), "unit-1")
	if pricing.RateRecordID != "rate-1" || pricing.RateVersion != 1 || !pricing.PricingAt.Equal(workStart) {
		t.Fatalf("pricing audit = (%q, %d, %s), want (rate-1, 1, %s)", pricing.RateRecordID, pricing.RateVersion, pricing.PricingAt, workStart)
	}
}

func paymentRateRecord(id string, version int, effective time.Time, amount int) schema.LinkedRecord {
	return schema.LinkedRecord{ID: id, Version: version, Type: "payment-setup", State: "ACTIVE", Payload: map[string]any{
		"effectiveFrom": effective,
		"payerPartyId":  "payer",
		"ratePerOutcomeUnit": map[string]any{
			"amountMinor": amount,
			"currency":    "KES",
		},
	}}
}

func TestPricedHoldRetryPreservesOriginalRateLink(t *testing.T) {
	in := Instruction{ID: "instruction-1", AmountMinor: 125, Currency: "KES", RateRecordID: "rate-old", RateVersion: 3}
	in.Held = &HeldReason{Code: "mechanism_not_live", OwnerPartyID: "payer"}
	if canRepriceHeld(in) {
		t.Fatal("a priced mechanism hold was eligible for rate repricing")
	}
	if in.AmountMinor != 125 || in.Currency != "KES" || in.RateRecordID != "rate-old" || in.RateVersion != 3 {
		t.Fatal("priced hold audit fields changed before retry")
	}

	missing := Instruction{ID: "instruction-2", Held: &HeldReason{Code: "no_rate_attached"}}
	if !canRepriceHeld(missing) {
		t.Fatal("missing-rate hold was not eligible for work-date resolution")
	}
	unreadable := Instruction{ID: "instruction-3", Held: &HeldReason{Code: "rate_unreadable"}}
	if canRepriceHeld(unreadable) {
		t.Fatal("unreadable-rate hold was eligible for automatic repricing")
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestPrivateFinanceReadRejectsAnonymousCallerInLocalMode(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/reconciliation?contextId=ctx-private", nil)
	rr := httptest.NewRecorder()
	d := service.Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Permits: func(context.Context, string, string, string) (bool, error) {
		return true, nil
	}}
	if authorizePaymentOperations(rr, req, d, "ctx-private") {
		t.Fatal("anonymous caller was authorized for a private finance report")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous finance read status = %d, want 401", rr.Code)
	}
}
