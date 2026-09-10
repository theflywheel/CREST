package verification

// The verifier pass, and the rate cap that is counted per pass (#27, G1 #9).
//
// Blueprint J9 puts pass issuance at L1: "name + reachable contact, no
// account, no vetting". A pass identifies a stranger who checks credentials
// without onboarding them. It grants nothing — the answer rides the signature
// either way — and it costs one thing: every check is recorded against it, and
// the worker checked can see the name on it (W8).
//
// G1 (#9) settled three rules for checking at volume, and two of them land
// here. A per-pass rate cap, whose number is configuration and whose existence
// is not; and a distinct authorization scope for bulk checking, so that the
// ability to sweep is a decision somebody made and can be seen to have made
// rather than a capacity that comes free with an ordinary pass. The third —
// every check in the worker's trail regardless of how it arrived — is what
// makes the first two auditable, and is what the cap is counted from.
//
// So an online check names who is asking, always: a pass, an onboarded party,
// or the signed-in caller themselves. An anonymous online check would be the
// loop the cap exists to bound, arriving without the thing it is counted per.
// The check that needs nobody's permission is the offline one — the signature
// against the published key — and that one never touches this service (W7).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

const (
	// HeaderPass carries a verifier pass token. Its own header rather than
	// Authorization, because the bearer scheme there is a signed-in caller
	// and a pass is deliberately not one — "no account".
	HeaderPass = "X-CREST-Verifier-Pass"

	// FunctionVerifyBulk is the authorization scope G1 requires for bulk
	// checking. An organisation holds it or does not; a pass never does.
	FunctionVerifyBulk = "verify-credentials-bulk"

	defaultRateCap    = 100
	defaultRateWindow = time.Hour

	passNameMin, passNameMax       = 2, 80
	passContactMin, passContactMax = 5, 120
)

// rateCap is the deployment's per-requester cap: at most Cap checks in any
// Window. The number is L2; that there is one is L1, so an unreadable or
// non-positive value falls back to the default with an error in the log
// rather than to "unlimited".
type rateCap struct {
	Cap    int
	Window time.Duration
}

func loadRateCap(log *slog.Logger) rateCap {
	rc := rateCap{Cap: defaultRateCap, Window: defaultRateWindow}
	if raw := config.Str("CREST_VERIFY_RATE_CAP", ""); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			log.Error("CREST_VERIFY_RATE_CAP is not a positive integer; using the default",
				"value", raw, "default", defaultRateCap)
		} else {
			rc.Cap = n
		}
	}
	window, err := config.PositiveDuration("CREST_VERIFY_RATE_WINDOW", defaultRateWindow)
	if err != nil {
		log.Error("CREST_VERIFY_RATE_WINDOW is unusable; using the default", "error", err, "default", defaultRateWindow)
	}
	rc.Window = window
	return rc
}

// verifierPass is one issued pass. Contact is kept for accountability and is
// never sent to the worker; the name is what they see.
type verifierPass struct {
	ID        string
	Name      string
	Contact   string
	IssuedAt  time.Time
	RotatedAt *time.Time
}

// passToken is the secret handed out once. Its hash is what is stored.
func passToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashPassToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// normaliseContact makes "one pass per contact" mean one per person rather
// than one per spelling: case and internal whitespace do not make a new
// reachable contact.
func normaliseContact(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

var errPassInvalid = errors.New("pass request invalid")

// validatePassRequest is what issuance refuses. A pass with no name puts
// nothing in the worker's trail; one with no reachable contact is not a pass,
// it is a nickname.
func validatePassRequest(name, contact string) (string, string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < passNameMin || n > passNameMax {
		return "", "", fmt.Errorf("%w: name must be %d-%d characters; the worker reads it", errPassInvalid, passNameMin, passNameMax)
	}
	contact = normaliseContact(contact)
	if n := utf8.RuneCountInString(contact); n < passContactMin || n > passContactMax {
		return "", "", fmt.Errorf("%w: contact must be %d-%d characters", errPassInvalid, passContactMin, passContactMax)
	}
	digits := 0
	for _, r := range contact {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if !strings.Contains(contact, "@") && digits < 7 {
		return "", "", fmt.Errorf("%w: contact must be an email address or a phone number somebody can reach you at", errPassInvalid)
	}
	return name, contact, nil
}

// issuePass is POST /v1/verifier-passes. Open by design: it is how a stranger
// stops being anonymous, so it cannot ask them to already not be.
func (h *handlers) issuePass(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Contact string `json:"contact"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	name, contact, err := validatePassRequest(req.Name, req.Contact)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_pass_request", "%s", strings.TrimPrefix(err.Error(), errPassInvalid.Error()+": "))
		return
	}
	token, err := passToken()
	if err != nil {
		httpx.Fail(w, h.d.Log, "mint a pass token", err)
		return
	}
	now := time.Now().UTC()
	pass := verifierPass{ID: id.New("pass"), Name: name, Contact: contact, IssuedAt: now}
	rotated := false
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		// One pass per contact. Asking again with the same contact rotates the
		// token and updates the name; it never mints a second identity, since
		// a cap per pass would otherwise be a cap per request for a pass.
		var existing verifierPass
		err := tx.QueryRow(r.Context(), `
			SELECT id, issued_at FROM verifier_passes WHERE contact = $1`, contact).
			Scan(&existing.ID, &existing.IssuedAt)
		switch {
		case err == nil:
			rotated = true
			pass.ID, pass.IssuedAt, pass.RotatedAt = existing.ID, existing.IssuedAt, &now
			_, err = tx.Exec(r.Context(), `
				UPDATE verifier_passes
				SET name = $2, token_hash = $3, rotated_at = $4, revoked_at = NULL
				WHERE id = $1`, pass.ID, pass.Name, hashPassToken(token), now)
			return err
		case errors.Is(err, store.ErrNotFound):
			_, err = tx.Exec(r.Context(), `
				INSERT INTO verifier_passes (id, name, contact, token_hash, issued_at)
				VALUES ($1, $2, $3, $4, $5)`, pass.ID, pass.Name, pass.Contact, hashPassToken(token), now)
			return err
		default:
			return err
		}
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "issue a verifier pass", err)
		return
	}
	h.d.Log.Info("verifier pass issued", "pass", pass.ID, "rotated", rotated)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"pass": map[string]any{
			"id": pass.ID, "name": pass.Name, "issuedAt": pass.IssuedAt,
			"rotated": rotated,
		},
		// Shown once. What is stored is its hash.
		"token":   token,
		"header":  HeaderPass,
		"rateCap": map[string]any{"checks": h.rate.Cap, "window": h.rate.Window.String()},
	})
}

var errNoPass = errors.New("no such pass")

// passFromRequest resolves the pass a request presents, if any. (zero, nil)
// means no pass header; an unknown or revoked token is an error.
func (h *handlers) passFromRequest(ctx context.Context, r *http.Request) (verifierPass, bool, error) {
	return passFromRequest(ctx, h.d.DB.Q(), r)
}

// passFromRequest is the lookup itself, shared with the share-request handlers:
// a pass-holder asks a worker to see more the same way an onboarded party does.
func passFromRequest(ctx context.Context, q store.Querier, r *http.Request) (verifierPass, bool, error) {
	token := strings.TrimSpace(r.Header.Get(HeaderPass))
	if token == "" {
		return verifierPass{}, false, nil
	}
	want := hashPassToken(token)
	var p verifierPass
	var storedHash string
	err := q.QueryRow(ctx, `
		SELECT id, name, contact, token_hash, issued_at, rotated_at
		FROM verifier_passes WHERE token_hash = $1 AND revoked_at IS NULL`, want).
		Scan(&p.ID, &p.Name, &p.Contact, &storedHash, &p.IssuedAt, &p.RotatedAt)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return verifierPass{}, true, errNoPass
		}
		return verifierPass{}, true, err
	}
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(want)) != 1 {
		return verifierPass{}, true, errNoPass
	}
	return p, true, nil
}

// requester is who a check is recorded against.
type requester struct {
	ID string
	// Scope is the presentation scope the trail row carries: "pass" for a
	// pass-holder, "scoped" for a party (named, or the caller themselves).
	Scope string
}

// resolveRequester answers "who is asking" for a single check, or writes the
// refusal and returns false. The order: a pass header, a named party, the
// signed-in caller. Nobody is not an answer.
func (h *handlers) resolveRequester(w http.ResponseWriter, r *http.Request, namedParty string) (requester, bool) {
	pass, presented, err := h.passFromRequest(r.Context(), r)
	switch {
	case presented && errors.Is(err, errNoPass):
		httpx.WriteError(w, http.StatusUnauthorized, "invalid_pass",
			"that verifier pass is not one this deployment issued, or it has been replaced; ask for a new one at POST /v1/verifier-passes")
		return requester{}, false
	case presented && err != nil:
		httpx.Fail(w, h.d.Log, "read the verifier pass", err)
		return requester{}, false
	case presented:
		if namedParty != "" {
			httpx.WriteError(w, http.StatusBadRequest, "one_requester",
				"a check is asked for by a pass or by a party, not both; drop requestedByPartyId or the pass header")
			return requester{}, false
		}
		return requester{ID: pass.ID, Scope: "pass"}, true
	}
	if namedParty != "" {
		if !authorizeParty(w, r, h.d, namedParty) {
			return requester{}, false
		}
		return requester{ID: namedParty, Scope: "scoped"}, true
	}
	if !identity.From(r.Context()).Authenticated() {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, http.StatusUnauthorized, "pass_required",
			"an online check names who is asking, so the worker can see it: present a verifier pass "+
				"(POST /v1/verifier-passes, then the %s header) or sign in. Checking a signature needs "+
				"neither — do it offline against the issuer's published key", HeaderPass)
		return requester{}, false
	}
	// A signed-in caller naming nobody is asking as themselves.
	party, ok := identity.Authorize(w, r, h.d.Log, "", "", true, h.d.Permits)
	if !ok {
		return requester{}, false
	}
	return requester{ID: party, Scope: "scoped"}, true
}

// checksInWindow counts what a requester has already left in the trail
// within the cap's window.
func (h *handlers) checksInWindow(ctx context.Context, requesterID string, now time.Time) (int, error) {
	var n int
	err := h.d.DB.Q().QueryRow(ctx, `
		SELECT count(*) FROM presentations
		WHERE requested_by = $1 AND created_at > $2`, requesterID, now.Add(-h.rate.Window)).Scan(&n)
	return n, err
}

// underRateCap admits `wanted` more checks for a requester, or writes the 429
// and returns false. A batch that would cross the cap is refused whole rather
// than trimmed: a trimmed batch silently checks different people than the
// caller thinks it did.
func (h *handlers) underRateCap(w http.ResponseWriter, r *http.Request, who requester, wanted int, now time.Time) bool {
	used, err := h.checksInWindow(r.Context(), who.ID, now)
	if err != nil {
		httpx.Fail(w, h.d.Log, "count checks in the rate window", err)
		return false
	}
	remaining := h.rate.Cap - used
	if remaining < 0 {
		remaining = 0
	}
	w.Header().Set("X-CREST-Rate-Cap", strconv.Itoa(h.rate.Cap))
	w.Header().Set("X-CREST-Rate-Window", h.rate.Window.String())
	if used+wanted > h.rate.Cap {
		w.Header().Set("X-CREST-Rate-Remaining", "0")
		w.Header().Set("Retry-After", strconv.Itoa(int(h.rate.Window.Seconds())))
		h.d.Log.Info("a requester reached the rate cap", "requester", who.ID, "scope", who.Scope,
			"used", used, "wanted", wanted, "cap", h.rate.Cap, "window", h.rate.Window)
		httpx.WriteError(w, http.StatusTooManyRequests, "rate_cap_exceeded",
			"this %s has made %d checks in the last %s and the deployment's cap is %d per %s; "+
				"%d more would cross it. The cap is per pass or party and cannot be turned off (G1 #9); "+
				"checking at volume is a batch under a bulk authorization",
			who.Scope, used, h.rate.Window, h.rate.Cap, h.rate.Window, wanted)
		return false
	}
	w.Header().Set("X-CREST-Rate-Remaining", strconv.Itoa(remaining-wanted))
	return true
}

// bulkAuthorised is G1's second rule: the ability to check in bulk is an
// authorization somebody granted, not a property of being signed in.
func (h *handlers) bulkAuthorised(w http.ResponseWriter, r *http.Request, partyID string) bool {
	if h.d.Permits == nil {
		httpx.WriteError(w, http.StatusForbidden, "bulk_scope_required",
			"this deployment cannot check authorizations, so nobody may check in bulk")
		return false
	}
	ok, err := h.d.Permits(r.Context(), partyID, FunctionVerifyBulk, "")
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"whether %s may check in bulk could not be established right now", partyID)
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulk_scope_required",
			"%s holds no active %s authorization; checking many credentials at once is a scope an "+
				"authority grants, not a capacity that comes with a pass or a sign-in (G1 #9)",
			partyID, FunctionVerifyBulk)
		return false
	}
	return true
}

// passNamed reads a pass by id — for the worker's face of a share request,
// which shows who is asking by name rather than by an id they cannot resolve.
func passNamed(ctx context.Context, q store.Querier, id string) (verifierPass, error) {
	var p verifierPass
	err := q.QueryRow(ctx, `SELECT id, name, contact, issued_at, rotated_at FROM verifier_passes WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Contact, &p.IssuedAt, &p.RotatedAt)
	return p, err
}

// isPassID says whether a requester id is a pass rather than a party.
func isPassID(id string) bool { return strings.HasPrefix(id, "crest:pass:") }
