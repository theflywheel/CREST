// Command esignet mints the relying-party key CREST's login runs on (#155).
//
// The key IS the client identity: eSignet cannot rotate a registered client's
// public key (finding C9), so this is generated once per deployment, stored in
// the deployment's secret store (ESIGNET_CLIENT_KEY, or Vault behind it), and
// never regenerated casually — a new key is a new client. The PEM goes to
// stdout and nowhere else; nothing is written to disk.
//
//	go run ./tools/esignet | pbcopy    # then set ESIGNET_CLIENT_KEY
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/theflywheel/crest/pkg/esignet"
)

func main() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generating key:", err)
		os.Exit(1)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encoding key:", err)
		os.Exit(1)
	}
	if err := pem.Encode(os.Stdout, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		fmt.Fprintln(os.Stderr, "writing key:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "client id:", esignet.ClientIDFor("core", key))
}
