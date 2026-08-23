// Package credential issues and verifies WorkEventCredentials.
//
// The rule it exists to serve is W6: a worker's record is portable and
// verifiable without CREST. Everything here is chosen so that a verifier with
// no network and no access to this deployment can still check a credential —
// which rules out anything that has to be fetched at verification time.
package credential

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The cryptosuite. eddsa-jcs-2022 is Ed25519 over JCS-canonicalised JSON — a
// registered suite, and the one whose verification needs no context fetch.
const (
	CryptosuiteName = "eddsa-jcs-2022"
	ProofType       = "DataIntegrityProof"

	// ContextVC is the W3C VC 2.0 context; ContextCREST carries the
	// WorkEventCredential terms. They are asserted, not fetched: a verifier
	// that must resolve a URL to check a signature is not an offline verifier.
	ContextVC    = "https://www.w3.org/ns/credentials/v2"
	ContextCREST = "urn:crest:context:work-event-credential:1"
)

// ErrBadSignature is a credential whose proof does not check out.
var ErrBadSignature = errors.New("the credential's signature does not verify")

// Issuer holds a deployment's signing key.
//
// One key per deployment is §14's stated default. The structural split it
// leaves open — specifier and issuer being different parties — is expressed in
// the credential (the definition names its authorised issuers) rather than in
// the key, so moving to per-organisation keys later changes who signs, not what
// a verifier checks.
type Issuer struct {
	id  string
	key ed25519.PrivateKey
}

// NewIssuer builds an issuer from a seed. issuerID is the DID the credential
// names and the verification method resolves under.
func NewIssuer(issuerID string, seed []byte) (*Issuer, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("credential: signing seed is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	if issuerID == "" {
		return nil, errors.New("credential: an issuer needs an identifier a verifier can resolve")
	}
	return &Issuer{id: issuerID, key: ed25519.NewKeyFromSeed(seed)}, nil
}

// ID is the issuer identifier.
func (i *Issuer) ID() string { return i.id }

// PublicKeyMultibase is the verification key in the form a verifier resolves.
func (i *Issuer) PublicKeyMultibase() string {
	pub := i.key.Public().(ed25519.PublicKey)
	// 0xed 0x01 is the multicodec prefix for an Ed25519 public key.
	return "z" + base58Encode(append([]byte{0xed, 0x01}, pub...))
}

// VerificationMethod is the identifier the proof points at.
func (i *Issuer) VerificationMethod() string { return i.id + "#key-1" }

// Issue signs a credential. The document handed in must already be complete
// except for its proof: signing is not the place to fill anything in, because a
// field added here would be a field the caller never validated.
func (i *Issuer) Issue(doc map[string]any, at time.Time) (map[string]any, error) {
	if _, exists := doc["proof"]; exists {
		return nil, errors.New("credential: the document already carries a proof")
	}
	options := map[string]any{
		"type":               ProofType,
		"cryptosuite":        CryptosuiteName,
		"created":            at.UTC().Format(time.RFC3339),
		"verificationMethod": i.VerificationMethod(),
		"proofPurpose":       "assertionMethod",
	}
	input, err := signingInput(doc, options)
	if err != nil {
		return nil, err
	}
	options["proofValue"] = "z" + base58Encode(ed25519.Sign(i.key, input))

	signed := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		signed[k] = v
	}
	signed["proof"] = options
	return signed, nil
}

// Verify checks a credential against a public key in multibase form.
//
// It takes the key rather than looking one up, for the same reason the strength
// function takes the definition: a verifier offline in a field office has the
// key it trusts, not a network to ask.
func Verify(doc map[string]any, publicKeyMultibase string) error {
	rawProof, ok := doc["proof"]
	if !ok {
		return errors.New("the credential carries no proof")
	}
	proof, ok := rawProof.(map[string]any)
	if !ok {
		return errors.New("the credential's proof is not an object")
	}
	if proof["type"] != ProofType || proof["cryptosuite"] != CryptosuiteName {
		return fmt.Errorf("proof is %v/%v, not %s/%s",
			proof["type"], proof["cryptosuite"], ProofType, CryptosuiteName)
	}
	value, ok := proof["proofValue"].(string)
	if !ok || !strings.HasPrefix(value, "z") {
		return errors.New("the proof value is not multibase base58btc")
	}
	sig, err := base58Decode(value[1:])
	if err != nil {
		return err
	}

	pub, err := PublicKeyFromMultibase(publicKeyMultibase)
	if err != nil {
		return err
	}

	options := map[string]any{}
	for k, v := range proof {
		if k == "proofValue" {
			continue
		}
		options[k] = v
	}
	unsigned := map[string]any{}
	for k, v := range doc {
		if k == "proof" {
			continue
		}
		unsigned[k] = v
	}
	input, err := signingInput(unsigned, options)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, input, sig) {
		return ErrBadSignature
	}
	return nil
}

// PublicKeyFromMultibase decodes a z-prefixed multicodec Ed25519 key.
func PublicKeyFromMultibase(s string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(s, "z") {
		return nil, errors.New("a verification key must be multibase base58btc ('z')")
	}
	raw, err := base58Decode(s[1:])
	if err != nil {
		return nil, err
	}
	if len(raw) != 2+ed25519.PublicKeySize || raw[0] != 0xed || raw[1] != 0x01 {
		return nil, errors.New("that is not a multicodec Ed25519 public key")
	}
	return ed25519.PublicKey(raw[2:]), nil
}

// signingInput is sha256(canonical proof options) || sha256(canonical
// document), as the cryptosuite specifies. Hashing the options separately is
// what stops someone lifting a valid signature onto a proof that claims a
// different purpose or a different key.
func signingInput(doc, options map[string]any) ([]byte, error) {
	canonDoc, err := CanonicaliseValue(doc)
	if err != nil {
		return nil, err
	}
	canonOpts, err := CanonicaliseValue(options)
	if err != nil {
		return nil, err
	}
	docHash := sha256.Sum256(canonDoc)
	optHash := sha256.Sum256(canonOpts)
	return append(optHash[:], docHash[:]...), nil
}

// Digest is the sha256 of the canonical credential, which is what CREST stores
// instead of the credential itself. There is deliberately no credential
// register (§3) — the wallet holds the only complete copy — but a presented
// credential still has to be tieable to the record that issued it.
func Digest(doc map[string]any) (string, error) {
	canon, err := CanonicaliseValue(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex(sum[:]), nil
}

func hex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = digits[c>>4]
		out[i*2+1] = digits[c&0x0f]
	}
	return string(out)
}

// Document builds the WorkEventCredential body for an accepted claim.
//
// Everything it carries is a fact; nothing it carries is a judgement. The tier
// is absent on purpose (§6), and so is any name — the subject is the pairwise
// reference and nothing else (W8, W9).
func Document(credentialID, issuerID, subjectRef string, unit schema.Unit,
	confirmation schema.ClaimConfirmationRoute, confirmedAt time.Time,
	activity string, statusListURL string, statusIndex int, validFrom time.Time) (map[string]any, error) {
	cred := schema.WorkEventCredential{
		Context:   []string{ContextVC, ContextCREST},
		ID:        credentialID,
		Type:      []string{"VerifiableCredential", "WorkEventCredential"},
		Issuer:    issuerID,
		ValidFrom: validFrom.UTC(),
		CredentialSubject: schema.WorkEventCredentialCredentialSubject{
			ID: subjectRef,
			WorkEvent: schema.WorkEventCredentialCredentialSubjectWorkEvent{
				EventID:    unit.ID,
				Definition: unit.Definition,
				Activity:   activity,
				Outcome:    unit.Outcome,
				Period:     unit.Period,
				Geography:  unit.Geography,
			},
			Provenance: unit.Provenance,
			Confirmation: schema.WorkEventCredentialCredentialSubjectConfirmation{
				Route: schema.WorkEventCredentialCredentialSubjectConfirmationRoute(confirmation),
				At:    confirmedAt.UTC(),
			},
		},
		CredentialStatus: schema.WorkEventCredentialCredentialStatus{
			ID:                   fmt.Sprintf("%s#%d", statusListURL, statusIndex),
			Type:                 "BitstringStatusListEntry",
			StatusPurpose:        "revocation",
			StatusListIndex:      fmt.Sprint(statusIndex),
			StatusListCredential: statusListURL,
		},
	}
	if err := schema.Validate(schema.IDWorkEventCred, cred); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	return doc, dec.Decode(&doc)
}

// SeedFromBase64 reads a signing seed from configuration.
func SeedFromBase64(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("credential: signing seed is not base64: %w", err)
	}
	return raw, nil
}
