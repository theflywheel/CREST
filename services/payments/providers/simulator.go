package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

// Simulator is a durable local provider. Submission creates a PENDING row and
// never claims settlement. Tests may call Settle explicitly to model a later
// provider callback without introducing a fake success on submit.
type Simulator struct {
	db  store.Querier
	now Clock
}

// NewSimulator returns a durable development payment simulator.
func NewSimulator(db store.Querier, now Clock) *Simulator {
	if now == nil {
		now = clock.System{}.Now
	}
	return &Simulator{db: db, now: now}
}

// Submit records a pending transfer and returns its current simulator state.
func (s *Simulator) Submit(ctx context.Context, req Request) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
	}
	var out Response
	var amount, settled *int64
	var currency, storedContext, storedReference, destination, state string
	var settlementReference, settledCurrency *string
	err := s.db.QueryRow(ctx, `
		INSERT INTO payment_simulator_transfers
		  (idempotency_key, instruction_id, context_id, reference, amount_minor, currency, destination, state, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',$8)
		ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING instruction_id, context_id, reference, amount_minor, currency, destination, state,
		          settlement_reference, settled_amount_minor, settled_currency`,
		req.IdempotencyKey, req.InstructionID, req.ContextID, req.Reference, req.AmountMinor, req.Currency, req.Destination, s.now()).Scan(
		&out.InstructionID, &storedContext, &storedReference, &amount, &currency, &destination, &state, &settlementReference, &settled, &settledCurrency)
	if err != nil {
		return Response{}, err
	}
	if out.InstructionID != req.InstructionID || storedContext != req.ContextID || storedReference != req.Reference || amount == nil || *amount != req.AmountMinor || currency != req.Currency || destination != req.Destination {
		return Response{}, fmt.Errorf("simulator idempotency key was reused with a different request")
	}
	out.IdempotencyKey, out.ContextID, out.Reference, out.AmountMinor, out.Currency, out.Status = req.IdempotencyKey, storedContext, storedReference, amount, currency, state
	if settlementReference != nil {
		out.Reference = *settlementReference
	}
	out.SettledAmountMinor = settled
	if settledCurrency != nil {
		out.SettledCurrency = *settledCurrency
	}
	return out, nil
}

// Settle is an explicit development/test operation, never called by Submit.
func (s *Simulator) Settle(ctx context.Context, contextID string, idempotencyKey string, reference string, amount int64, currency string) (Response, error) {
	if strings.TrimSpace(contextID) == "" || strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(reference) == "" || amount <= 0 || strings.TrimSpace(currency) == "" {
		return Response{}, errors.New("settlement needs a context, idempotency key, reference, positive amount and currency")
	}
	var out Response
	var storedAmount int64
	var storedCurrency, state, storedReference string
	var settlementReference, settledCurrency *string
	err := s.db.QueryRow(ctx, `UPDATE payment_simulator_transfers
		SET state='confirmed', settlement_reference=$3, settled_amount_minor=$4, settled_currency=$5
		WHERE context_id=$1 AND idempotency_key=$2 AND state='pending' AND amount_minor=$4 AND currency=$5
		RETURNING instruction_id, reference, settlement_reference, amount_minor, currency, state,
		          settled_amount_minor, settled_currency`, contextID, idempotencyKey, reference, amount, currency).Scan(
		&out.InstructionID, &storedReference, &settlementReference, &storedAmount, &storedCurrency, &state,
		&out.SettledAmountMinor, &settledCurrency)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return Response{}, err
		}
		// A concurrent request may have settled the transfer between the
		// conditional UPDATE and this read. Return that same terminal result
		// when the settlement tuple is identical; this makes the development
		// action safely idempotent across retries.
		err = s.db.QueryRow(ctx, `SELECT instruction_id, reference, settlement_reference,
			amount_minor, currency, state, settled_amount_minor, settled_currency
			FROM payment_simulator_transfers WHERE context_id=$1 AND idempotency_key=$2`, contextID, idempotencyKey).Scan(
			&out.InstructionID, &storedReference, &settlementReference, &storedAmount, &storedCurrency, &state,
			&out.SettledAmountMinor, &settledCurrency)
		if err != nil {
			return Response{}, err
		}
		if state != Confirmed || settlementReference == nil || *settlementReference != reference || out.SettledAmountMinor == nil || *out.SettledAmountMinor != amount || settledCurrency == nil || *settledCurrency != currency {
			return Response{}, errors.New("simulator settlement does not match the submitted transfer")
		}
	}
	if storedAmount != amount || storedCurrency != currency {
		return Response{}, errors.New("simulator settlement amount does not match the submitted request")
	}
	if settlementReference == nil || *settlementReference != reference || out.SettledAmountMinor == nil || *out.SettledAmountMinor != amount || settledCurrency == nil || *settledCurrency != currency {
		return Response{}, errors.New("simulator settlement does not match the submitted transfer")
	}
	out.IdempotencyKey, out.ContextID, out.Reference, out.AmountMinor, out.Currency, out.Status = idempotencyKey, contextID, reference, &storedAmount, storedCurrency, state
	out.SettledCurrency = currency
	return out, nil
}
