// offlineverify checks a printed card with nothing but local files.
//
// #66's third leg, and the one that matters. Blueprint §5 claims the offline
// payload carries the full signed VC, so a bare scan verifies signature and
// schema with no network — that is what "provable to a stranger in a minute,
// offline" means, and it is the promise that survives when a worker is standing
// somewhere with no signal in front of somebody who has never heard of CREST.
//
// So this program has no HTTP client, by construction rather than by
// configuration. It reads the decoded credential and a *cached* issuer DID
// document from disk. If it could fetch the DID document it would not be
// testing the property in question: an offline verifier that phones home for a
// key is an online verifier with a longer timeout.
//
// What it can and cannot say is worth being exact about. It proves the record
// was signed by the key the card's issuer published, and that nothing in it has
// been altered since. It proves *validity at issuance* and nothing later — a
// revocation or a contest lives in a status list this card cannot see. That
// limit is in §5 and in the journeys' bare-scan semantics; stating it is the
// difference between an honest offline check and one that overclaims.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/theflywheel/crest/pkg/credential"
)

type didDoc struct {
	ID                 string `json:"id"`
	VerificationMethod []struct {
		ID                 string `json:"id"`
		PublicKeyMultibase string `json:"publicKeyMultibase"`
	} `json:"verificationMethod"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: offlineverify <decoded-credential.json> <issuer-did.json>")
		os.Exit(2)
	}

	var doc map[string]any
	readJSON(os.Args[1], &doc)
	var did didDoc
	readJSON(os.Args[2], &did)

	issuer, _ := doc["issuer"].(string)
	if issuer == "" || issuer != did.ID {
		fail("the card names issuer %q; the cached DID document is for %q", issuer, did.ID)
	}
	if len(did.VerificationMethod) == 0 {
		fail("the cached DID document publishes no verification method")
	}

	// The proof must name a key the issuer's own document publishes. Verifying
	// against "whatever key we happen to have" would accept a card signed by
	// anyone whose key we had cached.
	proof, _ := doc["proof"].(map[string]any)
	if proof == nil {
		fail("the card carries no proof at all")
	}
	method, _ := proof["verificationMethod"].(string)
	key := ""
	for _, vm := range did.VerificationMethod {
		if vm.ID == method {
			key = vm.PublicKeyMultibase
		}
	}
	if key == "" {
		fail("the proof names %q, which the issuer's document does not publish", method)
	}

	if err := credential.Verify(doc, key); err != nil {
		fail("the signature does not verify: %v", err)
	}

	pass("the card decodes to a credential issued by %s", issuer)
	pass("its proof names a key that issuer publishes (%s)", short(method))
	pass("the Ed25519 signature verifies with no network access of any kind")

	// An offline check that quietly implied currency would be worse than none:
	// a verifier would read "valid" and hear "not revoked".
	if _, ok := doc["credentialStatus"]; ok {
		fmt.Println("[NOTE] this credential carries a status list. Whether it has since been " +
			"revoked or contested cannot be known offline — this proves validity at issuance only.")
	} else {
		fmt.Println("[NOTE] proves validity at issuance only. Revocation and contest standing " +
			"need a signal this card cannot carry.")
	}
}

func readJSON(path string, into any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		fail("parse %s: %v", path, err)
	}
}

func short(s string) string {
	if i := strings.LastIndex(s, "#"); i >= 0 && len(s) > i+9 {
		return "…#" + s[i+1:i+9]
	}
	return s
}

func pass(format string, a ...any) { fmt.Printf("[PASS] "+format+"\n", a...) }

func fail(format string, a ...any) {
	fmt.Printf("[FAIL] "+format+"\n", a...)
	os.Exit(1)
}
