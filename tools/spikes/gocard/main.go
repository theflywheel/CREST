// gocard writes the PixelPass payload CREST itself would print, so that MOSIP's
// reader can be asked whether it can read it (see printedcard/reads-ours.mjs).
//
// Separate from the service because the question is about pkg/credential's
// encoder, and standing up a database to ask it would prove less, not more.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/theflywheel/crest/pkg/credential"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gocard <credential.json> <out-payload.txt>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read credential: %v\n", err)
		os.Exit(1)
	}

	// Re-marshalled so the bytes compared on the other side are the same bytes
	// on both sides of the round trip, rather than differing by whitespace.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "parse credential: %v\n", err)
		os.Exit(1)
	}
	compact, err := json.Marshal(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal credential: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], compact, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "rewrite credential: %v\n", err)
		os.Exit(1)
	}

	payload, err := credential.EncodePixelPass(compact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode card: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], []byte(payload), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write payload: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] CREST's own encoder produced a %d-character card payload\n", len(payload))
}
