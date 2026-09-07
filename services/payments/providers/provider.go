// Package providers contains the payment-provider boundary. The payments
// application owns the instruction and reconciliation records; providers own
// only submission and the provider's reported outcome.
package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Pending means the provider accepted the request without settling it.
	Pending = "pending"
	// Failed means the provider rejected the request.
	Failed = "failed"
	// Confirmed means the provider reports terminal settlement.
	Confirmed = "confirmed"
)

// Request is the provider-neutral transfer request. IdempotencyKey is stable
// for the life of an instruction, so provider retries cannot create a second
// transfer.
type Request struct {
	IdempotencyKey string
	InstructionID  string
	ContextID      string
	Reference      string
	AmountMinor    int64
	Currency       string
	Destination    string
}

// Response is the only provider result payments consumes. Confirmed means
// the provider claims terminal settlement and must carry a reference and the
// settled amount; pending means accepted but not settled; failed means the
// provider rejected the request.
type Response struct {
	IdempotencyKey     string `json:"idempotency_key"`
	InstructionID      string `json:"instruction_id"`
	ContextID          string `json:"context_id,omitempty"`
	TransferID         string `json:"transfer_id"`
	Reference          string `json:"reference"`
	Status             string `json:"status"`
	AmountMinor        *int64 `json:"amount_minor"`
	SettledAmountMinor *int64 `json:"settled_amount_minor"`
	Currency           string `json:"currency"`
	SettledCurrency    string `json:"settled_currency"`
}

// Provider submits one transfer. A non-nil error describes transport or
// provider HTTP failure; the response may still contain a rejection, but a
// failed HTTP response can never establish Confirmed.
type Provider interface {
	Submit(context.Context, Request) (Response, error)
}

// ValidateRequest checks the required fields of a provider transfer request.
func ValidateRequest(req Request) error {
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.InstructionID) == "" {
		return errors.New("provider request needs an idempotency key and instruction id")
	}
	if req.AmountMinor <= 0 || strings.TrimSpace(req.Currency) == "" || strings.TrimSpace(req.Destination) == "" {
		return errors.New("provider request needs a positive amount, currency and destination")
	}
	return nil
}

// ValidateResponse checks that a provider response belongs to the request.
func ValidateResponse(req Request, resp Response) error {
	if resp.IdempotencyKey != "" && resp.IdempotencyKey != req.IdempotencyKey {
		return fmt.Errorf("provider response idempotency key %q does not match %q", resp.IdempotencyKey, req.IdempotencyKey)
	}
	if resp.InstructionID != "" && resp.InstructionID != req.InstructionID {
		return fmt.Errorf("provider response instruction id %q does not match %q", resp.InstructionID, req.InstructionID)
	}
	return nil
}

func settledAmount(resp Response) (int64, string, bool) {
	amount := resp.SettledAmountMinor
	if amount == nil {
		amount = resp.AmountMinor
	}
	currency := resp.SettledCurrency
	if currency == "" {
		currency = resp.Currency
	}
	if amount == nil || strings.TrimSpace(currency) == "" {
		return 0, "", false
	}
	return *amount, currency, true
}

// SettledAmount is shared by the application and provider contract tests.
func SettledAmount(resp Response) (int64, string, bool) { return settledAmount(resp) }

// Clock is the small dependency the simulator needs; it keeps the provider
// package independent of the domain clock implementation.
type Clock func() time.Time
